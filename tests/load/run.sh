#!/usr/bin/env bash
# CP-11 y RNF-03: levanta el sistema con Docker Compose y ejecuta la prueba de
# carga con 30 bots durante 60 s, en dos variantes:
#   1. Línea base (CP-11 puro, sin ranking en segundo plano)
#   2. Con ranking en bucle (RNF-03, GET /api/ranking concurrente)
#
# Uso:
#   tests/load/run.sh
#
# Variables opcionales:
#   LOAD_DURATION        duración de cada prueba (por defecto 60s)
#   LOAD_BOTS            número de bots (por defecto 30)
#   LOAD_VARIANT         both | baseline | ranking (por defecto both)
#   REPORTS_DIR          directorio de salida de los JSON (por defecto ./reports)
#   LOAD_SKIP_BUILD=1    no construir imágenes, usar las precargadas (CI)
#   LOAD_KEEP=1          no bajar el stack al terminar (para depuración)
#   HV_HTTP_PORT         puerto del host para nginx (8080)
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-hydrovalley-load}"
export COMPOSE_FILE="${repo_root}/deploy/compose.yml"
export COMPOSE_ENV_FILES="${repo_root}/deploy/.env.example"
export HV_HTTP_PORT="${HV_HTTP_PORT:-8080}"
export HV_BASE_URL="http://localhost:${HV_HTTP_PORT}"

LOAD_DURATION="${LOAD_DURATION:-60s}"
LOAD_BOTS="${LOAD_BOTS:-30}"
LOAD_VARIANT="${LOAD_VARIANT:-both}"
REPORTS_DIR="${REPORTS_DIR:-${repo_root}/reports}"

mkdir -p "${REPORTS_DIR}"
chmod 777 "${REPORTS_DIR}" 2>/dev/null || true

# Clave Ed25519 efímera para la prueba: se genera en cada ejecución y nunca
# se guarda (RNF-13). Si ya viene definida en el entorno, se respeta.
if [[ -z "${TOKEN_PRIVATE_KEY:-}" ]]; then
  TOKEN_PRIVATE_KEY="$(openssl genpkey -algorithm ed25519 | base64 | tr -d '\n')"
  export TOKEN_PRIVATE_KEY
fi
if [[ -z "${TOKEN_PUBLIC_KEY:-}" ]]; then
  if [[ "${TOKEN_PRIVATE_KEY}" == *"-----BEGIN"* ]]; then
    private_pem="${TOKEN_PRIVATE_KEY//\\n/$'\n'}"
  else
    private_pem="$(printf '%s' "${TOKEN_PRIVATE_KEY}" | base64 --decode)"
  fi
  TOKEN_PUBLIC_KEY="$(printf '%s\n' "${private_pem}" | openssl pkey -pubout | base64 | tr -d '\n')"
  if [[ -z "${TOKEN_PUBLIC_KEY}" ]]; then
    echo "run.sh: no se pudo derivar TOKEN_PUBLIC_KEY de TOKEN_PRIVATE_KEY" >&2
    exit 1
  fi
  export TOKEN_PUBLIC_KEY
fi

# Cuenta de administrador efímera para aprovisionar los bots
if [[ -z "${ADMIN_PASSWORD_HASH:-}" ]]; then
  export ADMIN_USERNAME="load-admin"
  E2E_ADMIN_PASSWORD="$(openssl rand -hex 16)"
  export E2E_ADMIN_PASSWORD
  if [[ "${LOAD_SKIP_BUILD:-0}" != "1" ]]; then
    docker compose build api
  fi
  api_image="${HV_REGISTRY:-ghcr.io/iadev2005}/hydrovalley-api:${HV_TAG:-dev}"
  ADMIN_PASSWORD_HASH="$(printf '%s' "${E2E_ADMIN_PASSWORD}" | docker run --rm -i "${api_image}" node --input-type=module -e '
import { hash } from "@node-rs/argon2";
let pw = ""; for await (const c of process.stdin) pw += c;
console.log(await hash(pw, { algorithm: 2, memoryCost: 19456, timeCost: 2, parallelism: 1 }));')"
  if [[ "${ADMIN_PASSWORD_HASH}" != '$argon2id$'* ]]; then
    echo "run.sh: no se pudo calcular el hash Argon2id del administrador" >&2
    exit 1
  fi
  export ADMIN_PASSWORD_HASH
fi

cleanup() {
  local status=$?
  if [[ "${LOAD_KEEP:-0}" == "1" ]]; then
    echo "LOAD_KEEP=1: el stack sigue en marcha (docker compose -p ${COMPOSE_PROJECT_NAME} down -v)."
    exit "${status}"
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "--- estado y registros de los servicios ante fallo ---"
    docker compose ps -a || true
    docker compose logs --no-color --tail=80 || true
  fi
  echo "--- bajando stack de docker compose ---"
  docker compose down -v --remove-orphans || true
  exit "${status}"
}
trap cleanup EXIT

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
  if [[ "$(curl -fsS --max-time 3 "${HV_BASE_URL}/api/system" 2>/dev/null | grep -o '"status":"up"' | wc -l | tr -d ' ')" == "5" ]]; then
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

run_passed=true

# 1. Variante Línea Base (CP-11 sin ranking)
if [[ "${LOAD_VARIANT}" == "both" || "${LOAD_VARIANT}" == "baseline" ]]; then
  echo ""
  echo "========================================================================"
  echo " 1. PRUEBA DE CARGA: LÍNEA BASE CP-11 (sin ranking)"
  echo "    Bots: ${LOAD_BOTS} · Duración: ${LOAD_DURATION}"
  echo "========================================================================"
  if ! docker compose --profile bots run --rm \
    -e HV_BASE_URL="http://nginx:8080" \
    -e HV_ADMIN_USERNAME="${ADMIN_USERNAME}" \
    -e HV_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD}" \
    -e BOTS_COUNT="${LOAD_BOTS}" \
    -e BOTS_DURATION="${LOAD_DURATION}" \
    -e BOTS_RESET=true \
    -e BOTS_REPORT_PATH="/reports/load-baseline.json" \
    -v "${REPORTS_DIR}:/reports" \
    bots load-test; then
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
  if ! docker compose --profile bots run --rm \
    -e HV_BASE_URL="http://nginx:8080" \
    -e HV_ADMIN_USERNAME="${ADMIN_USERNAME}" \
    -e HV_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD}" \
    -e BOTS_COUNT="${LOAD_BOTS}" \
    -e BOTS_DURATION="${LOAD_DURATION}" \
    -e BOTS_RESET=true \
    -e BOTS_LOAD_RANKING=true \
    -e BOTS_REPORT_PATH="/reports/load-ranking.json" \
    -v "${REPORTS_DIR}:/reports" \
    bots load-test; then
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
