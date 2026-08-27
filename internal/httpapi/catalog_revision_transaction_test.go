package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/httpapi"
	"truss-thickplate-weld-restraint-release/internal/store"
)

func TestModel_CatalogRevisionPublishing(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	handler := httpapi.NewServerWithStore(st).Handler()

	tests := []struct {
		name       string
		opID       string
		body       string
		wantStatus int
		wantCode   string
		wantLatest string
		wantAbsent string
	}{
		{
			name:       "first revision is published",
			opID:       "catalog-r1",
			body:       `{"id":"R1","design_id":"D1","design_version":1,"process_id":"P1","process_version":1,"effective_time":100}`,
			wantStatus: http.StatusOK,
			wantLatest: "R1",
		},
		{
			name:       "later revision is published",
			opID:       "catalog-r2",
			body:       `{"id":"R2","design_id":"D2","design_version":2,"process_id":"P2","process_version":2,"effective_time":200}`,
			wantStatus: http.StatusOK,
			wantLatest: "R2",
		},
		{
			name:       "same operation safely replays",
			opID:       "catalog-r2",
			body:       `{"id":"R2","design_id":"D2","design_version":2,"process_id":"P2","process_version":2,"effective_time":200}`,
			wantStatus: http.StatusOK,
			wantLatest: "R2",
		},
		{
			name:       "same operation with different content conflicts",
			opID:       "catalog-r2",
			body:       `{"id":"R3","design_id":"D3","design_version":3,"process_id":"P3","process_version":3,"effective_time":300}`,
			wantStatus: http.StatusConflict,
			wantCode:   "IDEMPOTENCY_CONFLICT",
			wantLatest: "R2",
			wantAbsent: "R3",
		},
		{
			name:       "older revision is stale and is not stored",
			opID:       "catalog-stale",
			body:       `{"id":"R0","design_id":"D0","design_version":0,"process_id":"P0","process_version":0,"effective_time":50}`,
			wantStatus: http.StatusConflict,
			wantCode:   "STALE_REVISION",
			wantLatest: "R2",
			wantAbsent: "R0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "/api/catalog/revisions", bytes.NewBufferString(tt.body)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Operation-Id", tt.opID)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)

			if response.Code != tt.wantStatus {
				t.Fatalf("POST status = %d, want %d; body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
			if tt.wantCode != "" {
				var failure struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if failure.Code != tt.wantCode {
					t.Fatalf("error code = %q, want %q", failure.Code, tt.wantCode)
				}
			}

			latestResponse := httptest.NewRecorder()
			handler.ServeHTTP(latestResponse, httptest.NewRequest(http.MethodGet, "/api/catalog/revisions", nil))
			if latestResponse.Code != http.StatusOK {
				t.Fatalf("GET latest status = %d; body=%s", latestResponse.Code, latestResponse.Body.String())
			}
			var latest struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(latestResponse.Body.Bytes(), &latest); err != nil {
				t.Fatalf("decode latest revision: %v", err)
			}
			if latest.ID != tt.wantLatest {
				t.Fatalf("latest revision = %q, want %q", latest.ID, tt.wantLatest)
			}
			if tt.wantAbsent != "" {
				if _, err := st.Revision(context.Background(), catalog.RevisionID(tt.wantAbsent)); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("revision %q was stored; want store.ErrNotFound, got %v", tt.wantAbsent, err)
				}
			}
		})
	}
}
