#!/usr/bin/env bash
# Smoke test for the truss thick-plate weld restraint-release service.
# Builds the binary, starts the service on a local port with a temporary
# database, drives a real catalog->task->lock API flow, then tears down every
# process and temporary file. No external network access is required.
set -euo pipefail

PORT="${PORT:-18080}"
ADDR="127.0.0.1:${PORT}"
BASE="http://${ADDR}"

WORKDIR="$(mktemp -d)"
BIN="$WORKDIR/server"
DB="$WORKDIR/smoke.db"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

echo "building server..."
go build -o "$BIN" ./cmd/server

echo "starting server on $ADDR ..."
DB_PATH="$DB" ADDR="$ADDR" "$BIN" &
SERVER_PID=$!

# Wait for the health endpoint to come up.
ready=""
for _ in $(seq 1 50); do
  if resp="$(curl -sS "http://${ADDR}/api/health" 2>/dev/null || true)"; then
    if printf '%s' "$resp" | grep -q '"status":"ok"'; then
      ready="$resp"
      break
    fi
  fi
  sleep 0.1
done
if [[ -z "$ready" ]]; then
  echo "server did not become healthy" >&2
  exit 1
fi
echo "health: $ready"

# 1. Create a rule-catalog revision.
rev_resp="$(curl -sS -X POST "$BASE/api/catalog/revisions" \
  -H 'Content-Type: application/json' \
  -H 'Operation-Id: smoke-rev-1' \
  -d '{"id":"R1","design_id":"D1","design_version":1,"process_id":"P1","process_version":1,"effective_time":100,"material_rules":[{"heat_number":"H-100","thickness":30000000,"batch_id":"B-1","batch_spec":"ER50-6"}],"threshold_sets":[{"id":"T1","interpass_min":{"raw":100,"scale":0},"interpass_max":{"raw":300,"scale":0},"preheat_coverage":{"raw":100,"scale":0},"exposure_limit":600000,"stop_work_limit":3600000}],"qualifications":[{"person_id":"alice","role":"WELD_INSPECTOR","valid_from":0,"valid_to":999999999}]}')"
printf '%s' "$rev_resp" | grep -q '"id":"R1"' || { echo "catalog revision failed: $rev_resp" >&2; exit 1; }
echo "catalog revision created"

# 2. Create a node welding task.
task_resp="$(curl -sS -X POST "$BASE/api/tasks" \
  -H 'Content-Type: application/json' \
  -H 'Operation-Id: smoke-task-1' \
  -d '{"id":"T1","zone":"Z-A","component":"C-1","node":"N-1","design_end":1000000,"groove_zones":[{"id":"z1","side":"A","interval":{"start":0,"end":1000000}},{"id":"z2","side":"B","interval":{"start":0,"end":1000000}}],"passes":[{"id":"A1","side":"A","sequence":1,"layer_id":"L1","zone_id":"z1","heat":"H-100","holding":"HG-1","interval":{"start":0,"end":1000000}},{"id":"B1","side":"B","sequence":1,"layer_id":"L1","zone_id":"z2","heat":"H-100","holding":"HG-1","interval":{"start":0,"end":1000000}}]}')"
printf '%s' "$task_resp" | grep -q '"id":"T1"' || { echo "task create failed: $task_resp" >&2; exit 1; }
echo "task created"

# 3. Lock the task into an immutable generation.
lock_resp="$(curl -sS -X POST "$BASE/api/tasks/T1/lock" \
  -H 'Content-Type: application/json' \
  -H 'Operation-Id: smoke-lock-1' \
  -d '{"design_id":"D1","design_version":1,"process_id":"P1","process_version":1,"revision_id":"R1","section_heat":"H-100","section_thickness":30000000,"groove_zones":[{"id":"z1","side":"A","interval":{"start":0,"end":1000000}},{"id":"z2","side":"B","interval":{"start":0,"end":1000000}}],"passes":[{"id":"A1","side":"A","sequence":1,"layer_id":"L1","zone_id":"z1","heat":"H-100","holding":"HG-1","interval":{"start":0,"end":1000000}},{"id":"B1","side":"B","sequence":1,"layer_id":"L1","zone_id":"z2","heat":"H-100","holding":"HG-1","interval":{"start":0,"end":1000000}}]}')"
printf '%s' "$lock_resp" | grep -q '"status":"LOCKED"' || { echo "task lock failed: $lock_resp" >&2; exit 1; }
echo "task locked"

# 4. Read back the task and graph to confirm the immutable snapshot.
get_resp="$(curl -sS "$BASE/api/tasks/T1")"
printf '%s' "$get_resp" | grep -q '"generation":1' || { echo "task read failed: $get_resp" >&2; exit 1; }
graph_resp="$(curl -sS "$BASE/api/tasks/T1/graph")"
printf '%s' "$graph_resp" | grep -q '"task_id":"T1"' || { echo "graph read failed: $graph_resp" >&2; exit 1; }
echo "task and graph read back (generation 1)"

echo "SMOKE OK"
