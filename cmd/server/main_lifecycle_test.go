package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type modelServerProcess struct {
	cmd  *exec.Cmd
	done chan error
	logs bytes.Buffer
}

func startModelServer(t *testing.T, dbPath string) (*modelServerProcess, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve server address: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release server address: %v", err)
	}

	proc := &modelServerProcess{done: make(chan error, 1)}
	proc.cmd = exec.Command(os.Args[0], "-test.run=^TestModel_ServerKeepsStoreAlive$")
	proc.cmd.Env = append(os.Environ(),
		"MODEL_SERVER_HELPER=1",
		"ADDR="+addr,
		"DB_PATH="+dbPath,
	)
	proc.cmd.Stdout = &proc.logs
	proc.cmd.Stderr = &proc.logs
	if err := proc.cmd.Start(); err != nil {
		t.Fatalf("start server command: %v", err)
	}
	go func() { proc.done <- proc.cmd.Wait() }()

	url := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := (&http.Client{Timeout: 200 * time.Millisecond}).Get(url + "/api/health")
		if err == nil {
			_ = resp.Body.Close()
			return proc, url
		}
		select {
		case err := <-proc.done:
			t.Fatalf("server command exited before listening: %v\n%s", err, proc.logs.String())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = proc.cmd.Process.Kill()
	<-proc.done
	t.Fatalf("server command did not listen on %s\n%s", addr, proc.logs.String())
	return nil, ""
}

func stopModelServer(t *testing.T, proc *modelServerProcess) {
	t.Helper()
	if proc == nil || proc.cmd.Process == nil {
		return
	}
	if err := proc.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt server command: %v", err)
	}
	select {
	case <-proc.done:
	case <-time.After(5 * time.Second):
		_ = proc.cmd.Process.Kill()
		<-proc.done
		t.Fatal("server command did not exit after interrupt")
	}
}

func TestModel_ServerKeepsStoreAlive(t *testing.T) {
	if os.Getenv("MODEL_SERVER_HELPER") == "1" {
		main()
		return
	}

	dbPath := filepath.Join(t.TempDir(), "server.db")
	proc, baseURL := startModelServer(t, dbPath)
	running := true
	t.Cleanup(func() {
		if running {
			stopModelServer(t, proc)
		}
	})

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		operation  string
		restart    bool
		wantStatus int
		wantBody   string
	}{
		{name: "health remains available", method: http.MethodGet, path: "/api/health", wantStatus: http.StatusOK, wantBody: `"status":"ok"`},
		{name: "task reads use the live database", method: http.MethodGet, path: "/api/tasks", wantStatus: http.StatusOK, wantBody: `[]`},
		{name: "transactional task writes use the live database", method: http.MethodPost, path: "/api/tasks", body: `{"id":"T1","zone":"Z","component":"C","node":"N","design_end":1000}`, operation: "create-T1", wantStatus: http.StatusOK, wantBody: `"id":"T1"`},
		{name: "written tasks can be read", method: http.MethodGet, path: "/api/tasks", wantStatus: http.StatusOK, wantBody: `"id":"T1"`},
		{name: "file database is restored after restart", method: http.MethodGet, path: "/api/tasks", restart: true, wantStatus: http.StatusOK, wantBody: `"id":"T1"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.restart {
				stopModelServer(t, proc)
				running = false
				proc, baseURL = startModelServer(t, dbPath)
				running = true
			}

			req, err := http.NewRequest(tc.method, baseURL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tc.operation != "" {
				req.Header.Set("Operation-Id", tc.operation)
			}
			resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			data, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				t.Fatalf("read response: %v", readErr)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("%s %s status = %d, want %d; body=%s", tc.method, tc.path, resp.StatusCode, tc.wantStatus, data)
			}
			if !bytes.Contains(data, []byte(tc.wantBody)) {
				t.Fatalf("%s %s body = %s, want it to contain %s", tc.method, tc.path, data, fmt.Sprintf("%q", tc.wantBody))
			}
		})
	}
}
