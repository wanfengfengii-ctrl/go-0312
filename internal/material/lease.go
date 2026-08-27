package material

import (
	"truss-thickplate-weld-restraint-release/internal/domain"
)

// ResourceType enumerates the mutually exclusive resources.
type ResourceType string

const (
	ResourcePreheater   ResourceType = "PREHEATER"
	ResourceWelder      ResourceType = "WELDER"
	ResourceHoldingOven ResourceType = "HOLDING_OVEN"
	ResourceTempChannel ResourceType = "TEMP_CHANNEL"
	ResourceGouging     ResourceType = "GOUGING_POSITION"
	ResourceUltrasonic  ResourceType = "ULTRASONIC_CHANNEL"
)

// Resource is an identifiable, leasable physical resource.
type Resource struct {
	ID   string       `json:"id"`
	Type ResourceType `json:"type"`
}

// Lease is a time-bounded exclusive hold on a resource, versioned so renewals
// and releases can be compared-and-swapped.
type Lease struct {
	ID         string              `json:"id"`
	ResourceID string              `json:"resource_id"`
	Operation  string              `json:"operation"`
	Start      domain.Milliseconds `json:"start"`
	End        domain.Milliseconds `json:"end"`
	Version    int64               `json:"version"`
}

// Overlaps reports whether two leases on the same resource overlap in logical
// time. Boundary contact (a.End == b.Start) is allowed.
func (l Lease) Overlaps(o Lease) bool {
	return l.Start < o.End && o.Start < l.End
}

// ExpiredAt reports whether the lease is expired at a logical time. A lease is
// valid on [Start, End) and expires exactly at End.
func (l Lease) ExpiredAt(t domain.Milliseconds) bool {
	return t < l.Start || t >= l.End
}

// Validate checks a lease's interval and returns a stable error.
func (l Lease) Validate() *domain.DomainError {
	if l.Start < 0 || l.End < 0 {
		return domain.NewError(domain.CodeLeaseConflict, "material.lease", 0, "negative lease time")
	}
	if l.Start >= l.End {
		return domain.NewError(domain.CodeLeaseConflict, "material.lease", 0, "degenerate lease interval")
	}
	return nil
}

// CheckConflict validates a new lease against existing leases for the same
// resource, returning domain.CodeLeaseConflict on any overlapping interval.
func CheckConflict(existing []Lease, candidate Lease) *domain.DomainError {
	for _, e := range existing {
		if e.ResourceID != candidate.ResourceID {
			continue
		}
		if candidate.Overlaps(e) {
			return domain.NewError(domain.CodeLeaseConflict, "material.lease", candidate.Start,
				"overlap with lease "+e.ID)
		}
	}
	return nil
}

// DeviceCall records a scripted device invocation and its deterministic retry
// state. Failures only append call records; they never fabricate readings.
type DeviceCall struct {
	ID          string           `json:"id"`
	ResourceID  string           `json:"resource_id"`
	PayloadHash string           `json:"payload_hash"`
	Payload     []byte           `json:"payload,omitempty"`
	RetrySeq    int64            `json:"retry_seq"`
	Status      DeviceCallStatus `json:"status"`
}

// DeviceCallStatus is the lifecycle of a device call.
type DeviceCallStatus string

const (
	DevicePending    DeviceCallStatus = "PENDING"
	DeviceSucceeded  DeviceCallStatus = "SUCCEEDED"
	DeviceManualFail DeviceCallStatus = "MANUAL_FAIL"
)

// MaxRetries is the fixed retry ceiling after which a call enters manual
// exception instead of producing a reading.
const MaxRetries = 3

// CanRetry reports whether a call may still be retried under the fixed ceiling.
func (c DeviceCall) CanRetry() bool {
	return c.Status == DevicePending && c.RetrySeq < MaxRetries
}

// ScriptOutcome classifies a scripted device invocation's deterministic result.
type ScriptOutcome string

const (
	ScriptTimeout   ScriptOutcome = "TIMEOUT"
	ScriptMalformed ScriptOutcome = "MALFORMED"
	ScriptSuccess   ScriptOutcome = "SUCCESS"
)

// ScriptedDeviceOutcome returns the deterministic outcome of a scripted device
// given how many attempts have already been made. The public script is: the
// first attempt times out, the second returns a malformed payload, and the
// third (and any later) attempt succeeds. This makes the device retry sequence
// exactly assertable and restartable.
func ScriptedDeviceOutcome(attempt int64) ScriptOutcome {
	switch {
	case attempt <= 0:
		return ScriptTimeout
	case attempt == 1:
		return ScriptMalformed
	default:
		return ScriptSuccess
	}
}
