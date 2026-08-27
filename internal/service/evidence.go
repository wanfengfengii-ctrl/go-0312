package service

import (
	"context"
	"encoding/json"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

// EvidenceRequest is the payload for appending one piece of welding evidence.
type EvidenceRequest struct {
	TaskID      string              `json:"task_id,omitempty"`
	Kind        string              `json:"kind"`
	Generation  int64               `json:"generation"`
	LogicalTime domain.Milliseconds `json:"logical_time"`
	PassID      string              `json:"pass_id,omitempty"`
	Current     domain.Fixed        `json:"current,omitempty"`
	Voltage     domain.Fixed        `json:"voltage,omitempty"`
	Speed       domain.Fixed        `json:"speed,omitempty"`
	Temperature domain.Fixed        `json:"temperature,omitempty"`
	Coverage    domain.Fixed        `json:"coverage,omitempty"`
	ResourceID  string              `json:"resource_id,omitempty"`
}

// EvidenceResult is the outcome of an evidence write.
type EvidenceResult struct {
	EventID            string        `json:"event_id"`
	Kind               string        `json:"kind"`
	Accepted           bool          `json:"accepted"`
	PrefixVersion      int64         `json:"prefix_version"`
	BarrierEstablished bool          `json:"barrier_established"`
	DeviceCallID       string        `json:"device_call_id,omitempty"`
	LineEnergy         *domain.Fixed `json:"line_energy,omitempty"`
}

// deviceBackedKind reports whether a kind requires a scripted device reading.
func deviceBackedKind(kind string) bool {
	return kind == string(thermal.KindPreheat) || kind == string(thermal.KindUltrasonic)
}

// WriteEvidence appends evidence for a task. Device-backed kinds (preheat,
// ultrasonic) invoke the scripted device and, on the deterministic first
// timeout, return DEVICE_RETRY_PENDING with the device call id instead of
// recording a reading or advancing any projection.
func (s *Service) WriteEvidence(ctx context.Context, opID string, taskID task.TaskID, req EvidenceRequest) (EvidenceResult, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		t, err := tx.Task(ctx, taskID)
		if err != nil {
			return nil, ensureNotFound(err, domain.CodePrefixViolation, "service.evidence")
		}
		if t.Status != task.StatusLocked && t.Status != task.StatusWelding && t.Status != task.StatusRepair {
			return nil, domain.NewError(domain.CodePrefixViolation, "service.evidence", req.LogicalTime,
				"task not locked")
		}
		if req.Generation != int64(t.Generation) {
			return nil, domain.NewError(domain.CodePrefixViolation, "service.evidence", req.LogicalTime,
				"generation mismatch")
		}
		if deviceBackedKind(req.Kind) {
			req.TaskID = string(taskID)
			return s.startDeviceCall(ctx, tx, taskID, req)
		}
		return s.applyEvidence(ctx, tx, t, req)
	})
	if err != nil {
		return EvidenceResult{}, err
	}
	var out EvidenceResult
	if err := json.Unmarshal(body, &out); err != nil {
		return EvidenceResult{}, err
	}
	return out, nil
}

// startDeviceCall records the deterministic first (timeout) attempt of a
// scripted device. It commits the pending call and returns a non-accepted
// result carrying the device call id, so the client can drive retries without
// any reading being recorded or any projection advanced.
func (s *Service) startDeviceCall(ctx context.Context, tx store.Store, taskID task.TaskID, req EvidenceRequest) (EvidenceResult, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return EvidenceResult{}, err
	}
	call := material.DeviceCall{
		ID:          newID(),
		ResourceID:  req.ResourceID,
		PayloadHash: contentHash(payload),
		Payload:     payload,
		RetrySeq:    0,
		Status:      material.DevicePending,
	}
	if err := tx.SaveDeviceCall(ctx, call); err != nil {
		return EvidenceResult{}, err
	}
	return EvidenceResult{
		Kind:         req.Kind,
		Accepted:     false,
		DeviceCallID: call.ID,
	}, nil
}

// applyEvidence records a non-device evidence event and advances the derived
// projections. It is also reused by the device retry path on success.
func (s *Service) applyEvidence(ctx context.Context, tx store.Store, t task.NodeTask, req EvidenceRequest) (EvidenceResult, error) {
	switch req.Kind {
	case string(thermal.KindGrooveConfirm), string(thermal.KindInterpass), string(thermal.KindPostHeat), string(thermal.KindSlowCool), string(thermal.KindVisual), string(thermal.KindUltrasonic):
		return s.recordSimple(ctx, tx, t, req)
	case string(thermal.KindWeldPass):
		return s.recordWeldPass(ctx, tx, t, req)
	case string(thermal.KindPreheat):
		return s.recordPreheat(ctx, tx, t, req)
	default:
		return EvidenceResult{}, domain.NewError(domain.CodePrefixViolation, "service.evidence", req.LogicalTime,
			"unknown evidence kind "+req.Kind)
	}
}

// recordSimple appends a plain evidence event without changing the pass prefix.
func (s *Service) recordSimple(ctx context.Context, tx store.Store, t task.NodeTask, req EvidenceRequest) (EvidenceResult, error) {
	event := thermal.EvidenceEvent{
		ID:          newID(),
		TaskID:      string(t.ID),
		Generation:  int64(t.Generation),
		Kind:        thermal.EvidenceKind(req.Kind),
		LogicalTime: req.LogicalTime,
		Temperature: req.Temperature,
		Coverage:    req.Coverage,
		PassID:      req.PassID,
	}
	if err := tx.AppendEvidence(ctx, event); err != nil {
		return EvidenceResult{}, err
	}
	prefix, _ := tx.PassPrefix(ctx, t.ID)
	barrier, _ := tx.ThermalBarrier(ctx, t.ID)
	return EvidenceResult{EventID: event.ID, Kind: req.Kind, Accepted: true, PrefixVersion: prefix.Version, BarrierEstablished: barrier.Established}, nil
}

// recordPreheat records a preheat reading and establishes the thermal barrier
// once the measured coverage reaches the prescribed threshold.
func (s *Service) recordPreheat(ctx context.Context, tx store.Store, t task.NodeTask, req EvidenceRequest) (EvidenceResult, error) {
	event := thermal.EvidenceEvent{
		ID:          newID(),
		TaskID:      string(t.ID),
		Generation:  int64(t.Generation),
		Kind:        thermal.KindPreheat,
		LogicalTime: req.LogicalTime,
		Temperature: req.Temperature,
		Coverage:    req.Coverage,
	}
	if err := tx.AppendEvidence(ctx, event); err != nil {
		return EvidenceResult{}, err
	}
	barrier, _ := tx.ThermalBarrier(ctx, t.ID)
	established := barrier.Established
	if rev, err := tx.Revision(ctx, catalog.RevisionID(t.CatalogRevision)); err == nil {
		if ts, ok := firstThreshold(rev); ok && req.Coverage.Cmp(ts.PreheatCoverage) >= 0 {
			established = true
		}
	}
	if established && !barrier.Established {
		if err := tx.SaveThermalBarrier(ctx, t.ID, true, barrier.Version+1); err != nil {
			return EvidenceResult{}, err
		}
		barrier.Version++
		barrier.Established = true
	}
	return EvidenceResult{EventID: event.ID, Kind: req.Kind, Accepted: true, PrefixVersion: mustPrefixVersion(ctx, tx, t.ID), BarrierEstablished: established}, nil
}

func mustPrefixVersion(ctx context.Context, tx store.Store, taskID task.TaskID) int64 {
	prefix, err := tx.PassPrefix(ctx, taskID)
	if err != nil {
		return 0
	}
	return prefix.Version
}

// recordWeldPass validates the thermal barrier, the two-sided prefix order, the
// interpass temperature window, the exposure limit and the heat input before
// advancing the valid construction prefix.
func (s *Service) recordWeldPass(ctx context.Context, tx store.Store, t task.NodeTask, req EvidenceRequest) (EvidenceResult, error) {
	barrier, _ := tx.ThermalBarrier(ctx, t.ID)
	if !barrier.Established {
		return EvidenceResult{}, domain.NewError(domain.CodeThermalOutOfRange, "service.evidence", req.LogicalTime,
			"thermal barrier not established")
	}

	prefix, _ := tx.PassPrefix(ctx, t.ID)
	completed := prefix.Completed
	if err := t.AppendPass(completed, req.PassID); err != nil {
		err.Path = "service.evidence"
		err.LogicalTime = req.LogicalTime
		return EvidenceResult{}, err
	}

	rev, _ := tx.Revision(ctx, catalog.RevisionID(t.CatalogRevision))
	if ts, ok := firstThreshold(rev); ok {
		if !req.Temperature.Between(ts.InterpassMin, ts.InterpassMax) {
			return EvidenceResult{}, domain.NewError(domain.CodeThermalOutOfRange, "service.evidence", req.LogicalTime,
				"interpass temperature out of range")
		}
		if ts.ExposureLimit > 0 {
			last := lastWeldTime(ctx, tx, t.ID)
			if last >= 0 && req.LogicalTime-last > ts.ExposureLimit {
				return EvidenceResult{}, domain.NewError(domain.CodeExposureExpired, "service.evidence", req.LogicalTime,
					"exposure limit exceeded")
			}
		}
	}

	var lineEnergy *domain.Fixed
	if req.Current.Raw != 0 || req.Voltage.Raw != 0 || req.Speed.Raw != 0 {
		le, err := thermal.LineEnergy(req.Current, req.Voltage, req.Speed)
		if err != nil {
			return EvidenceResult{}, err
		}
		lineEnergy = &le
	}

	event := thermal.EvidenceEvent{
		ID:          newID(),
		TaskID:      string(t.ID),
		Generation:  int64(t.Generation),
		Kind:        thermal.KindWeldPass,
		LogicalTime: req.LogicalTime,
		Current:     req.Current,
		Voltage:     req.Voltage,
		Speed:       req.Speed,
		Temperature: req.Temperature,
		PassID:      req.PassID,
	}
	if err := tx.AppendEvidence(ctx, event); err != nil {
		return EvidenceResult{}, err
	}
	completed = append(completed, req.PassID)
	if err := tx.SavePassPrefix(ctx, t.ID, completed, prefix.Version+1); err != nil {
		return EvidenceResult{}, err
	}
	return EvidenceResult{EventID: event.ID, Kind: req.Kind, Accepted: true, PrefixVersion: prefix.Version + 1, BarrierEstablished: true, LineEnergy: lineEnergy}, nil
}

// firstThreshold returns the first threshold set of a revision, if present.
func firstThreshold(rev catalog.CatalogRevision) (catalog.ThresholdSet, bool) {
	if len(rev.ThresholdSets) == 0 {
		return catalog.ThresholdSet{}, false
	}
	return rev.ThresholdSets[0], true
}

// lastWeldTime returns the logical time of the last weld-pass event, or -1.
func lastWeldTime(ctx context.Context, tx store.Store, taskID task.TaskID) domain.Milliseconds {
	events, err := tx.Evidence(ctx, taskID)
	if err != nil {
		return -1
	}
	var last domain.Milliseconds = -1
	for _, e := range events {
		if e.Kind == thermal.KindWeldPass && e.LogicalTime > last {
			last = e.LogicalTime
		}
	}
	return last
}
