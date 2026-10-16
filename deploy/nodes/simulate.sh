#!/usr/bin/env bash
# Simulación del plan A en una sola máquina (Mac o Linux).
#
# Levanta los cinco nodos de deploy/nodes/ como cinco proyectos de Compose
# distintos (hv-sim-<nodo>), cada uno con su red, y hace que se hablen SOLO por
# puertos publicados del anfitrión (host.docker.internal), como si fueran
# máquinas distintas. Después:
#   1. espera a que /api/system dé los cinco servicios en `up`;
#   2. humo: acceso del administrador, GET /api/catalog y una gota por WSS;
#   3. fallo (CP-07): para el nodo persistence, comprueba que un jugador
#      conectado sigue jugando, lo arranca de nuevo y espera los cinco en `up`;
#   4. baja todo, con sus volúmenes.
#
#   deploy/nodes/simulate.sh
#
# Variables opcionales:
#   HV_TAG            etiqueta de las imágenes (local por defecto: se construyen
#                     de esta copia con `docker buildx bake`)
#   HV_REGISTRY       registro de las imágenes (ghcr.io/iadev2005)
#   SIM_SKIP_BUILD=1  no construir: usar las imágenes que ya existan con HV_TAG
#   SIM_SKIP_FAULT=1  omitir la fase de fallo
#   SIM_KEEP=1        no bajar los nodos al terminar
#   SIM_PORT_DB, SIM_PORT_PERSISTENCE, SIM_PORT_WORLD, SIM_PORT_HUB,
#   SIM_PORT_PROXY    puertos del anfitrión (por defecto 15433, 50071, 50061,
#                     18081 y 18480: no chocan con el 8080 del plan B ni con
#                     los 50051 de los nodos reales)
#
# Solo toca proyectos hv-sim-*. Requiere Docker con Compose v2 y buildx,
# openssl, curl y Node 22 o superior.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
nodes_dir="${repo_root}/deploy/nodes"
cd "${repo_root}"

export HV_TAG="${HV_TAG:-local}"
export HV_REGISTRY="${HV_REGISTRY:-ghcr.io/iadev2005}"
port_db="${SIM_PORT_DB:-15433}"
port_persistence="${SIM_PORT_PERSISTENCE:-50071}"
port_world="${SIM_PORT_WORLD:-50061}"
port_hub="${SIM_PORT_HUB:-18081}"
port_proxy="${SIM_PORT_PROXY:-18480}"
nodes=(db persistence world hub proxy)
wait_limit=300

# Certificado, claves y administrador efímeros: las mismas piezas que la e2e.
source "${repo_root}/tests/lib/stack.sh"
# shellcheck disable=SC2034 # las lee tests/lib/stack.sh
STACK_TOOL="simulate.sh"
# shellcheck disable=SC2034
STACK_SKIP_BUILD=1 # las imágenes las construye este script (o ya existen)

tmp="$(mktemp -d)"
chmod 700 "${tmp}"
addresses="${tmp}/lab-addresses.local"
secrets="${tmp}/secrets.local"

# Compose de un nodo: proyecto propio, su archivo y el de extra_hosts.
node_compose() {
  local node="$1"
  shift
  docker compose -p "hv-sim-${node}" -f "${nodes_dir}/${node}.yml" -f "${tmp}/${node}.override.yml" \
    --env-file "${addresses}" --env-file "${secrets}" "$@"
}

cleanup() {
  local status=$?
  trap - EXIT
  if [[ "${SIM_KEEP:-0}" == "1" ]]; then
    echo "SIM_KEEP=1: los nodos siguen en marcha (docker compose -p hv-sim-<nodo> down -v)."
    echo "Variables en ${tmp}"
    exit "${status}"
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "--- estado y registros de los nodos ---"
    for node in "${nodes[@]}"; do
      node_compose "${node}" ps -a 2>/dev/null || true
      node_compose "${node}" logs --no-color --tail=40 2>/dev/null || true
    done
  fi
  for node in "${nodes[@]}"; do
    node_compose "${node}" down -v --remove-orphans >/dev/null 2>&1 || true
  done
  rm -rf "${tmp}"
  exit "${status}"
}
trap cleanup EXIT

total_start=$SECONDS

# 1. Imágenes locales de esta copia (linux/<arquitectura del anfitrión>).
build_s=0
if [[ "${SIM_SKIP_BUILD:-0}" != "1" ]]; then
  echo "== Construyendo las imágenes (HV_TAG=${HV_TAG})"
  t=$SECONDS
  # Valores de relleno: bake solo necesita que las variables obligatorias existan.
  BUILDX_BAKE_ENTITLEMENTS_FS=0 POSTGRES_USER=bake POSTGRES_PASSWORD=bake POSTGRES_DB=bake \
    TOKEN_PRIVATE_KEY=bake TOKEN_PUBLIC_KEY=bake \
    docker buildx bake -f deploy/compose.yml --set '*.context=.' --progress=quiet --load \
    persistence world hub api nginx
  build_s=$((SECONDS - t))
fi
for svc in persistence world hub api proxy; do
  if ! docker image inspect "${HV_REGISTRY}/hydrovalley-${svc}:${HV_TAG}" >/dev/null 2>&1; then
    echo "simulate.sh: falta la imagen ${HV_REGISTRY}/hydrovalley-${svc}:${HV_TAG}" >&2
    exit 1
  fi
done

# 2. Variables del plan A: cada nodo llama a los demás por host.docker.internal
# y sus puertos publicados.
cat > "${addresses}" <<ADDR
LAB_DB_HOST=host.docker.internal
LAB_PERSISTENCE_HOST=host.docker.internal
LAB_WORLD_HOST=host.docker.internal
LAB_HUB_HOST=host.docker.internal
LAB_PROXY_HOST=host.docker.internal
HV_DB_PORT=${port_db}
HV_PERSISTENCE_PORT=${port_persistence}
HV_WORLD_PORT=${port_world}
HV_HUB_PORT=${port_hub}
HV_PROXY_PORT=${port_proxy}
ADDR

stack_init_keys
stack_init_admin "sim-admin"
pg_password="$(openssl rand -hex 12)"
{
  echo "POSTGRES_USER=hydrovalley"
  echo "POSTGRES_PASSWORD=${pg_password}"
  echo "POSTGRES_DB=hydrovalley"
  echo "ADMIN_USERNAME=${ADMIN_USERNAME}"
  echo "ADMIN_PASSWORD_HASH='${ADMIN_PASSWORD_HASH}'"
  echo "TOKEN_PRIVATE_KEY=${TOKEN_PRIVATE_KEY}"
  echo "TOKEN_PUBLIC_KEY=${TOKEN_PUBLIC_KEY}"
} > "${secrets}"
chmod 600 "${addresses}" "${secrets}"
# Que no queden en el entorno de los procesos hijos: cada nodo lee su archivo.
unset TOKEN_PRIVATE_KEY TOKEN_PUBLIC_KEY ADMIN_PASSWORD_HASH

# host.docker.internal también en Linux (host-gateway). Va en un archivo
# aparte, solo de la simulación: los archivos de los nodos no lo llevan.
services_of() {
  case "$1" in
    db) echo "postgres" ;;
    proxy) echo "api nginx" ;;
    *) echo "$1" ;;
  esac
}
for node in "${nodes[@]}"; do
  {
    echo "services:"
    for svc in $(services_of "${node}"); do
      echo "  ${svc}:"
      echo "    extra_hosts:"
      echo "      - \"host.docker.internal:host-gateway\""
    done
  } > "${tmp}/${node}.override.yml"
done

# 3. Los cinco nodos, en cualquier orden y sin esperar entre ellos (RNF-09).
echo "== Levantando los cinco nodos (proyectos hv-sim-*, publicados en el anfitrión)"
up_start=$SECONDS
for node in proxy hub world persistence db; do
  node_compose "${node}" up -d --quiet-pull
done

# El certificado del proxy lo genera nginx al arrancar: se toma de ahí y se usa
# como CA, así que la verificación TLS no se omite.
export HV_BASE_URL="https://localhost:${port_proxy}"
export NODE_EXTRA_CA_CERTS="${tmp}/tls.crt"
until node_compose proxy cp nginx:/etc/nginx/tls/tls.crt "${tmp}/tls.crt" >/dev/null 2>&1 && [[ -s "${tmp}/tls.crt" ]]; do
  if (( SECONDS - up_start > 60 )); then
    echo "simulate.sh: el proxy no generó su certificado en 60 s" >&2
    exit 1
  fi
  sleep 1
done

all_up() {
  [[ "$(curl -fsS --cacert "${tmp}/tls.crt" --max-time 3 "${HV_BASE_URL}/api/system" 2>/dev/null | grep -o '"status":"up"' | wc -l | tr -d ' ')" == "5" ]]
}
until all_up; do
  if (( SECONDS - up_start > wait_limit )); then
    echo "simulate.sh: /api/system no dio los cinco servicios en up en ${wait_limit} s" >&2
    exit 1
  fi
  sleep 1
done
up_s=$((SECONDS - up_start))
echo "== /api/system: los cinco servicios en up (${up_s} s desde el primer up)"
curl -fsS --cacert "${tmp}/tls.crt" "${HV_BASE_URL}/api/system"
echo

# 4. Humo y fallo.
export ADMIN_USERNAME SIM_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD}"
echo "== Humo"
t=$SECONDS
node "${nodes_dir}/simulate-check.mjs" smoke
smoke_s=$((SECONDS - t))

fault_s=n/d
if [[ "${SIM_SKIP_FAULT:-0}" != "1" ]]; then
  echo "== Fallo: se para el nodo persistence"
  t=$SECONDS
  persistence_cmd="docker compose -p hv-sim-persistence -f '${nodes_dir}/persistence.yml' -f '${tmp}/persistence.override.yml' --env-file '${addresses}' --env-file '${secrets}'"
  SIM_STOP_PERSISTENCE="${persistence_cmd} stop" SIM_START_PERSISTENCE="${persistence_cmd} start" \
    node "${nodes_dir}/simulate-check.mjs" fault
  fault_s=$((SECONDS - t))
fi

echo "MEDICION PLAN A SIMULADO: build_s=${build_s} up_s=${up_s} smoke_s=${smoke_s} fault_s=${fault_s} total_s=$((SECONDS - total_start)) tag=${HV_TAG}"
