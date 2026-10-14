#!/usr/bin/env bash
# CP-11 y RNF-03: levanta el sistema con Docker Compose (por HTTPS, con un
# certificado efímero) y ejecuta la prueba de carga con 30 bots durante 60 s,
# en dos variantes:
#   1. Línea base (CP-11 puro, sin ranking en segundo plano)
#   2. Con GET /api/ranking en bucle (RNF-03): su p95 se compara con el de la
#      línea base (BOTS_LOAD_BASELINE_REPORT); sin línea base RNF-03 no se evalúa.
#
# Uso:
#   tests/load/run.sh
#
# Variables opcionales:
#   LOAD_DURATION        duración de cada prueba (por defecto 60s)
#   LOAD_BOTS            número de bots (por defecto 30)
#   LOAD_VARIANT         both | baseline | ranking (por defecto both); «ranking»
#                        solo sirve con un load-baseline.json previo en REPORTS_DIR
#   LOAD_RANKING_INTERVAL  espera entre consultas de ranking (BOTS_LOAD_RANKING_INTERVAL,
#                        2s por defecto)
#   REPORTS_DIR          directorio de salida de los JSON (por defecto ./reports,
#                        ignorado por git: son artefactos, no resultados versionados)
#   LOAD_SKIP_BUILD=1    no construir imágenes, usar las precargadas (CI)
#   LOAD_KEEP=1          no bajar el stack al terminar (para depuración)
#   HV_HTTP_PORT         puerto del host para nginx (8080)
# Requiere Docker con Compose v2 y openssl.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-hydrovalley-load}"
# compose.tls.yml monta el certificado efímero en nginx y en los bots.
export COMPOSE_FILE="${repo_root}/deploy/compose.yml:${repo_root}/tests/e2e/compose.tls.yml"
export COMPOSE_ENV_FILES="${repo_root}/deploy/.env.example"
export HV_HTTP_PORT="${HV_HTTP_PORT:-8080}"
export HV_BASE_URL="https://localhost:${HV_HTTP_PORT}"

LOAD_DURATION="${LOAD_DURATION:-60s}"
LOAD_BOTS="${LOAD_BOTS:-30}"
LOAD_VARIANT="${LOAD_VARIANT:-both}"
REPORTS_DIR="${REPORTS_DIR:-${repo_root}/reports}"

mkdir -p "${REPORTS_DIR}"
chmod 777 "${REPORTS_DIR}" 2>/dev/null || true

# Certificado TLS, claves y administrador efímeros, y limpieza: comunes con
# tests/e2e/run.sh (ver tests/lib/stack.sh).
source "${repo_root}/tests/lib/stack.sh"
STACK_TOOL="run.sh"
STACK_SKIP_BUILD="${LOAD_SKIP_BUILD:-0}"
STACK_KEEP="${LOAD_KEEP:-0}"
STACK_KEEP_NAME="LOAD_KEEP"
stack_init_tls
stack_init_keys
stack_init_admin "load-admin"
trap stack_cleanup EXIT

echo "--- levantando servicios de hydrovalley ---"
up_args=(up -d --quiet-pull)
if [[ "${LOAD_SKIP_BUILD:-0}" != "1" ]]; then
  up_args+=(--build)
fi
docker compose "${up_args[@]}"

# Espera a que los 5 servicios reporten 'up' en /api/system (hasta 300 s)
start_wait=$SECONDS
healthy=false
while (( SECONDS - start_wait < 300 )); do
  if [[ "$(curl -fsS --cacert "${NODE_EXTRA_CA_CERTS}" --max-time 3 "${HV_BASE_URL}/api/system" 2>/dev/null | grep -o '"status":"up"' | wc -l | tr -d ' ')" == "5" ]]; then
    healthy=true
    break
  fi
  sleep 1
done

if [[ "${healthy}" != "true" ]]; then
  echo "run.sh: los servicios no estuvieron listos a tiempo" >&2
  exit 1
fi
echo "--- servicios listos y saludables en $((SECONDS - start_wait)) s ---"

bots_image="${HV_REGISTRY:-ghcr.io/iadev2005}/hydrovalley-bots:${HV_TAG:-dev}"
if [[ "${LOAD_SKIP_BUILD:-0}" != "1" ]] || ! docker image inspect "${bots_image}" >/dev/null 2>&1; then
  docker compose --profile bots build bots
fi

# Los bots llegan a nginx por https://nginx:8080 y confían en el certificado
# efímero (HV_CA_CERT, ver tests/e2e/compose.tls.yml).
bots_load() {
  local report="$1"
  shift
  docker compose --profile bots run --rm \
    -e HV_ADMIN_USERNAME="${ADMIN_USERNAME}" \
    -e HV_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD}" \
    -e BOTS_COUNT="${LOAD_BOTS}" \
    -e BOTS_DURATION="${LOAD_DURATION}" \
    -e BOTS_RESET=true \
    -e BOTS_REPORT_PATH="/reports/${report}" \
    -v "${REPORTS_DIR}:/reports" \
    "$@" \
    bots load-test
}

run_passed=true

# 1. Variante Línea Base (CP-11 sin ranking)
if [[ "${LOAD_VARIANT}" == "both" || "${LOAD_VARIANT}" == "baseline" ]]; then
  echo ""
  echo "========================================================================"
  echo " 1. PRUEBA DE CARGA: LÍNEA BASE CP-11 (sin ranking)"
  echo "    Bots: ${LOAD_BOTS} · Duración: ${LOAD_DURATION}"
  echo "========================================================================"
  if ! bots_load load-baseline.json; then
    echo "AVISO: la prueba de carga base no superó los umbrales exigidos" >&2
    run_passed=false
  fi
fi

# 2. Variante Con Ranking (RNF-03)
if [[ "${LOAD_VARIANT}" == "both" || "${LOAD_VARIANT}" == "ranking" ]]; then
  echo ""
  echo "========================================================================"
  echo " 2. PRUEBA DE CARGA CON RANKING EN BUCLE: RNF-03"
  echo "    Bots: ${LOAD_BOTS} · Duración: ${LOAD_DURATION} · GET /api/ranking continuo"
  echo "========================================================================"
  ranking_env=(-e BOTS_LOAD_RANKING=true)
  if [[ -n "${LOAD_RANKING_INTERVAL:-}" ]]; then
    ranking_env+=(-e "BOTS_LOAD_RANKING_INTERVAL=${LOAD_RANKING_INTERVAL}")
  fi
  if [[ -f "${REPORTS_DIR}/load-baseline.json" ]]; then
    ranking_env+=(-e BOTS_LOAD_BASELINE_REPORT=/reports/load-baseline.json)
  else
    echo "AVISO: no hay ${REPORTS_DIR}/load-baseline.json: RNF-03 no se podrá evaluar" >&2
  fi
  if ! bots_load load-ranking.json "${ranking_env[@]}"; then
    echo "AVISO: la prueba de carga con ranking no superó los umbrales exigidos" >&2
    run_passed=false
  fi
fi

echo ""
echo "========================================================================"
echo " RESUMEN DE INFORMES GENERADOS"
echo "========================================================================"
ls -lh "${REPORTS_DIR}"/load-*.json 2>/dev/null || echo "No se encontraron informes JSON."

if [[ "${run_passed}" != "true" ]]; then
  echo "run.sh: Al menos una de las pruebas de carga falló." >&2
  exit 1
fi

echo "run.sh: Pruebas de carga finalizadas con éxito."
exit 0
