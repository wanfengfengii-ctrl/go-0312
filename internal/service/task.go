package service

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
)

// CreateTaskRequest is the payload for creating a draft node welding task.
type CreateTaskRequest struct {
	ID          string             `json:"id"`
	Zone        string             `json:"zone"`
	Component   string             `json:"component"`
	Node        string             `json:"node"`
	DesignEnd   domain.Micrometers `json:"design_end"`
	GrooveZones []task.GrooveZone  `json:"groove_zones"`
	Passes      []task.WeldPass    `json:"passes"`
}

// LockTaskRequest is the payload for locking a task into an immutable
// generation: it validates coverage, heat match, and topology against a
// catalog revision, then fixes the snapshot.
type LockTaskRequest struct {
	DesignID         string             `json:"design_id"`
	DesignVersion    int64              `json:"design_version"`
	ProcessID        string             `json:"process_id"`
	ProcessVersion   int64              `json:"process_version"`
	RevisionID       string             `json:"revision_id"`
	SectionHeat      string             `json:"section_heat"`
	SectionThickness domain.Micrometers `json:"section_thickness"`
	GrooveZones      []task.GrooveZone  `json:"groove_zones"`
	Passes           []task.WeldPass    `json:"passes"`
}

// CreateTask creates a draft node welding task with generation 0.
func (s *Service) CreateTask(ctx context.Context, opID string, req CreateTaskRequest) (task.NodeTask, error) {
	t := task.NodeTask{
		ID:         task.TaskID(req.ID),
		Zone:       req.Zone,
		Component:  req.Component,
		Node:       req.Node,
		Status:     task.StatusDraft,
		Generation: 0,
		Weld: task.Weld{
			ID:          "W-" + req.ID,
			DesignStart: 0,
			DesignEnd:   req.DesignEnd,
			GrooveZones: req.GrooveZones,
			Layers:      groupLayers(req.Passes),
		},
	}
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		if err := tx.SaveTask(ctx, t); err != nil {
			return nil, err
		}
		return t, nil
	})
	if err != nil {
		return task.NodeTask{}, err
	}
	var out task.NodeTask
	if err := json.Unmarshal(body, &out); err != nil {
		return task.NodeTask{}, err
	}
	return out, nil
}

// LockTask validates the lock-time invariants against the referenced catalog
// revision and, on success, fixes the snapshot into a new immutable generation.
func (s *Service) LockTask(ctx context.Context, opID string, taskID task.TaskID, req LockTaskRequest) (task.NodeTask, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		current, err := tx.Task(ctx, taskID)
		if err != nil {
			return nil, err
		}
		rev, err := tx.Revision(ctx, catalog.RevisionID(req.RevisionID))
		if err != nil {
			return nil, domain.NewError(domain.CodeStaleRevision, "service.task.lock", 0,
				"referenced revision does not exist")
		}
		lr := task.LockRequest{
			TaskID:           taskID,
			DesignID:         req.DesignID,
			DesignVersion:    req.DesignVersion,
			ProcessID:        req.ProcessID,
			ProcessVersion:   req.ProcessVersion,
			SectionHeat:      req.SectionHeat,
			SectionThickness: req.SectionThickness,
			Revision:         rev,
			DesignEnd:        current.Weld.DesignEnd,
			GrooveZones:      req.GrooveZones,
			Passes:           req.Passes,
		}
		if err := task.ValidateLock(lr, rev.EffectiveTime); err != nil {
			return nil, err
		}
		current.Status = task.StatusLocked
		current.Generation++
		current.CatalogRevision = req.RevisionID
		current.Weld.GrooveZones = req.GrooveZones
		current.Weld.Layers = groupLayers(req.Passes)
		if err := tx.SaveTask(ctx, current); err != nil {
			return nil, err
		}
		return current, nil
	})
	if err != nil {
		return task.NodeTask{}, err
	}
	var out task.NodeTask
	if err := json.Unmarshal(body, &out); err != nil {
		return task.NodeTask{}, err
	}
	return out, nil
}

// GetTask returns a task by ID.
func (s *Service) GetTask(ctx context.Context, id task.TaskID) (task.NodeTask, error) {
	return s.store.Task(ctx, id)
}

// ListTasks returns every task ordered by ID.
func (s *Service) ListTasks(ctx context.Context) ([]task.NodeTask, error) {
	return s.store.ListTasks(ctx)
}

// GraphView is the stable, sorted evidence-graph projection served to clients.
type GraphView struct {
	TaskID     string                `json:"task_id"`
	Status     task.Status           `json:"status"`
	Generation task.Generation       `json:"generation"`
	Zones      []task.GrooveZone     `json:"zones"`
	Passes     []task.WeldPass       `json:"passes"`
	Deps       []task.PassDependency `json:"dependencies"`
	Completed  []string              `json:"completed"`
}

// GetGraph returns the task evidence graph with the current valid prefix.
func (s *Service) GetGraph(ctx context.Context, id task.TaskID) (GraphView, error) {
	t, err := s.store.Task(ctx, id)
	if err != nil {
		return GraphView{}, err
	}
	view := GraphView{
		TaskID:     string(t.ID),
		Status:     t.Status,
		Generation: t.Generation,
		Zones:      t.Weld.GrooveZones,
		Passes:     t.Passes(),
		Deps:       collectDeps(t.Passes()),
	}
	if prefix, err := s.store.PassPrefix(ctx, id); err == nil {
		view.Completed = prefix.Completed
	}
	return view, nil
}

// groupLayers collapses flat passes into a single weld layer "L1". The
// two-sided symmetric sequence rule operates on the flat pass sequence, so a
// single layer preserves that ordering while satisfying the layer/aggregate
// shape.
func groupLayers(passes []task.WeldPass) []task.WeldLayer {
	if len(passes) == 0 {
		return nil
	}
	return []task.WeldLayer{{ID: "L1", Sequence: 1, Passes: passes}}
}

// collectDeps returns the directed predecessor edges of a pass set in stable
// order.
func collectDeps(passes []task.WeldPass) []task.PassDependency {
	var deps []task.PassDependency
	for _, p := range passes {
		for _, pred := range p.Preds {
			deps = append(deps, task.PassDependency{From: pred, To: p.ID})
		}
	}
	return deps
}

// ensureNotFound maps a store not-found error to a domain error.
func ensureNotFound(err error, code domain.ErrorCode, path string) error {
	if errors.Is(err, store.ErrNotFound) {
		return domain.NewError(code, path, 0, "resource not found")
	}
	return err
}
