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
MOCK_OLLAMA_PORT="${MOCK_OLLAMA_PORT:-11435}"
# The local mocks listen on 127.0.0.1, which the SSRF guard blocks by default. Allow exactly
# that host (not ALLOW_LOCAL_TOOLS, which turns the guard off).
export AOS_TOOL_INTERNAL_HOSTS="${AOS_TOOL_INTERNAL_HOSTS:-127.0.0.1}"

cleanup() {
  trap - TERM INT EXIT
  [[ -n "${MOCK_PID:-}" ]] && kill "${MOCK_PID}" 2>/dev/null || true
  [[ -n "${AOS_PID:-}" ]] && kill "${AOS_PID}" 2>/dev/null || true
  [[ -n "${UI_PID:-}" ]] && kill "${UI_PID}" 2>/dev/null || true
  [[ -n "${MOCK_OLLAMA_PID:-}" ]] && kill "${MOCK_OLLAMA_PID}" 2>/dev/null || true
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
  demo)
    # Live-demo stack: like "all", but the LLM is the deterministic mock-ollama (no model,
    # no GPU, same answer every time). The advertised model name says it is a mock.
    export AOS_CALLBACK_BASE_URL="${AOS_CALLBACK_BASE_URL:-http://127.0.0.1:${AOS_PORT}}"
    export AOS_MOCK_BASE_URL="${AOS_MOCK_BASE_URL:-http://127.0.0.1:${MOCK_PORT}}"
    export AUDIT_LOG_PATH="${AUDIT_LOG_PATH-${ROOT_DIR}/data/aos-audit.jsonl}"
    export LLM_ENGINE=ollama
    export OLLAMA_BASE_URL="http://127.0.0.1:${MOCK_OLLAMA_PORT}"
    export LLM_MODEL="${MOCK_OLLAMA_MODEL:-aos-demo:deterministic}"
    export OLLAMA_INTENT_MODEL="${LLM_MODEL}"
    MOCK_OLLAMA_PORT="${MOCK_OLLAMA_PORT}" go run ./cmd/mock-ollama & MOCK_OLLAMA_PID=$!
    go run ./cmd/mocks & MOCK_PID=$!
    go run ./cmd/aos -port "${AOS_PORT}" & AOS_PID=$!
    if [[ -d "${UI_DIR}" ]]; then
      (cd "${UI_DIR}" && AOS_BACKEND_URL="${AOS_BACKEND_URL:-http://127.0.0.1:${AOS_PORT}}" npm run dev -- --host 0.0.0.0 --port "${UI_PORT}") & UI_PID=$!
    fi
    echo "AOS:         http://localhost:${AOS_PORT}"
    echo "Mock tools:  http://localhost:${MOCK_PORT}"
    echo "Mock Ollama: http://localhost:${MOCK_OLLAMA_PORT} (model ${LLM_MODEL})"
    [[ -n "${UI_PID:-}" ]] && echo "UI:          http://localhost:${UI_PORT}"
    wait "${AOS_PID}"
    ;;
  *)
    echo "Usage: $0 [all|backend|ui|demo]" >&2
    exit 2
    ;;
esac
