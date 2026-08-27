package service

import (
	"context"
	"encoding/json"
	"sort"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/repair"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
)

// MaxRepairs is the fixed repair-count ceiling; exceeding it yields REPAIR_LIMIT.
const MaxRepairs = 3

// CreateDefectRequest is the payload for recording a detected defect.
type CreateDefectRequest struct {
	Grade      string              `json:"grade"`
	Start      domain.Micrometers  `json:"start"`
	End        domain.Micrometers  `json:"end"`
	PassIDs    []string            `json:"pass_ids"`
	DetectedAt domain.Milliseconds `json:"detected_at"`
}

// CreateRepairRequest is the payload for opening a repair generation.
type CreateRepairRequest struct {
	DefectID    string              `json:"defect_id"`
	GougeVolume domain.Micrometers  `json:"gouge_volume"`
	CreatedAt   domain.Milliseconds `json:"created_at"`
}

// CreateRetestRequest is the payload for recording retest coverage.
type CreateRetestRequest struct {
	RepairID  string              `json:"repair_id"`
	ZoneIDs   []string            `json:"zone_ids"`
	Passed    bool                `json:"passed"`
	CreatedAt domain.Milliseconds `json:"created_at"`
}

// CreateDefect records a defect with its integer-micrometer coordinates and
// directly hit passes.
func (s *Service) CreateDefect(ctx context.Context, opID string, taskID task.TaskID, req CreateDefectRequest) (repair.Defect, error) {
	d := repair.Defect{
		ID:         newID(),
		TaskID:     string(taskID),
		Grade:      repair.DefectGrade(req.Grade),
		Start:      req.Start,
		End:        req.End,
		PassIDs:    req.PassIDs,
		DetectedAt: req.DetectedAt,
	}
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		if _, err := tx.Task(ctx, taskID); err != nil {
			return nil, ensureNotFound(err, domain.CodePrefixViolation, "service.defect")
		}
		if err := tx.CreateDefect(ctx, d); err != nil {
			return nil, err
		}
		return d, nil
	})
	if err != nil {
		return repair.Defect{}, err
	}
	var out repair.Defect
	if err := json.Unmarshal(body, &out); err != nil {
		return repair.Defect{}, err
	}
	return out, nil
}

// CreateRepair computes the deterministic closure, enforces the repair-count
// ceiling and opens a new immutable repair generation (bumping the task
// generation so late receipts from the superseded generation are isolated).
func (s *Service) CreateRepair(ctx context.Context, opID string, taskID task.TaskID, req CreateRepairRequest) (repair.RepairGeneration, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		t, err := tx.Task(ctx, taskID)
		if err != nil {
			return nil, ensureNotFound(err, domain.CodePrefixViolation, "service.repair")
		}
		d, err := tx.Defect(ctx, req.DefectID)
		if err != nil {
			return nil, ensureNotFound(err, domain.CodePrefixViolation, "service.repair")
		}
		count, err := tx.RepairCount(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if count >= MaxRepairs {
			return nil, domain.NewError(domain.CodeRepairLimit, "service.repair", req.CreatedAt,
				"repair count limit reached")
		}
		members := computeClosureFromTask(t, d)
		r := repair.RepairGeneration{
			ID:        newID(),
			TaskID:    string(taskID),
			Number:    count + 1,
			DefectID:  d.ID,
			Members:   members,
			CreatedAt: req.CreatedAt,
		}
		if err := tx.CreateRepair(ctx, r); err != nil {
			return nil, err
		}
		if err := tx.CreateGouging(ctx, repair.GougingRecord{ID: newID(), DefectID: d.ID, RepairID: r.ID, Volume: req.GougeVolume}); err != nil {
			return nil, err
		}
		// A repair opens a new generation: the old snapshot and its evidence are
		// permanently retained, and late receipts are isolated as audit events.
		t.Generation++
		t.Status = task.StatusRepair
		if err := tx.SaveTask(ctx, t); err != nil {
			return nil, err
		}
		return r, nil
	})
	if err != nil {
		return repair.RepairGeneration{}, err
	}
	var out repair.RepairGeneration
	if err := json.Unmarshal(body, &out); err != nil {
		return repair.RepairGeneration{}, err
	}
	return out, nil
}

// CreateRetest records retest coverage for a repair generation.
func (s *Service) CreateRetest(ctx context.Context, opID string, taskID task.TaskID, req CreateRetestRequest) (repair.RetestResult, error) {
	r := repair.RetestResult{ID: newID(), RepairID: req.RepairID, ZoneIDs: req.ZoneIDs, Passed: req.Passed, CreatedAt: req.CreatedAt}
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		if err := tx.CreateRetest(ctx, r); err != nil {
			return nil, err
		}
		return r, nil
	})
	if err != nil {
		return repair.RetestResult{}, err
	}
	var out repair.RetestResult
	if err := json.Unmarshal(body, &out); err != nil {
		return repair.RetestResult{}, err
	}
	return out, nil
}

// computeClosureFromTask builds the deterministic closure input from the
// locked task snapshot: pass identity (zone, layer, heat, holding) and the
// adjacency table (explicit heat-affected adjacencies plus same-layer pairs).
func computeClosureFromTask(t task.NodeTask, d repair.Defect) []repair.RepairMember {
	passes := map[string]repair.PassRef{}
	for _, p := range t.Passes() {
		passes[p.ID] = repair.PassRef{LayerID: p.LayerID, ZoneID: p.ZoneID, Heat: p.Heat, Holding: p.Holding}
	}
	adjacency := map[string][]string{}
	for _, a := range t.Weld.Adjacencies {
		adjacency[a.PassA] = append(adjacency[a.PassA], a.PassB)
		adjacency[a.PassB] = append(adjacency[a.PassB], a.PassA)
	}
	// Same-layer adjacency: passes sharing a layer are mutually adjacent.
	byLayer := map[string][]string{}
	for _, p := range t.Passes() {
		byLayer[p.LayerID] = append(byLayer[p.LayerID], p.ID)
	}
	for _, ids := range byLayer {
		for i := range ids {
			for j := range ids {
				if i != j {
					adjacency[ids[i]] = append(adjacency[ids[i]], ids[j])
				}
			}
		}
	}
	for k := range adjacency {
		sort.Strings(adjacency[k])
	}
	return repair.ComputeClosure(repair.ClosureInput{Defect: d, Passes: passes, Adjacency: adjacency})
}
