# Piezas comunes de tests/e2e/run.sh y tests/load/run.sh: certificado TLS
# efímero, claves Ed25519 efímeras, cuenta de administrador efímera y limpieza
# del stack. Se carga con `source`; no se ejecuta sola.
#
# Cada script define antes COMPOSE_PROJECT_NAME, COMPOSE_FILE (con
# tests/e2e/compose.tls.yml), COMPOSE_ENV_FILES y HV_HTTP_PORT, y después llama
# a las funciones en este orden: stack_init_tls, stack_init_keys,
# stack_init_admin y `trap stack_cleanup EXIT`.
#
# Variables que lee:
#   STACK_SKIP_BUILD  "1": no construir, usar las imágenes ya cargadas
#   STACK_KEEP        "1": no bajar el stack al terminar
#   STACK_KEEP_NAME   nombre de la variable que activa STACK_KEEP, para el aviso
#   STACK_TOOL        nombre del script, para los mensajes de error

# Certificado TLS efímero para nginx (RNF-12): se genera en cada ejecución, se
# monta en nginx y quien prueba confía en él (NODE_EXTRA_CA_CERTS para Node,
# HV_CA_CERT para los bots; ver tests/e2e/compose.tls.yml): nunca se omite la
# verificación. Directorio y clave legibles por cualquiera porque nginx corre
# con el usuario 101; es una clave desechable que stack_cleanup borra.
stack_init_tls() {
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
}

# Clave Ed25519 efímera: se genera en cada ejecución y nunca se guarda
# (RNF-13). Si ya viene definida en el entorno, se respeta.
stack_init_keys() {
  if [[ -z "${TOKEN_PRIVATE_KEY:-}" ]]; then
    TOKEN_PRIVATE_KEY="$(openssl genpkey -algorithm ed25519 | base64 | tr -d '\n')"
    export TOKEN_PRIVATE_KEY
  fi
  # La pública sale de la privada, para el hub (base64 del PEM, una línea). La
  # privada puede venir como PEM o como el base64 del PEM.
  if [[ -z "${TOKEN_PUBLIC_KEY:-}" ]]; then
    local private_pem
    if [[ "${TOKEN_PRIVATE_KEY}" == *"-----BEGIN"* ]]; then
      private_pem="${TOKEN_PRIVATE_KEY//\\n/$'\n'}"
    else
      private_pem="$(printf '%s' "${TOKEN_PRIVATE_KEY}" | base64 --decode)"
    fi
    TOKEN_PUBLIC_KEY="$(printf '%s\n' "${private_pem}" | openssl pkey -pubout | base64 | tr -d '\n')"
    if [[ -z "${TOKEN_PUBLIC_KEY}" ]]; then
      echo "${STACK_TOOL:-stack.sh}: no se pudo derivar TOKEN_PUBLIC_KEY de TOKEN_PRIVATE_KEY" >&2
      exit 1
    fi
    export TOKEN_PUBLIC_KEY
  fi
}

# Cuenta de administrador (RF-31): contraseña efímera y su hash Argon2id,
# calculado con la imagen de la API (como en deploy/README.md). Ni la
# contraseña ni el hash se guardan en disco. Si ADMIN_PASSWORD_HASH ya viene
# definido, se respeta (y E2E_ADMIN_PASSWORD también debe venir). Los
# parámetros de Argon2id (m=19456, t=2, p=1) deben ser los de la API:
# src/api/src/passwords.ts.
#   stack_init_admin <nombre de usuario>
stack_init_admin() {
  [[ -n "${ADMIN_PASSWORD_HASH:-}" ]] && return 0
  export ADMIN_USERNAME="$1"
  E2E_ADMIN_PASSWORD="$(openssl rand -hex 16)"
  export E2E_ADMIN_PASSWORD
  if [[ "${STACK_SKIP_BUILD:-0}" != "1" ]]; then
    # La imagen de la API tiene que existir antes de calcular el hash.
    docker compose build api
  fi
  local api_image="${HV_REGISTRY:-ghcr.io/iadev2005}/hydrovalley-api:${HV_TAG:-dev}"
  ADMIN_PASSWORD_HASH="$(printf '%s' "${E2E_ADMIN_PASSWORD}" | docker run --rm -i "${api_image}" node --input-type=module -e '
import { hash } from "@node-rs/argon2";
let pw = ""; for await (const c of process.stdin) pw += c;
console.log(await hash(pw, { algorithm: 2, memoryCost: 19456, timeCost: 2, parallelism: 1 }));')"
  if [[ "${ADMIN_PASSWORD_HASH}" != '$argon2id$'* ]]; then
    echo "${STACK_TOOL:-stack.sh}: no se pudo calcular el hash Argon2id del administrador" >&2
    exit 1
  fi
  export ADMIN_PASSWORD_HASH
}

# Limpieza: con fallo muestra el estado y los registros; baja el stack con sus
# volúmenes y borra el certificado, salvo que STACK_KEEP=1.
stack_cleanup() {
  local status=$?
  if [[ "${STACK_KEEP:-0}" == "1" ]]; then
    echo "${STACK_KEEP_NAME:-STACK_KEEP}=1: el stack sigue en marcha (docker compose -p ${COMPOSE_PROJECT_NAME} down -v)."
    exit "${status}"
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "--- estado y registros de los servicios ---"
    docker compose ps -a || true
    docker compose logs --no-color --tail=80 || true
  fi
  docker compose down -v --remove-orphans || true
  rm -rf "${E2E_TLS_DIR:-}"
  exit "${status}"
}
