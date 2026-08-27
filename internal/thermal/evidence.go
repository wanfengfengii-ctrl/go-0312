// Package thermal records temperature, welding and slow-cooling evidence with
// logical time and computes coverage, line energy, cumulative heat input,
// equivalent post-heat duration and cooling rate using checked integer
// fixed-point arithmetic.
package thermal

import (
	"truss-thickplate-weld-restraint-release/internal/domain"
)

// EvidenceKind classifies an append-only evidence event.
type EvidenceKind string

const (
	KindGrooveConfirm EvidenceKind = "GROOVE_CONFIRM"
	KindPreheat       EvidenceKind = "PREHEAT"
	KindWeldPass      EvidenceKind = "WELD_PASS"
	KindInterpass     EvidenceKind = "INTERPASS_CLEAN"
	KindPostHeat      EvidenceKind = "POST_HEAT"
	KindSlowCool      EvidenceKind = "SLOW_COOL"
	KindVisual        EvidenceKind = "VISUAL"
	KindUltrasonic    EvidenceKind = "ULTRASONIC"
)

// EvidenceEvent is an append-only record of one measurement or action.
type EvidenceEvent struct {
	ID          string              `json:"id"`
	TaskID      string              `json:"task_id"`
	Generation  int64               `json:"generation"`
	Kind        EvidenceKind        `json:"kind"`
	LogicalTime domain.Milliseconds `json:"logical_time"`
	Current     domain.Fixed        `json:"current,omitempty"`
	Voltage     domain.Fixed        `json:"voltage,omitempty"`
	Speed       domain.Fixed        `json:"speed,omitempty"`
	Temperature domain.Fixed        `json:"temperature,omitempty"`
	Coverage    domain.Fixed        `json:"coverage,omitempty"`
	PassID      string              `json:"pass_id,omitempty"`
}

// PassPrefixProjection is the derived, rebuildable prefix of completed passes.
// It is versioned so evidence writes can perform optimistic concurrency checks.
type PassPrefixProjection struct {
	Completed []string `json:"completed"`
	Version   int64    `json:"version"`
}

// ThermalBarrierProjection tracks whether a valid preheat coverage has been
// established, gating pass evidence until the barrier is released.
type ThermalBarrierProjection struct {
	Established bool  `json:"established"`
	Version     int64 `json:"version"`
}

// LineEnergy computes line energy = current * voltage / speed. The result is
// expressed in the receiver's scale. All operations check overflow and zero
// divisors.
func LineEnergy(current, voltage, speed domain.Fixed) (domain.Fixed, error) {
	if speed.Raw <= 0 {
		return domain.Fixed{}, domain.NewError(domain.CodeFixedPointOverflow, "thermal.line-energy", 0, "non-positive speed")
	}
	p, err := current.Mul(voltage)
	if err != nil {
		return domain.Fixed{}, err
	}
	return p.Div(speed)
}

// CumulativeHeatInput sums a sequence of per-pass line energies. It aligns
// scales, checks overflow at every step and returns CodeFixedPointOverflow on
// any violation.
func CumulativeHeatInput(energies []domain.Fixed) (domain.Fixed, error) {
	if len(energies) == 0 {
		return domain.Fixed{}, domain.NewError(domain.CodeFixedPointOverflow, "thermal.cumulative", 0, "no inputs")
	}
	scale := energies[0].Scale
	acc := domain.Fixed{Raw: 0, Scale: scale}
	for _, e := range energies {
		r, err := e.Rescale(scale)
		if err != nil {
			return domain.Fixed{}, err
		}
		acc, err = acc.Add(r)
		if err != nil {
			return domain.Fixed{}, err
		}
	}
	return acc, nil
}

// CoolingRate computes the average cooling rate over a slow-cooling window:
// (startTemp - endTemp) / duration, at the temperature scale. A non-positive
// duration is rejected.
func CoolingRate(startTemp, endTemp domain.Fixed, duration domain.Milliseconds) (domain.Fixed, error) {
	if duration <= 0 {
		return domain.Fixed{}, domain.NewError(domain.CodeFixedPointOverflow, "thermal.cooling", 0, "non-positive duration")
	}
	diff, err := startTemp.Sub(endTemp)
	if err != nil {
		return domain.Fixed{}, err
	}
	return diff.Div(domain.Fixed{Raw: int64(duration), Scale: 0})
}

// EquivalentPostHeat sums the durations of post-heat events, requiring a
// strictly increasing logical-time sequence.
func EquivalentPostHeat(events []EvidenceEvent) (domain.Milliseconds, error) {
	var total domain.Milliseconds
	var prev domain.Milliseconds = -1
	for _, e := range events {
		if e.Kind != KindPostHeat {
			continue
		}
		if e.LogicalTime <= prev {
			return 0, domain.NewError(domain.CodeThermalOutOfRange, "thermal.post-heat", e.LogicalTime, "logical time not increasing")
		}
		prev = e.LogicalTime
		total++
	}
	return total, nil
}
