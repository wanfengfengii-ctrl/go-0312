package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/httpapi"
)

func TestModel_ResponseWriterIsolation(t *testing.T) {
	type requestCase struct {
		name        string
		method      string
		path        string
		operationID string
		body        string
		status      int
		assert      func(*testing.T, any)
	}

	requests := []requestCase{
		{
			name:   "health success",
			method: http.MethodGet,
			path:   "/api/health",
			status: http.StatusOK,
			assert: func(t *testing.T, payload any) {
				body, ok := payload.(map[string]any)
				if !ok || body["status"] != "ok" || body["service"] != "truss-thickplate-weld-restraint-release" {
					t.Fatalf("unexpected health payload: %#v", payload)
				}
			},
		},
		{
			name:   "task list success",
			method: http.MethodGet,
			path:   "/api/tasks",
			status: http.StatusOK,
			assert: func(t *testing.T, payload any) {
				body, ok := payload.([]any)
				if !ok || len(body) != 0 {
					t.Fatalf("unexpected task list payload: %#v", payload)
				}
			},
		},
		{
			name:        "material success",
			method:      http.MethodPost,
			path:        "/api/material/operations",
			operationID: "material-register",
			body:        `{"type":"REGISTER","package_id":"PKG-1","batch_id":"B-1","spec":"E7018","grams":25,"timestamp":1}`,
			status:      http.StatusOK,
			assert: func(t *testing.T, payload any) {
				body, ok := payload.(map[string]any)
				if !ok || body["package_id"] != "PKG-1" || body["balance"] != float64(25) || body["disposition"] != "STOCK" {
					t.Fatalf("unexpected material payload: %#v", payload)
				}
			},
		},
		{
			name:        "lease success",
			method:      http.MethodPost,
			path:        "/api/leases/acquire",
			operationID: "lease-acquire",
			body:        `{"resource_id":"oven-1","operation":"hold","start":10,"end":20}`,
			status:      http.StatusOK,
			assert: func(t *testing.T, payload any) {
				body, ok := payload.(map[string]any)
				id, idOK := body["id"].(string)
				if !ok || !idOK || id == "" || body["resource_id"] != "oven-1" || body["operation"] != "hold" || body["version"] != float64(1) {
					t.Fatalf("unexpected lease payload: %#v", payload)
				}
			},
		},
		{
			name:        "evidence stable error",
			method:      http.MethodPost,
			path:        "/api/tasks/missing/evidence",
			operationID: "evidence-missing-task",
			body:        `{"kind":"VISUAL","generation":0,"logical_time":7}`,
			status:      http.StatusUnprocessableEntity,
			assert: func(t *testing.T, payload any) {
				body, ok := payload.(map[string]any)
				reasons, reasonsOK := body["reasons"].([]any)
				if !ok || body["code"] != "PREFIX_VIOLATION" || body["path"] != "service.evidence" || body["logical_time"] != float64(0) || !reasonsOK || len(reasons) != 1 {
					t.Fatalf("unexpected evidence error payload: %#v", payload)
				}
			},
		},
	}

	tests := []struct {
		name       string
		concurrent bool
	}{
		{name: "consecutive requests"},
		{name: "concurrent requests", concurrent: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httpapi.NewServer()
			handler := server.Handler()

			run := func(t *testing.T, test requestCase) {
				t.Helper()
				req := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
				if test.operationID != "" {
					req.Header.Set("Operation-Id", test.operationID+"-"+tc.name)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)

				if response.Code != test.status {
					t.Fatalf("status = %d, want %d; body = %q", response.Code, test.status, response.Body.String())
				}
				if got := response.Header().Get("Content-Type"); got != "application/json" {
					t.Fatalf("Content-Type = %q, want application/json", got)
				}
				var payload any
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatalf("response is not a complete JSON body: %q: %v", response.Body.String(), err)
				}
				test.assert(t, payload)
			}

			if !tc.concurrent {
				for _, test := range requests {
					run(t, test)
				}
				return
			}

			var wg sync.WaitGroup
			for _, test := range requests {
				test := test
				wg.Add(1)
				go func() {
					defer wg.Done()
					run(t, test)
				}()
			}
			wg.Wait()
		})
	}
}
