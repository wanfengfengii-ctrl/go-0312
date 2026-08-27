package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/httpapi"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/store"
)

func TestModel_LeaseRenewalExclusivity(t *testing.T) {
	tests := []struct {
		name           string
		newEnd         int64
		version        int64
		wantStatus     int
		wantCode       string
		wantPersistEnd int64
		replay         bool
	}{
		{
			name:           "renewal crossing another lease is rejected without mutation",
			newEnd:         150,
			version:        1,
			wantStatus:     http.StatusConflict,
			wantCode:       "LEASE_CONFLICT",
			wantPersistEnd: 100,
		},
		{
			name:           "renewal ending at another lease boundary is allowed and replayable",
			newEnd:         120,
			version:        1,
			wantStatus:     http.StatusOK,
			wantPersistEnd: 120,
			replay:         true,
		},
		{
			name:           "renewal with a stale version remains rejected without mutation",
			newEnd:         110,
			version:        2,
			wantStatus:     http.StatusConflict,
			wantCode:       "LEASE_CONFLICT",
			wantPersistEnd: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			h := httpapi.NewServerWithStore(st).Handler()

			doPost := func(path, operationID string, body any) (int, []byte) {
				t.Helper()
				payload, err := json.Marshal(body)
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Operation-Id", operationID)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				res := rec.Result()
				defer res.Body.Close()
				got, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatalf("read response: %v", err)
				}
				return res.StatusCode, got
			}

			acquire := func(operationID string, start, end int64) material.Lease {
				t.Helper()
				status, body := doPost("/api/leases/acquire", operationID, map[string]any{
					"resource_id": "W-1",
					"operation":   "WELD",
					"start":       start,
					"end":         end,
				})
				if status != http.StatusOK {
					t.Fatalf("acquire [%d,%d): status %d, body %s", start, end, status, body)
				}
				var lease material.Lease
				if err := json.Unmarshal(body, &lease); err != nil {
					t.Fatalf("decode acquired lease: %v", err)
				}
				return lease
			}

			first := acquire("acquire-first", 0, 100)
			_ = acquire("acquire-second", 120, 200)

			status, body := doPost("/api/leases/acquire", "acquire-overlap", map[string]any{
				"resource_id": "W-1",
				"operation":   "WELD",
				"start":       90,
				"end":         130,
			})
			var conflict struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(body, &conflict); err != nil {
				t.Fatalf("decode acquire conflict: %v", err)
			}
			if status != http.StatusConflict || conflict.Code != "LEASE_CONFLICT" {
				t.Fatalf("overlapping acquire: status %d, code %q, body %s", status, conflict.Code, body)
			}

			renewPath := fmt.Sprintf("/api/leases/%s/renew", first.ID)
			renewRequest := map[string]any{"new_end": tt.newEnd, "version": tt.version}
			status, body = doPost(renewPath, "renew-first", renewRequest)
			if status != tt.wantStatus {
				t.Fatalf("renew status = %d, want %d; body %s", status, tt.wantStatus, body)
			}
			if tt.wantCode != "" {
				var got struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode renewal error: %v", err)
				}
				if got.Code != tt.wantCode {
					t.Fatalf("renew code = %q, want %q; body %s", got.Code, tt.wantCode, body)
				}
			}

			persisted, err := st.Lease(context.Background(), first.ID)
			if err != nil {
				t.Fatalf("load renewed lease: %v", err)
			}
			if int64(persisted.End) != tt.wantPersistEnd {
				t.Fatalf("persisted end = %d, want %d", persisted.End, tt.wantPersistEnd)
			}

			if tt.replay {
				replayStatus, replayBody := doPost(renewPath, "renew-first", renewRequest)
				if replayStatus != status || !bytes.Equal(replayBody, body) {
					t.Fatalf("same Operation-Id replay changed response: first=(%d,%s) replay=(%d,%s)", status, body, replayStatus, replayBody)
				}
			}
		})
	}
}
