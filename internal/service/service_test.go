package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/repair"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

func newMemService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func TestMaterialIssueRollback(t *testing.T) {
	svc := newMemService(t)
	ctx := context.Background()

	if _, err := svc.MaterialOperation(ctx, "reg-a", MaterialOperationRequest{Type: OpRegister, PackageID: "P-A", BatchID: "B-A", Grams: 1000}); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if _, err := svc.MaterialOperation(ctx, "reg-b", MaterialOperationRequest{Type: OpRegister, PackageID: "P-B", BatchID: "B-B", Grams: 500}); err != nil {
		t.Fatalf("register B: %v", err)
	}
	if _, err := svc.MaterialOperation(ctx, "dry-a", MaterialOperationRequest{Type: OpDry, PackageID: "P-A", OvenID: "OV-1", Timestamp: 10}); err != nil {
		t.Fatalf("dry A: %v", err)
	}

	_, err := svc.MaterialOperation(ctx, "issue-b", MaterialOperationRequest{Type: OpIssue, PackageID: "P-B", Grams: 100, OvenID: "OV-1", LeaseStart: 20, LeaseEnd: 100, Timestamp: 20})
	var de *domain.DomainError
	if !errors.As(err, &de) || de.Code != domain.CodeContainerContam {
		t.Fatalf("expected CONTAINER_CONTAMINATION, got %v", err)
	}

	ledger, _ := svc.store.Ledger(ctx, "P-B")
	if got := ledger.Balance(); got != 500 {
		t.Fatalf("package B balance changed after rollback: %d", got)
	}
	leases, _ := svc.store.Leases(ctx, "OV-1")
	if len(leases) != 0 {
		t.Fatalf("lease leaked after rollback: %+v", leases)
	}
}

func TestIdempotencyReplayAndConflict(t *testing.T) {
	svc := newMemService(t)
	ctx := context.Background()

	if _, err := svc.MaterialOperation(ctx, "reg-1", MaterialOperationRequest{Type: OpRegister, PackageID: "P-1", BatchID: "B-1", Grams: 1000}); err != nil {
		t.Fatalf("register: %v", err)
	}
	issue := MaterialOperationRequest{Type: OpIssue, PackageID: "P-1", Grams: 300, Timestamp: 5}

	first, err := svc.MaterialOperation(ctx, "issue-1", issue)
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}
	replay, err := svc.MaterialOperation(ctx, "issue-1", issue)
	if err != nil {
		t.Fatalf("replay issue: %v", err)
	}
	if replay.Balance != first.Balance {
		t.Fatalf("replay changed balance: %d != %d", replay.Balance, first.Balance)
	}

	issue.Grams = 999
	_, err = svc.MaterialOperation(ctx, "issue-1", issue)
	var de *domain.DomainError
	if !errors.As(err, &de) || de.Code != domain.CodeIdempotencyConflict {
		t.Fatalf("expected IDEMPOTENCY_CONFLICT, got %v", err)
	}
}

func TestDeviceRetrySequence(t *testing.T) {
	svc := newMemService(t)
	ctx := context.Background()
	seedRevision(t, svc)
	tsk := lockTask(t, svc)

	ev := EvidenceRequest{TaskID: "T1", Kind: "PREHEAT", Generation: 1, LogicalTime: 1, Temperature: domain.MustFixed(150, 0), Coverage: domain.MustFixed(100, 0), ResourceID: "TC-1"}
	res, err := svc.WriteEvidence(ctx, "preheat-1", tsk.ID, ev)
	if err != nil {
		t.Fatalf("preheat evidence write: %v", err)
	}
	if res.Accepted || res.DeviceCallID == "" {
		t.Fatalf("expected pending device call, got %+v", res)
	}

	r1, err := svc.RetryDeviceCall(ctx, "retry-1", res.DeviceCallID, RetryDeviceCallRequest{LogicalTime: 2})
	if err != nil || r1.Outcome != material.ScriptMalformed {
		t.Fatalf("expected malformed pending, got %v %v", r1, err)
	}

	r2, err := svc.RetryDeviceCall(ctx, "retry-2", res.DeviceCallID, RetryDeviceCallRequest{LogicalTime: 3})
	if err != nil || r2.Outcome != material.ScriptSuccess {
		t.Fatalf("expected success, got %v %v", r2, err)
	}
	barrier, _ := svc.store.ThermalBarrier(ctx, tsk.ID)
	if !barrier.Established {
		t.Fatal("thermal barrier not established after device success")
	}
}

func TestVerdictSingleWinner(t *testing.T) {
	svc := newMemService(t)
	ctx := context.Background()
	seedRevision(t, svc)
	tsk := releaseReadyTask(t, svc)

	got, err := svc.CreateVerdict(ctx, "verdict-1", tsk.ID, CreateVerdictRequest{Type: "RELEASE", CreatedAt: 100})
	if err != nil {
		t.Fatalf("release verdict: %v", err)
	}
	if got.Credential == "" {
		t.Fatal("release verdict missing credential")
	}

	_, err = svc.CreateVerdict(ctx, "verdict-2", tsk.ID, CreateVerdictRequest{Type: "CRACK_RISK_ISOLATION", CreatedAt: 101})
	var de *domain.DomainError
	if !errors.As(err, &de) || de.Code != domain.CodeTerminalConflict {
		t.Fatalf("expected TERMINAL_CONFLICT, got %v", err)
	}
}

func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "restart.db")

	st, err := store.Open(db)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	svc := New(st)
	ctx := context.Background()

	if _, err := svc.MaterialOperation(ctx, "reg-1", MaterialOperationRequest{Type: OpRegister, PackageID: "P-1", BatchID: "B-1", Grams: 1000}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.AcquireLease(ctx, "lease-1", AcquireLeaseRequest{ResourceID: "W-1", Operation: "WELD", Start: 0, End: 100}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	st.Close()

	st2, err := store.Open(db)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	ledger, err := st2.Ledger(ctx, "P-1")
	if err != nil || ledger.Balance() != 1000 {
		t.Fatalf("ledger not recovered: %d %v", ledger.Balance(), err)
	}
	leases, err := st2.Leases(ctx, "W-1")
	if err != nil || len(leases) != 1 || leases[0].ResourceID != "W-1" || leases[0].Version != 1 {
		t.Fatalf("lease not recovered: %+v %v", leases, err)
	}
}

func seedRevision(t *testing.T, svc *Service) {
	t.Helper()
	_, err := svc.CreateRevision(context.Background(), "rev-1", CreateRevisionRequest{
		ID: "R1", DesignID: "D1", DesignVersion: 1, ProcessID: "P1", ProcessVersion: 1, EffectiveTime: 1,
		MaterialRules: []catalog.MaterialRule{
			{HeatNumber: "H-100", Thickness: 30000000, BatchID: "B-1", BatchSpec: "ER50-6"},
		},
		ThresholdSets: []catalog.ThresholdSet{
			{ID: "T1", InterpassMin: domain.MustFixed(100, 0), InterpassMax: domain.MustFixed(300, 0), PreheatCoverage: domain.MustFixed(100, 0)},
		},
		Qualifications: []catalog.Qualification{
			{PersonID: "alice", Role: "WELD_INSPECTOR", ValidFrom: 0, ValidTo: 1000000},
			{PersonID: "bob", Role: "WELD_INSPECTOR", ValidFrom: 0, ValidTo: 1000000},
		},
	})
	if err != nil {
		t.Fatalf("seed revision: %v", err)
	}
}

func lockTask(t *testing.T, svc *Service) task.NodeTask {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.CreateTask(ctx, "task-1", CreateTaskRequest{ID: "T1", Zone: "Z", Component: "C", Node: "N", DesignEnd: 1000000}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	tsk, err := svc.LockTask(ctx, "lock-1", "T1", LockTaskRequest{
		DesignID: "D1", DesignVersion: 1, ProcessID: "P1", ProcessVersion: 1, RevisionID: "R1",
		SectionHeat: "H-100", SectionThickness: 30000000,
		GrooveZones: []task.GrooveZone{{ID: "z1", Side: task.SideA, Interval: domain.Interval{Start: 0, End: 1000000}}},
		Passes:      []task.WeldPass{{ID: "A1", Side: task.SideA, Sequence: 1, LayerID: "L1", ZoneID: "z1", Heat: "H-100", Holding: "HG-1", Interval: domain.Interval{Start: 0, End: 1000000}}},
	})
	if err != nil {
		t.Fatalf("lock task: %v", err)
	}
	return tsk
}

// releaseReadyTask builds a task that satisfies every release precondition:
// closed prefix, established barrier, visual and ultrasonic inspection and two
// qualified reviews.
func releaseReadyTask(t *testing.T, svc *Service) task.NodeTask {
	t.Helper()
	ctx := context.Background()
	tsk := lockTask(t, svc)

	if err := svc.store.SaveThermalBarrier(ctx, tsk.ID, true, 1); err != nil {
		t.Fatalf("save barrier: %v", err)
	}
	if err := svc.store.SavePassPrefix(ctx, tsk.ID, []string{"A1"}, 1); err != nil {
		t.Fatalf("save prefix: %v", err)
	}
	for i, kind := range []thermal.EvidenceKind{thermal.KindVisual, thermal.KindUltrasonic} {
		ev := thermal.EvidenceEvent{ID: newID(), TaskID: string(tsk.ID), Generation: 1, Kind: kind, LogicalTime: domain.Milliseconds(50 + i)}
		if err := svc.store.AppendEvidence(ctx, ev); err != nil {
			t.Fatalf("append evidence: %v", err)
		}
	}
	for _, p := range []string{"alice", "bob"} {
		r := repair.Review{ID: newID(), TaskID: string(tsk.ID), PersonID: p, Role: "WELD_INSPECTOR", Qualified: true, EvidenceHash: "h", CreatedAt: 60}
		if err := svc.store.CreateReview(ctx, r); err != nil {
			t.Fatalf("create review: %v", err)
		}
	}
	return tsk
}
