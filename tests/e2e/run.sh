#!/usr/bin/env bash
# CP-01-lite: levanta el plan B con Docker Compose, ejecuta smoke.test.mjs
# contra nginx y lo baja todo, también los volúmenes, pase lo que pase.
#
#   tests/e2e/run.sh
#
# Variables opcionales:
#   HV_HTTP_PORT        puerto del host para nginx (8080)
#   E2E_SKIP_BUILD=1    no construir: usar las imágenes ya cargadas (CI)
#   E2E_KEEP=1          no bajar el stack al terminar (para depurar)
# Requiere Docker con Compose v2 y Node 22 o superior.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

export COMPOSE_PROJECT_NAME=hydrovalley-e2e
export COMPOSE_FILE="${repo_root}/deploy/compose.yml"
# Valores de ejemplo con una base desechable: nunca el deploy/.env real.
export COMPOSE_ENV_FILES="${repo_root}/deploy/.env.example"
export HV_HTTP_PORT="${HV_HTTP_PORT:-8080}"
export HV_BASE_URL="http://localhost:${HV_HTTP_PORT}"

cleanup() {
  local status=$?
  if [[ "${E2E_KEEP:-0}" == "1" ]]; then
    echo "E2E_KEEP=1: el stack sigue en marcha (docker compose -p ${COMPOSE_PROJECT_NAME} down -v)."
    exit "${status}"
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "--- estado y registros de los servicios ---"
    docker compose ps -a || true
    docker compose logs --no-color --tail=80 || true
  fi
  docker compose down -v --remove-orphans || true
  exit "${status}"
}
trap cleanup EXIT

up_args=(up -d --quiet-pull)
if [[ "${E2E_SKIP_BUILD:-0}" != "1" ]]; then
  up_args+=(--build)
fi
docker compose "${up_args[@]}"

node --test --test-reporter=spec "${repo_root}/tests/e2e/smoke.test.mjs"
