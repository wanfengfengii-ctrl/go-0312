package domain

import (
	"math"
)

// Fixed is a signed fixed-point number. Raw is the integer value scaled by
// 10^Scale. Temperature, coverage, power, speed, line energy and cooling rate
// all use explicit signed integer fixed-point scales so results are exactly
// assertable. Operations check overflow, negative duration and zero divisors
// before producing a value; any violation returns CodeFixedPointOverflow.
type Fixed struct {
	Raw   int64 `json:"raw"`
	Scale int32 `json:"scale"`
}

// NewFixed returns a fixed-point value, rejecting negative scales.
func NewFixed(raw int64, scale int32) (Fixed, error) {
	if scale < 0 {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "negative scale")
	}
	return Fixed{Raw: raw, Scale: scale}, nil
}

// MustFixed panics on invalid scale; intended for constants in tests and code.
func MustFixed(raw int64, scale int32) Fixed {
	f, err := NewFixed(raw, scale)
	if err != nil {
		panic(err)
	}
	return f
}

// Cmp compares two fixed-point values. Scales are ignored only for equality of
// a numerically identical value; callers should Rescale before arithmetic.
func (f Fixed) Cmp(o Fixed) int {
	// Compare at a common scale to be numerically correct.
	common := f.Scale
	if o.Scale > common {
		common = o.Scale
	}
	a, errA := f.Rescale(common)
	b, errB := o.Rescale(common)
	if errA != nil || errB != nil {
		// Overflow while aligning: fall back to sign/magnitude of raw.
		lf, _ := f.float()
		lo, _ := o.float()
		if lf < lo {
			return -1
		}
		if lf > lo {
			return 1
		}
		return 0
	}
	switch {
	case a.Raw < b.Raw:
		return -1
	case a.Raw > b.Raw:
		return 1
	default:
		return 0
	}
}

func (f Fixed) float() (float64, error) {
	return float64(f.Raw) / math.Pow10(int(f.Scale)), nil
}

// Rescale returns f represented at target scale using round half away from
// zero. Reducing scale may round; increasing scale multiplies.
func (f Fixed) Rescale(target int32) (Fixed, error) {
	if target < 0 {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "negative target scale")
	}
	if target == f.Scale {
		return f, nil
	}
	if target > f.Scale {
		mul := pow10(target - f.Scale)
		raw, err := mulInt64Checked(f.Raw, mul)
		if err != nil {
			return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "rescale overflow")
		}
		return Fixed{Raw: raw, Scale: target}, nil
	}
	div := pow10(f.Scale - target)
	raw := roundHalfAway(f.Raw, div)
	return Fixed{Raw: raw, Scale: target}, nil
}

// Add adds two fixed-point values at equal scale with overflow checking.
func (f Fixed) Add(o Fixed) (Fixed, error) {
	if f.Scale != o.Scale {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "mismatched scales")
	}
	sum, err := addInt64Checked(f.Raw, o.Raw)
	if err != nil {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "addition overflow")
	}
	return Fixed{Raw: sum, Scale: f.Scale}, nil
}

// Sub subtracts two fixed-point values at equal scale with overflow checking.
func (f Fixed) Sub(o Fixed) (Fixed, error) {
	if f.Scale != o.Scale {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "mismatched scales")
	}
	diff, err := subInt64Checked(f.Raw, o.Raw)
	if err != nil {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "subtraction overflow")
	}
	return Fixed{Raw: diff, Scale: f.Scale}, nil
}

// Mul multiplies two fixed-point values. The result scale is the sum of the
// input scales. Overflow is checked before multiplying.
func (f Fixed) Mul(o Fixed) (Fixed, error) {
	if f.Raw != 0 && o.Raw != 0 {
		if mulOverflows(f.Raw, o.Raw) {
			return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "multiplication overflow")
		}
	}
	scale := int64(f.Scale) + int64(o.Scale)
	if scale > math.MaxInt32 {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "scale overflow")
	}
	return Fixed{Raw: f.Raw * o.Raw, Scale: int32(scale)}, nil
}

// Div divides f by o with round half away from zero. A zero divisor or a
// negative/overflowing scale is rejected before any division.
func (f Fixed) Div(o Fixed) (Fixed, error) {
	if o.Raw == 0 {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "zero divisor")
	}
	if f.Scale < o.Scale {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "negative result scale")
	}
	// Align to a common numerator scale so the integer quotient is exact at
	// f.Scale before applying round-half-away.
	diff := f.Scale - o.Scale
	num, err := mulInt64Checked(f.Raw, pow10(diff))
	if err != nil {
		return Fixed{}, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "division overflow")
	}
	return Fixed{Raw: num / o.Raw, Scale: f.Scale}, nil
}

// roundHalfAway divides n by d rounding half away from zero.
func roundHalfAway(n, d int64) int64 {
	q := n / d
	r := n % d
	// r has the sign of n.
	if r == 0 {
		return q
	}
	if r < 0 {
		r = -r
	}
	if 2*r >= d {
		if n < 0 {
			return q - 1
		}
		return q + 1
	}
	return q
}

func pow10(e int32) int64 {
	var v int64 = 1
	for i := int32(0); i < e; i++ {
		v *= 10
	}
	return v
}

func mulOverflows(a, b int64) bool {
	if a == -1 && b == math.MinInt64 {
		return true
	}
	if b == -1 && a == math.MinInt64 {
		return true
	}
	return a > math.MaxInt64/b || a < math.MinInt64/b
}

func addInt64Checked(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "overflow")
	}
	return a + b, nil
}

func subInt64Checked(a, b int64) (int64, error) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "overflow")
	}
	return a - b, nil
}

func mulInt64Checked(a, b int64) (int64, error) {
	if a != 0 && b != 0 && mulOverflows(a, b) {
		return 0, NewError(CodeFixedPointOverflow, "domain.fixed", 0, "overflow")
	}
	return a * b, nil
}

// Between reports whether f lies within [lo, hi] inclusive, comparing at a
// common scale with round-half-away alignment.
func (f Fixed) Between(lo, hi Fixed) bool {
	common := f.Scale
	if lo.Scale > common {
		common = lo.Scale
	}
	if hi.Scale > common {
		common = hi.Scale
	}
	a, errA := f.Rescale(common)
	b, errB := lo.Rescale(common)
	c, errC := hi.Rescale(common)
	if errA != nil || errB != nil || errC != nil {
		return false
	}
	return a.Raw >= b.Raw && a.Raw <= c.Raw
}
