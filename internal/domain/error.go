package domain

import (
	"fmt"
	"sort"
	"strings"
)

// ErrorCode is a stable, machine-readable error code shared by the HTTP API
// and every business transaction. Codes are a documented failure boundary:
// callers rely on their spelling and stability.
type ErrorCode string

// Stable error codes required by the documented error contract.
const (
	CodeStaleRevision       ErrorCode = "STALE_REVISION"
	CodeHeatMismatch        ErrorCode = "HEAT_MISMATCH"
	CodeIntervalGap         ErrorCode = "INTERVAL_GAP"
	CodeIntervalOverlap     ErrorCode = "INTERVAL_OVERLAP"
	CodeGraphCycle          ErrorCode = "GRAPH_CYCLE"
	CodeMaterialOverdrawn   ErrorCode = "MATERIAL_OVERDRAWN"
	CodeContainerContam     ErrorCode = "CONTAINER_CONTAMINATION"
	CodeLeaseConflict       ErrorCode = "LEASE_CONFLICT"
	CodeLeaseExpired        ErrorCode = "LEASE_EXPIRED"
	CodePrefixViolation     ErrorCode = "PREFIX_VIOLATION"
	CodeThermalOutOfRange   ErrorCode = "THERMAL_OUT_OF_RANGE"
	CodeExposureExpired     ErrorCode = "EXPOSURE_EXPIRED"
	CodeDeviceRetryPending  ErrorCode = "DEVICE_RETRY_PENDING"
	CodeFixedPointOverflow  ErrorCode = "FIXED_POINT_OVERFLOW"
	CodeRepairLimit         ErrorCode = "REPAIR_LIMIT"
	CodeIdempotencyConflict ErrorCode = "IDEMPOTENCY_CONFLICT"
	CodeTerminalConflict    ErrorCode = "TERMINAL_CONFLICT"
)

// AllErrorCodes returns every documented error code in a stable order.
func AllErrorCodes() []ErrorCode {
	return []ErrorCode{
		CodeStaleRevision,
		CodeHeatMismatch,
		CodeIntervalGap,
		CodeIntervalOverlap,
		CodeGraphCycle,
		CodeMaterialOverdrawn,
		CodeContainerContam,
		CodeLeaseConflict,
		CodeLeaseExpired,
		CodePrefixViolation,
		CodeThermalOutOfRange,
		CodeExposureExpired,
		CodeDeviceRetryPending,
		CodeFixedPointOverflow,
		CodeRepairLimit,
		CodeIdempotencyConflict,
		CodeTerminalConflict,
	}
}

// DomainError is the single error shape returned by every business rule and
// serialized by the HTTP API. Reasons are always stored sorted and de-duplicated
// so that two identical failures produce byte-identical payloads.
type DomainError struct {
	Code        ErrorCode
	Path        string
	LogicalTime Milliseconds
	Reasons     []string
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("%s at %s (t=%d): %s", e.Code, e.Path, e.LogicalTime, strings.Join(e.Reasons, "; "))
}

// NewError builds a DomainError with sorted, de-duplicated reasons.
func NewError(code ErrorCode, path string, logicalTime Milliseconds, reasons ...string) *DomainError {
	seen := make(map[string]struct{}, len(reasons))
	uniq := make([]string, 0, len(reasons))
	for _, r := range reasons {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		uniq = append(uniq, r)
	}
	sort.Strings(uniq)
	return &DomainError{Code: code, Path: path, LogicalTime: logicalTime, Reasons: uniq}
}
