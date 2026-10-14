#!/usr/bin/env bash
# CP-01-lite: levanta el plan B con Docker Compose, ejecuta smoke.test.mjs
# contra nginx y lo baja todo, también los volúmenes, pase lo que pase.
# Fases del flujo por defecto, en este orden:
#   1. smoke.test.mjs (humo, CP-02/03 y demás).
#   2. cp01-concurrent-drops.test.mjs: CP-01 (100 gotas simultáneas, RF-08), en
#      su propia invocación de `node --test` y con jugadores de
#      POST /api/admin/bot-accounts, para no gastar el límite de registro.
#   3. tls, request-id y los casos de fallos (CP-05 a CP-08 y CP-12).
#   4. cp04-firefighter.test.mjs: CP-04 con incendios reales, siempre al final:
#      baja el stack y lo levanta de nuevo, con base de datos vacía, sumando
#      tests/e2e/compose.cp04.yml (monta tests/e2e/fixtures/catalog.test-fire.json,
#      con ignition_chance_ppm alto, y fija CATALOG_PATH), y corre solo ese
#      archivo. La imagen y catalog/catalog.json no se tocan.
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
# request-id.test.mjs sigue un identificador de petición en los registros.
#   E2E_SKIP_CP04=1     no correr la fase de CP-04 (la del catálogo de prueba)
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
# compose.cp04.yml monta de aquí el catálogo de prueba (solo la fase de CP-04).
export E2E_FIXTURES_DIR="${repo_root}/tests/e2e/fixtures"

# Certificado TLS, claves y administrador efímeros, y limpieza: comunes con
# tests/load/run.sh (ver tests/lib/stack.sh).
source "${repo_root}/tests/lib/stack.sh"
STACK_TOOL="run.sh"
STACK_SKIP_BUILD="${E2E_SKIP_BUILD:-0}"
STACK_KEEP="${E2E_KEEP:-0}"
STACK_KEEP_NAME="E2E_KEEP"
stack_init_tls
stack_init_keys
stack_init_admin "e2e-admin"
trap stack_cleanup EXIT

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
rnf10_wait="$(stack_wait_all_up 300)"
if [[ -n "${rnf10_wait}" ]]; then
  rnf10_healthy=$((rnf10_up + rnf10_wait))
fi
echo "MEDICION RNF-10: compose_up_s=${rnf10_up} all_up_s=${rnf10_healthy:-n/d} build=$([[ "${E2E_SKIP_BUILD:-0}" == "1" ]] && echo no || echo si) limite_s=300"
# Los bots viven bajo el perfil «bots» y `up` no los construye: smoke.test.mjs
# los lanza con `compose run`, así que su imagen tiene que existir.
if [[ "${E2E_SKIP_BUILD:-0}" != "1" ]]; then
  docker compose --profile bots build bots
fi

# Si le pasas argumentos, ejecuta solo los archivos indicados y termina.
if [[ $# -gt 0 ]]; then
  node --test --test-reporter=spec "$@"
  exit 0
fi

# Flujo por defecto (sin argumentos): corre toda la suite.
node --test --test-reporter=spec "${repo_root}/tests/e2e/smoke.test.mjs"

# CP-01 (RF-08): aparte del humo, porque `node --test` corre los archivos a la
# vez y juntos gastarían el cupo de registros públicos; sus jugadores salen de
# POST /api/admin/bot-accounts.
node --test --test-reporter=spec "${repo_root}/tests/e2e/cp01-concurrent-drops.test.mjs"

# RNF-12: TLS en nginx (redirección de HTTP, versiones, healthcheck).
node --test --test-reporter=spec "${repo_root}/tests/e2e/tls.test.mjs"

# RNF-07: el identificador de petición se sigue en los registros de nginx, la
# API, la persistencia y el simulador.
node --test --test-reporter=spec "${repo_root}/tests/e2e/request-id.test.mjs"

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

# CP-04 (RF-29, RF-30): incendios reales. Va al final porque cambia el catálogo
# y necesita un valle nuevo (una sola casilla plantada, sin restos de otras
# pruebas): baja el stack con sus volúmenes y lo levanta de nuevo sumando
# tests/e2e/compose.cp04.yml, que monta el catálogo de prueba de solo lectura y
# fija CATALOG_PATH en la persistencia. Ni la imagen ni deploy/compose.yml ni
# catalog/catalog.json cambian. Para repetirla a mano: levanta el stack con ese
# archivo en COMPOSE_FILE y corre el archivo de la prueba (ver su cabecera).
if [[ "${E2E_SKIP_CP04:-0}" != "1" ]]; then
  docker compose down -v --remove-orphans
  # Exportado: stack_cleanup y los `docker compose` de la prueba (bots) ven la
  # misma configuración que el stack levantado.
  export COMPOSE_FILE="${COMPOSE_FILE}:${repo_root}/tests/e2e/compose.cp04.yml"
  docker compose up -d --quiet-pull
  cp04_up="$(stack_wait_all_up 300)"
  if [[ -z "${cp04_up}" ]]; then
    echo "run.sh: el stack del catálogo de prueba no quedó listo en 300 s" >&2
    exit 1
  fi
  echo "MEDICION CP-04 (preparación): catalogo=catalog.test-fire.json all_up_s=${cp04_up}"
  node --test --test-reporter=spec "${repo_root}/tests/e2e/cp04-firefighter.test.mjs"
fi
