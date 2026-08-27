package httpapi

import (
	"embed"
	"io/fs"
	"net/http"

	"truss-thickplate-weld-restraint-release/internal/service"
	"truss-thickplate-weld-restraint-release/internal/store"
)

//go:embed all:dist
var distFS embed.FS

// Server wires the JSON API routes and the embedded frontend assets.
type Server struct {
	mux *http.ServeMux
	svc *service.Service
}

// NewServer builds a Server with the documented routes registered over an
// in-memory store, suitable for development and smoke testing.
func NewServer() *Server {
	st, err := store.Open(":memory:")
	if err != nil {
		panic(err)
	}
	return NewServerWithStore(st)
}

// NewServerWithStore builds a Server over an explicit store.
func NewServerWithStore(st store.Store) *Server {
	s := &Server{mux: http.NewServeMux(), svc: service.New(st)}
	s.routes()
	return s
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	// Catalog and task management.
	s.mux.HandleFunc("POST /api/catalog/revisions", s.requireOperation(s.createRevision))
	s.mux.HandleFunc("GET /api/catalog/revisions", s.listCatalog)
	s.mux.HandleFunc("POST /api/tasks", s.requireOperation(s.createTask))
	s.mux.HandleFunc("GET /api/tasks", s.listTasks)
	s.mux.HandleFunc("POST /api/tasks/{id}/lock", s.requireOperation(s.lockTask))
	s.mux.HandleFunc("GET /api/tasks/{id}", s.getTask)
	s.mux.HandleFunc("GET /api/tasks/{id}/graph", s.getGraph)

	// Material and leases.
	s.mux.HandleFunc("POST /api/material/operations", s.requireOperation(s.materialOperation))
	s.mux.HandleFunc("POST /api/leases/acquire", s.requireOperation(s.acquireLease))
	s.mux.HandleFunc("POST /api/leases/{id}/renew", s.requireOperation(s.renewLease))

	// Evidence and device calls.
	s.mux.HandleFunc("POST /api/tasks/{id}/evidence", s.requireOperation(s.writeEvidence))
	s.mux.HandleFunc("POST /api/device-calls/{id}/retry", s.requireOperation(s.retryDeviceCall))

	// Defects, repairs, retests, reviews, verdicts.
	s.mux.HandleFunc("POST /api/tasks/{id}/defects", s.requireOperation(s.createDefect))
	s.mux.HandleFunc("POST /api/tasks/{id}/repairs", s.requireOperation(s.createRepair))
	s.mux.HandleFunc("POST /api/tasks/{id}/retests", s.requireOperation(s.createRetest))
	s.mux.HandleFunc("POST /api/tasks/{id}/reviews", s.requireOperation(s.createReview))
	s.mux.HandleFunc("POST /api/tasks/{id}/verdicts", s.requireOperation(s.createVerdict))

	// Health for live backend-state display in the frontend.
	s.mux.HandleFunc("GET /api/health", s.health)

	s.serveDist()
}

// serveDist serves the embedded frontend build at the root path.
func (s *Server) serveDist() {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok",
				"note":   "frontend assets not built; run `npm run build` in web/",
			})
		})
		return
	}
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
}

// health reports live backend state for the frontend page.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"service":     "truss-thickplate-weld-restraint-release",
		"error_codes": len(domainErrorCodes()),
	})
}

func domainErrorCodes() []string {
	return []string{
		"STALE_REVISION", "HEAT_MISMATCH", "INTERVAL_GAP", "INTERVAL_OVERLAP",
		"GRAPH_CYCLE", "MATERIAL_OVERDRAWN", "CONTAINER_CONTAMINATION",
		"LEASE_CONFLICT", "LEASE_EXPIRED", "PREFIX_VIOLATION",
		"THERMAL_OUT_OF_RANGE", "EXPOSURE_EXPIRED", "DEVICE_RETRY_PENDING",
		"FIXED_POINT_OVERFLOW", "REPAIR_LIMIT", "IDEMPOTENCY_CONFLICT",
		"TERMINAL_CONFLICT",
	}
}

// requireOperation enforces the Operation-Id header on every write endpoint.
func (s *Server) requireOperation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Operation-Id") == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"code": "MISSING_OPERATION_ID",
			})
			return
		}
		next(w, r)
	}
}
