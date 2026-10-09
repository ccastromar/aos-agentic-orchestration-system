#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "${AOS_WORKSPACE_ROOT:-}" || "${AOS_WORKSPACE_ROOT}" == "/" ]]; then
  export AOS_WORKSPACE_ROOT="${ROOT_DIR}"
else
  export AOS_WORKSPACE_ROOT
fi
UI_DIR="${AOS_UI_DIR:-${ROOT_DIR}/../aos-ui}"
AOS_PORT="${AOS_PORT:-8080}"
MOCK_PORT="${AOS_MOCK_PORT:-9000}"
UI_PORT="${AOS_UI_PORT:-5173}"

cleanup() {
  trap - TERM INT EXIT
  [[ -n "${MOCK_PID:-}" ]] && kill "${MOCK_PID}" 2>/dev/null || true
  [[ -n "${AOS_PID:-}" ]] && kill "${AOS_PID}" 2>/dev/null || true
  [[ -n "${UI_PID:-}" ]] && kill "${UI_PID}" 2>/dev/null || true
}
trap cleanup TERM INT EXIT

case "${1:-all}" in
  backend)
    export AOS_CALLBACK_BASE_URL="${AOS_CALLBACK_BASE_URL:-http://127.0.0.1:${AOS_PORT}}"
    export AOS_MOCK_BASE_URL="${AOS_MOCK_BASE_URL:-http://127.0.0.1:${MOCK_PORT}}"
    export AUDIT_LOG_PATH="${AUDIT_LOG_PATH-${ROOT_DIR}/data/aos-audit.jsonl}"
    go run ./cmd/mocks & MOCK_PID=$!
    go run ./cmd/aos -port "${AOS_PORT}" & AOS_PID=$!
    wait "${AOS_PID}"
    ;;
  ui)
    [[ -d "${UI_DIR}" ]] || { echo "UI directory not found: ${UI_DIR}" >&2; exit 1; }
    (cd "${UI_DIR}" && AOS_BACKEND_URL="${AOS_BACKEND_URL:-http://127.0.0.1:${AOS_PORT}}" npm run dev -- --host 0.0.0.0 --port "${UI_PORT}")
    ;;
  all)
    export AOS_CALLBACK_BASE_URL="${AOS_CALLBACK_BASE_URL:-http://127.0.0.1:${AOS_PORT}}"
    export AOS_MOCK_BASE_URL="${AOS_MOCK_BASE_URL:-http://127.0.0.1:${MOCK_PORT}}"
    export AUDIT_LOG_PATH="${AUDIT_LOG_PATH-${ROOT_DIR}/data/aos-audit.jsonl}"
    go run ./cmd/mocks & MOCK_PID=$!
    go run ./cmd/aos -port "${AOS_PORT}" & AOS_PID=$!
    [[ -d "${UI_DIR}" ]] || { echo "UI directory not found: ${UI_DIR}" >&2; exit 1; }
    (cd "${UI_DIR}" && AOS_BACKEND_URL="${AOS_BACKEND_URL:-http://127.0.0.1:${AOS_PORT}}" npm run dev -- --host 0.0.0.0 --port "${UI_PORT}") & UI_PID=$!
    echo "AOS:  http://localhost:${AOS_PORT}"
    echo "Mock: http://localhost:${MOCK_PORT}"
    echo "UI:   http://localhost:${UI_PORT}"
    wait "${UI_PID}"
    ;;
  *)
    echo "Usage: $0 [all|backend|ui]" >&2
    exit 2
    ;;
esac
