package domain

// Micrometers is an integer measure of length. All lengths, thicknesses,
// root faces, gaps, leg sizes, interval endpoints and defect coordinates use
// integer micrometers and must never be negative.
type Micrometers int64

// Grams is an integer measure of mass. Consumable welding material is
// accounted in integer grams only; fractional grams are forbidden.
type Grams int64

// Milliseconds is an integer measure of logical time. Leases, evidence and
// device calls are ordered by this clock.
type Milliseconds int64
