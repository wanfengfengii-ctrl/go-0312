package service

import (
	"context"
	"encoding/json"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
)

// RetryDeviceCallRequest is the payload for advancing a scripted device call.
type RetryDeviceCallRequest struct {
	LogicalTime domain.Milliseconds `json:"logical_time"`
}

// RetryDeviceCallResult is the outcome of one device retry attempt.
type RetryDeviceCallResult struct {
	DeviceCallID string                    `json:"device_call_id"`
	Status       material.DeviceCallStatus `json:"status"`
	RetrySeq     int64                     `json:"retry_seq"`
	Outcome      material.ScriptOutcome    `json:"outcome"`
	Evidence     *EvidenceResult           `json:"evidence,omitempty"`
}

// RetryDeviceCall advances a pending scripted device call by one deterministic
// attempt. The first retry returns a malformed payload, the second succeeds and
// records the real reading (advancing the thermal barrier or inspection). A
// call past its ceiling enters manual exception and never fabricates a reading.
func (s *Service) RetryDeviceCall(ctx context.Context, opID string, callID string, req RetryDeviceCallRequest) (RetryDeviceCallResult, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		call, err := tx.DeviceCall(ctx, callID)
		if err != nil {
			return nil, ensureNotFound(err, domain.CodeDeviceRetryPending, "service.device")
		}
		if call.Status == material.DeviceSucceeded {
			return RetryDeviceCallResult{DeviceCallID: call.ID, Status: call.Status, RetrySeq: call.RetrySeq, Outcome: material.ScriptSuccess}, nil
		}
		if !call.CanRetry() {
			return nil, domain.NewError(domain.CodeDeviceRetryPending, "service.device", req.LogicalTime,
				"device call exhausted; manual exception required")
		}

		attempt := call.RetrySeq + 1
		outcome := material.ScriptedDeviceOutcome(attempt)
		switch outcome {
		case material.ScriptSuccess:
			call.Status = material.DeviceSucceeded
			call.RetrySeq = attempt
			if err := tx.SaveDeviceCall(ctx, call); err != nil {
				return nil, err
			}
			ev, err := s.recordDeviceReading(ctx, tx, call)
			if err != nil {
				return nil, err
			}
			return RetryDeviceCallResult{DeviceCallID: call.ID, Status: call.Status, RetrySeq: call.RetrySeq, Outcome: outcome, Evidence: &ev}, nil
		default:
			call.RetrySeq = attempt
			if err := tx.SaveDeviceCall(ctx, call); err != nil {
				return nil, err
			}
			return RetryDeviceCallResult{DeviceCallID: call.ID, Status: call.Status, RetrySeq: call.RetrySeq, Outcome: outcome}, nil
		}
	})
	if err != nil {
		return RetryDeviceCallResult{}, err
	}
	var out RetryDeviceCallResult
	if err := json.Unmarshal(body, &out); err != nil {
		return RetryDeviceCallResult{}, err
	}
	return out, nil
}

// recordDeviceReading decodes the pending evidence request from a successful
// device call and records the real reading, advancing the thermal barrier or
// inspection result.
func (s *Service) recordDeviceReading(ctx context.Context, tx store.Store, call material.DeviceCall) (EvidenceResult, error) {
	var req EvidenceRequest
	if err := json.Unmarshal(call.Payload, &req); err != nil {
		return EvidenceResult{}, err
	}
	t, err := tx.Task(ctx, task.TaskID(req.TaskID))
	if err != nil {
		return EvidenceResult{}, ensureNotFound(err, domain.CodePrefixViolation, "service.device")
	}
	if req.Generation != int64(t.Generation) {
		return EvidenceResult{}, domain.NewError(domain.CodePrefixViolation, "service.device", req.LogicalTime,
			"generation mismatch on device success")
	}
	return s.applyEvidence(ctx, tx, t, req)
}
