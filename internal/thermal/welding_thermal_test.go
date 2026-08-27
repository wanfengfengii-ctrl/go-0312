package thermal

import (
	"testing"

	"truss-thickplate-weld-restraint-release/internal/domain"
)

func TestLineEnergy(t *testing.T) {
	// I = 200 A, U = 30 V, v = 5 mm/s => line energy = 1200 J/mm at scale 0.
	current := domain.MustFixed(200, 0)
	voltage := domain.MustFixed(30, 0)
	speed := domain.MustFixed(5, 0)
	got, err := LineEnergy(current, voltage, speed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Raw != 1200 || got.Scale != 0 {
		t.Fatalf("line energy = %d (scale %d), want 1200 scale 0", got.Raw, got.Scale)
	}
}

func TestLineEnergyRejectsZeroSpeed(t *testing.T) {
	_, err := LineEnergy(domain.MustFixed(200, 0), domain.MustFixed(30, 0), domain.MustFixed(0, 0))
	derr, ok := err.(*domain.DomainError)
	if !ok || derr.Code != domain.CodeFixedPointOverflow {
		t.Fatalf("expected FIXED_POINT_OVERFLOW, got %v", err)
	}
}

func TestFixedRoundHalfAwayFromZero(t *testing.T) {
	// 1.25 at scale 2 -> scale 1 should round to 1.3 (half away from zero).
	f := domain.MustFixed(125, 2)
	got, err := f.Rescale(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Raw != 13 {
		t.Fatalf("round half away = %d, want 13", got.Raw)
	}

	// Negative half away: -1.25 -> -1.3.
	n := domain.MustFixed(-125, 2)
	got, err = n.Rescale(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Raw != -13 {
		t.Fatalf("negative round half away = %d, want -13", got.Raw)
	}
}

func TestCumulativeHeatInput(t *testing.T) {
	energies := []domain.Fixed{
		domain.MustFixed(1200, 0),
		domain.MustFixed(800, 0),
		domain.MustFixed(50, 0),
	}
	got, err := CumulativeHeatInput(energies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Raw != 2050 {
		t.Fatalf("cumulative = %d, want 2050", got.Raw)
	}
}

func TestMulOverflow(t *testing.T) {
	big := domain.MustFixed(1<<40, 0)
	_, err := big.Mul(big)
	derr, ok := err.(*domain.DomainError)
	if !ok || derr.Code != domain.CodeFixedPointOverflow {
		t.Fatalf("expected FIXED_POINT_OVERFLOW, got %v", err)
	}
}
