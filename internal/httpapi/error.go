// Package httpapi exposes the documented JSON API: Operation-Id on all writes,
// a stable error response shape, task and evidence queries and scripted device
// adaptation, plus embedded deterministic frontend static assets.
package httpapi

import (
	"encoding/json"
	"net/http"

	"truss-thickplate-weld-restraint-release/internal/domain"
)

// errorBody is the normalized error payload returned on every failure.
type errorBody struct {
	Code        string              `json:"code"`
	Path        string              `json:"path"`
	LogicalTime domain.Milliseconds `json:"logical_time"`
	Reasons     []string            `json:"reasons"`
}

// writeError serializes a DomainError with the stable contract.
func writeError(w http.ResponseWriter, status int, err *domain.DomainError) {
	if err.Reasons == nil {
		err.Reasons = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{
		Code:        string(err.Code),
		Path:        err.Path,
		LogicalTime: err.LogicalTime,
		Reasons:     err.Reasons,
	})
}

// writeDomainError maps a domain error to its HTTP status and serializes it.
func writeDomainError(w http.ResponseWriter, err *domain.DomainError) {
	writeError(w, statusForCode(err.Code), err)
}

// statusForCode maps each stable error code to an HTTP status. Conflicts map to
// 409, domain-rule violations to 422, retry-pending to 503, and anything else
// to 400.
func statusForCode(code domain.ErrorCode) int {
	switch code {
	case domain.CodeLeaseConflict,
		domain.CodeIdempotencyConflict,
		domain.CodeTerminalConflict,
		domain.CodeContainerContam,
		domain.CodeStaleRevision:
		return http.StatusConflict
	case domain.CodeDeviceRetryPending:
		return http.StatusServiceUnavailable
	case domain.CodeMaterialOverdrawn,
		domain.CodeIntervalGap,
		domain.CodeIntervalOverlap,
		domain.CodeGraphCycle,
		domain.CodeHeatMismatch,
		domain.CodePrefixViolation,
		domain.CodeThermalOutOfRange,
		domain.CodeExposureExpired,
		domain.CodeFixedPointOverflow,
		domain.CodeRepairLimit,
		domain.CodeLeaseExpired:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusBadRequest
	}
}

// writeJSON serializes any success payload.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeResult serializes a success payload with status 200.
func writeResult(w http.ResponseWriter, v any) {
	writeJSON(w, http.StatusOK, v)
}
