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
#   E2E_SKIP_FAULTS=1   no correr los casos de fallos (faults*.test.mjs: paran y
#                       pausan servicios del stack; tardan unos minutos)
#   ADMIN_USERNAME, ADMIN_PASSWORD_HASH, E2E_ADMIN_PASSWORD  administrador
#                       propio; sin ellas se genera uno efímero
# smoke.test.mjs también corre el bot tramposo (imagen de los bots, perfil
# «bots») con el administrador efímero.
# nginx sirve HTTPS: run.sh genera un certificado efímero (openssl) y lo monta
# con tests/e2e/compose.tls.yml; ver tls.test.mjs.
# Requiere Docker con Compose v2, openssl y Node 22 o superior.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

export COMPOSE_PROJECT_NAME=hydrovalley-e2e
# compose.tls.yml monta el certificado efímero (E2E_TLS_DIR) en nginx.
export COMPOSE_FILE="${repo_root}/deploy/compose.yml:${repo_root}/tests/e2e/compose.tls.yml"
# Valores de ejemplo con una base desechable: nunca el deploy/.env real.
export COMPOSE_ENV_FILES="${repo_root}/deploy/.env.example"
export HV_HTTP_PORT="${HV_HTTP_PORT:-8080}"
export HV_BASE_URL="https://localhost:${HV_HTTP_PORT}"

# Certificado TLS efímero para nginx (RNF-12): se genera en cada ejecución, se
# monta en nginx y las pruebas confían en él con NODE_EXTRA_CA_CERTS (los
# bots, con HV_CA_CERT; ver compose.tls.yml): nunca se omite la verificación.
# Directorio y clave legibles por cualquiera porque nginx corre con el usuario
# 101; es una clave desechable que se borra al terminar.
E2E_TLS_DIR="$(mktemp -d)"
export E2E_TLS_DIR
chmod 755 "${E2E_TLS_DIR}"
openssl ecparam -genkey -name prime256v1 -noout -out "${E2E_TLS_DIR}/tls.key"
openssl req -new -x509 -key "${E2E_TLS_DIR}/tls.key" -out "${E2E_TLS_DIR}/tls.crt" \
  -days 2 -subj "/CN=hydrovalley-e2e" \
  -addext "subjectAltName=DNS:localhost,DNS:nginx,IP:127.0.0.1" \
  -addext "extendedKeyUsage=serverAuth"
chmod 644 "${E2E_TLS_DIR}/tls.key" "${E2E_TLS_DIR}/tls.crt"
export NODE_EXTRA_CA_CERTS="${E2E_TLS_DIR}/tls.crt"

# Clave Ed25519 efímera para la prueba: se genera en cada ejecución y nunca
# se guarda (RNF-13). Si ya viene definida en el entorno, se respeta.
if [[ -z "${TOKEN_PRIVATE_KEY:-}" ]]; then
  TOKEN_PRIVATE_KEY="$(openssl genpkey -algorithm ed25519 | base64 | tr -d '\n')"
  export TOKEN_PRIVATE_KEY
fi
# La pública sale de la privada, para el hub (base64 del PEM, una línea). La
# privada puede venir como PEM o como el base64 del PEM.
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

# Los parámetros de Argon2id (m=19456, t=2, p=1) deben ser los de la API:
# src/api/src/passwords.ts.
# Cuenta de administrador para la prueba (RF-31): contraseña efímera y su hash
# Argon2id, calculado con la imagen de la API (como en deploy/README.md). Ni la
# contraseña ni el hash se guardan en disco. Si ya vienen definidos en el
# entorno, se respetan (en ese caso E2E_ADMIN_PASSWORD también debe venir).
if [[ -z "${ADMIN_PASSWORD_HASH:-}" ]]; then
  export ADMIN_USERNAME="e2e-admin"
  E2E_ADMIN_PASSWORD="$(openssl rand -hex 16)"
  export E2E_ADMIN_PASSWORD
  if [[ "${E2E_SKIP_BUILD:-0}" != "1" ]]; then
    # La imagen de la API tiene que existir antes de calcular el hash.
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
  rm -rf "${E2E_TLS_DIR}"
  exit "${status}"
}
trap cleanup EXIT

up_args=(up -d --quiet-pull)
if [[ "${E2E_SKIP_BUILD:-0}" != "1" ]]; then
  up_args+=(--build)
fi
# RNF-10: tiempo de `compose up` hasta que /api/system da los cinco servicios
# `up` (con imágenes ya cargadas si E2E_SKIP_BUILD=1; con construcción incluye
# el build, y entonces la cifra no es la de «levantar desde las imágenes»).
rnf10_start=$SECONDS
docker compose "${up_args[@]}"
rnf10_up=$((SECONDS - rnf10_start))
rnf10_healthy=""
while (( SECONDS - rnf10_start < 300 )); do
  if [[ "$(curl -fsS --cacert "${NODE_EXTRA_CA_CERTS}" --max-time 3 "${HV_BASE_URL}/api/system" 2>/dev/null | grep -o '"status":"up"' | wc -l | tr -d ' ')" == "5" ]]; then
    rnf10_healthy=$((SECONDS - rnf10_start))
    break
  fi
  sleep 1
done
echo "MEDICION RNF-10: compose_up_s=${rnf10_up} all_up_s=${rnf10_healthy:-n/d} build=$([[ "${E2E_SKIP_BUILD:-0}" == "1" ]] && echo no || echo si) limite_s=300"
# Los bots viven bajo el perfil «bots» y `up` no los construye: smoke.test.mjs
# los lanza con `compose run`, así que su imagen tiene que existir.
if [[ "${E2E_SKIP_BUILD:-0}" != "1" ]]; then
  docker compose --profile bots build bots
fi

node --test --test-reporter=spec "${repo_root}/tests/e2e/smoke.test.mjs"

# RNF-12: TLS en nginx (redirección de HTTP, versiones, healthcheck).
node --test --test-reporter=spec "${repo_root}/tests/e2e/tls.test.mjs"

# CP-05 a CP-08 y CP-12: fallos de servicios (faults*.test.mjs; CP-08, el más
# disruptivo, va al final). Corren después de la prueba de
# humo, con el mismo stack y el administrador efímero.
if [[ "${E2E_SKIP_FAULTS:-0}" != "1" ]]; then
  node --test --test-reporter=spec --test-concurrency=1 \
    "${repo_root}/tests/e2e/faults.test.mjs" \
    "${repo_root}/tests/e2e/faults-hub-persistence.test.mjs"
  # CP-08 para todos los servicios y los levanta en otro orden: va aparte y al
  # final (node ordena los archivos por nombre, así que no basta con listarlo).
  node --test --test-reporter=spec --test-concurrency=1 \
    "${repo_root}/tests/e2e/faults-startup.test.mjs"
fi
