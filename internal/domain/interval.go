package domain

import "sort"

// Interval is a closed half-open integer interval [Start, End) in integer
// micrometers. A valid interval satisfies 0 <= Start < End. Groove zones and
// weld passes must continuously cover the locked design weld and may not
// overlap except where the rules explicitly allow boundary contact.
type Interval struct {
	Start Micrometers `json:"start"`
	End   Micrometers `json:"end"`
}

// Validate checks negativity and degenerate (zero or inverted) intervals.
func (i Interval) Validate() *DomainError {
	if i.Start < 0 || i.End < 0 {
		return NewError(CodeIntervalOverlap, "domain.interval", 0, "negative endpoint")
	}
	if i.Start >= i.End {
		return NewError(CodeIntervalOverlap, "domain.interval", 0, "degenerate interval")
	}
	return nil
}

// Length returns the integer micrometer length of a valid interval.
func (i Interval) Length() Micrometers {
	return i.End - i.Start
}

// Overlaps reports whether two intervals share a non-boundary overlap. Boundary
// contact (a.End == b.Start) is permitted.
func (i Interval) Overlaps(o Interval) bool {
	return i.Start < o.End && o.Start < i.End
}

// SortIntervals returns intervals sorted by start, then end, in place.
func SortIntervals(in []Interval) {
	sort.Slice(in, func(a, b int) bool {
		if in[a].Start != in[b].Start {
			return in[a].Start < in[b].Start
		}
		return in[a].End < in[b].End
	})
}

// CoverResult is the outcome of validating that a set of intervals
// continuously covers a design interval without illegal overlap.
type CoverResult struct {
	Gaps     []Interval
	Overlaps []Interval
}

// ValidateCoverage validates that the intervals continuously cover [start,end)
// with no illegal overlap. It returns the offending gaps and overlaps (empty
// when the coverage is valid).
func ValidateCoverage(start, end Micrometers, intervals []Interval) (CoverResult, *DomainError) {
	if start < 0 || end <= start {
		return CoverResult{}, NewError(CodeIntervalGap, "domain.interval", 0, "invalid design interval")
	}
	sorted := append([]Interval(nil), intervals...)
	SortIntervals(sorted)

	var res CoverResult
	cursor := start
	for _, iv := range sorted {
		if err := iv.Validate(); err != nil {
			return CoverResult{}, err
		}
		if iv.Start > cursor {
			res.Gaps = append(res.Gaps, Interval{Start: cursor, End: iv.Start})
		}
		if iv.Start < cursor {
			res.Overlaps = append(res.Overlaps, Interval{Start: iv.Start, End: cursor})
		}
		if iv.End > cursor {
			cursor = iv.End
		}
	}
	if cursor < end {
		res.Gaps = append(res.Gaps, Interval{Start: cursor, End: end})
	}
	if len(res.Gaps) > 0 || len(res.Overlaps) > 0 {
		code := CodeIntervalGap
		if len(res.Overlaps) > 0 {
			code = CodeIntervalOverlap
		}
		return res, NewError(code, "domain.interval", 0,
			coverageReasons(res)...)
	}
	return res, nil
}

func coverageReasons(res CoverResult) []string {
	var reasons []string
	for _, g := range res.Gaps {
		reasons = append(reasons, "gap ["+itoa(int64(g.Start))+","+itoa(int64(g.End))+")")
	}
	for _, o := range res.Overlaps {
		reasons = append(reasons, "overlap ["+itoa(int64(o.Start))+","+itoa(int64(o.End))+")")
	}
	return reasons
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
