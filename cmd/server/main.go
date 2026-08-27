// Command server runs the truss thick-plate weld restraint-release web service:
// a Go HTTP backend that serves the documented JSON API and the embedded
// deterministic frontend build, persisted in an embedded SQL database so that
// task generations, material ledgers, leases, device calls, pass prefixes and
// terminal credentials survive a restart.
package main

import (
	"log"
	"net/http"
	"os"

	"truss-thickplate-weld-restraint-release/internal/httpapi"
	"truss-thickplate-weld-restraint-release/internal/store"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./truss-weld.db"
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := httpapi.NewServerWithStore(st)
	log.Printf("listening on %s (db=%s)", addr, dbPath)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
