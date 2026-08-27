package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/service"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
)

// operationID returns the Operation-Id header value.
func operationID(r *http.Request) string { return r.Header.Get("Operation-Id") }

// decodeBody decodes a JSON request body, rejecting empty or malformed input.
func decodeBody(r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("empty request body")
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil && err != io.EOF {
		return err
	}
	return nil
}

// respond writes a service result or a domain error with the stable contract.
func (s *Server) respond(w http.ResponseWriter, result any, err error) {
	if err != nil {
		var de *domain.DomainError
		if errors.As(err, &de) {
			writeDomainError(w, de)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, domain.NewError("NOT_FOUND", "httpapi", 0, "resource not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, domain.NewError("INTERNAL", "httpapi", 0, err.Error()))
		return
	}
	writeResult(w, result)
}

func (s *Server) createRevision(w http.ResponseWriter, r *http.Request) {
	var req service.CreateRevisionRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.catalog", 0, err.Error()))
		return
	}
	res, err := s.svc.CreateRevision(r.Context(), operationID(r), req)
	s.respond(w, res, err)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTaskRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.task", 0, err.Error()))
		return
	}
	res, err := s.svc.CreateTask(r.Context(), operationID(r), req)
	s.respond(w, res, err)
}

func (s *Server) lockTask(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	var req service.LockTaskRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.task.lock", 0, err.Error()))
		return
	}
	res, err := s.svc.LockTask(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	res, err := s.svc.GetTask(r.Context(), id)
	s.respond(w, res, err)
}

func (s *Server) getGraph(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	res, err := s.svc.GetGraph(r.Context(), id)
	s.respond(w, res, err)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.ListTasks(r.Context())
	s.respond(w, res, err)
}

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.LatestRevision(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeResult(w, []any{})
			return
		}
		s.respond(w, nil, err)
		return
	}
	writeResult(w, res)
}

func (s *Server) materialOperation(w http.ResponseWriter, r *http.Request) {
	var req service.MaterialOperationRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.material", 0, err.Error()))
		return
	}
	res, err := s.svc.MaterialOperation(r.Context(), operationID(r), req)
	s.respond(w, res, err)
}

func (s *Server) acquireLease(w http.ResponseWriter, r *http.Request) {
	var req service.AcquireLeaseRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.lease", 0, err.Error()))
		return
	}
	res, err := s.svc.AcquireLease(r.Context(), operationID(r), req)
	s.respond(w, res, err)
}

func (s *Server) renewLease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req service.RenewLeaseRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.lease.renew", 0, err.Error()))
		return
	}
	res, err := s.svc.RenewLease(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) writeEvidence(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	var req service.EvidenceRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.evidence", 0, err.Error()))
		return
	}
	res, err := s.svc.WriteEvidence(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) retryDeviceCall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req service.RetryDeviceCallRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.device", 0, err.Error()))
		return
	}
	res, err := s.svc.RetryDeviceCall(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) createDefect(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	var req service.CreateDefectRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.defect", 0, err.Error()))
		return
	}
	res, err := s.svc.CreateDefect(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) createRepair(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	var req service.CreateRepairRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.repair", 0, err.Error()))
		return
	}
	res, err := s.svc.CreateRepair(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) createRetest(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	var req service.CreateRetestRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.retest", 0, err.Error()))
		return
	}
	res, err := s.svc.CreateRetest(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) createReview(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	var req service.CreateReviewRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.review", 0, err.Error()))
		return
	}
	res, err := s.svc.CreateReview(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}

func (s *Server) createVerdict(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	var req service.CreateVerdictRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError("BAD_REQUEST", "httpapi.verdict", 0, err.Error()))
		return
	}
	res, err := s.svc.CreateVerdict(r.Context(), operationID(r), id, req)
	s.respond(w, res, err)
}
