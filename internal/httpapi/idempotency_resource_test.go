package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/httpapi"
	"truss-thickplate-weld-restraint-release/internal/store"
)

func TestModel_OperationIDIsScopedToTaskLockResource(t *testing.T) {
	tests := []struct {
		name       string
		secondTask string
		wantStatus int
		wantCode   string
	}{
		{name: "same task safely replays", secondTask: "T1", wantStatus: http.StatusOK},
		{name: "different task conflicts", secondTask: "T2", wantStatus: http.StatusConflict, wantCode: "IDEMPOTENCY_CONFLICT"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			handler := httpapi.NewServerWithStore(st).Handler()

			do := func(method, path, operationID, body string) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
				if operationID != "" {
					req.Header.Set("Operation-Id", operationID)
				}
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				handler.ServeHTTP(res, req)
				return res
			}
			mustOK := func(method, path, operationID, body string) *httptest.ResponseRecorder {
				t.Helper()
				res := do(method, path, operationID, body)
				if res.Code != http.StatusOK {
					t.Fatalf("%s %s: status %d, body %s", method, path, res.Code, res.Body.String())
				}
				return res
			}

			revision := `{"id":"R1","design_id":"D1","design_version":1,"process_id":"P1","process_version":1,"effective_time":1,"material_rules":[{"heat_number":"H-100","thickness":100,"batch_id":"B1","batch_spec":"S"}]}`
			mustOK(http.MethodPost, "/api/catalog/revisions", "create-revision", revision)
			for _, id := range []string{"T1", "T2"} {
				body := fmt.Sprintf(`{"id":%q,"zone":"Z","component":"C","node":"N","design_end":100}`, id)
				mustOK(http.MethodPost, "/api/tasks", "create-"+id, body)
			}

			lockBody := `{"design_id":"D1","design_version":1,"process_id":"P1","process_version":1,"revision_id":"R1","section_heat":"H-100","section_thickness":100,"groove_zones":[{"id":"z1","side":"A","interval":{"start":0,"end":100}}],"passes":[{"id":"A1","side":"A","sequence":1,"layer_id":"L1","zone_id":"z1","heat":"H-100","holding":"HG-1","interval":{"start":0,"end":100}}]}`
			first := mustOK(http.MethodPost, "/api/tasks/T1/lock", "shared-lock-operation", lockBody)

			second := do(http.MethodPost, "/api/tasks/"+tc.secondTask+"/lock", "shared-lock-operation", lockBody)
			if second.Code != tc.wantStatus {
				t.Fatalf("second lock status = %d, want %d; first body %s, second body %s", second.Code, tc.wantStatus, first.Body.String(), second.Body.String())
			}
			if tc.wantCode != "" {
				var failure struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(second.Body.Bytes(), &failure); err != nil {
					t.Fatalf("decode conflict response: %v", err)
				}
				if failure.Code != tc.wantCode {
					t.Fatalf("second lock code = %q, want %q; body %s", failure.Code, tc.wantCode, second.Body.String())
				}
			} else if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
				t.Fatalf("safe replay changed response: first %s, replay %s", first.Body.String(), second.Body.String())
			}

			for id, wantStatus := range map[string]string{"T1": "LOCKED", "T2": "DRAFT"} {
				res := mustOK(http.MethodGet, "/api/tasks/"+id, "", "")
				var taskState struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				}
				if err := json.Unmarshal(res.Body.Bytes(), &taskState); err != nil {
					t.Fatalf("decode task %s: %v", id, err)
				}
				if taskState.ID != id || taskState.Status != wantStatus {
					t.Fatalf("task %s state = {id:%q status:%q}, want {id:%q status:%q}", id, taskState.ID, taskState.Status, id, wantStatus)
				}
			}
		})
	}
}
