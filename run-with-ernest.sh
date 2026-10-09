#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${AOS_ERNEST_ENV_FILE:-${ROOT_DIR}/data/ernest-local.env}"

if [[ ! -f "${CONFIG_FILE}" ]]; then
  echo "Ernest integration config not found: ${CONFIG_FILE}" >&2
  exit 1
fi

set -a
# This generated local file contains only simple KEY=value assignments.
source "${CONFIG_FILE}"
set +a

exec "${ROOT_DIR}/run.sh" "${1:-all}"
