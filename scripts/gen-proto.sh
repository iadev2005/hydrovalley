#!/usr/bin/env bash
# Valida los contratos (buf lint) y genera el código de Go, Python y
# TypeScript en gen/ (buf generate con proto/buf.gen.yaml). Usa la imagen
# oficial de Buf en Docker, así que no hace falta instalar buf.
#
# Uso, desde cualquier carpeta del repositorio:
#
#   scripts/gen-proto.sh
#
# Si el asistente de credenciales de Docker se cuelga, se puede apuntar a
# una configuración vacía; la CLI de Docker lee DOCKER_CONFIG del entorno:
#
#   DOCKER_CONFIG=/ruta/a/carpeta/con/config.json/vacío scripts/gen-proto.sh
#
# Funciona en macOS, Linux y Git Bash/WSL en Windows.
set -euo pipefail

# Versión fijada de buf; se sube a mano, igual que en el CI.
BUF_IMAGE="bufbuild/buf:1.73.0"

# Raíz del repositorio, sin depender de la carpeta desde la que se llame.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Paso explícito de DOCKER_CONFIG: si viene definido, se exporta tal cual.
if [[ -n "${DOCKER_CONFIG:-}" ]]; then
  export DOCKER_CONFIG
fi

# En Git Bash, evita que MSYS reescriba las rutas del contenedor.
export MSYS_NO_PATHCONV=1

# Ejecuta buf en el contenedor con el repositorio montado en /workspace.
# El usuario del anfitrión evita que gen/ quede con dueño root en Linux, y
# la caché de buf va a /tmp porque ese usuario no tiene carpeta personal.
buf() {
  docker run --rm \
    --user "$(id -u):$(id -g)" \
    -e BUF_CACHE_DIR=/tmp/buf-cache \
    -v "${ROOT}:/workspace" \
    -w /workspace \
    "${BUF_IMAGE}" "$@"
}

echo "==> buf lint"
buf lint proto

echo "==> buf generate"
buf generate --template proto/buf.gen.yaml

# gen/go es un módulo Go propio; los servicios lo usan con
#   require github.com/iadev2005/hydrovalley/gen/go v0.0.0
#   replace github.com/iadev2005/hydrovalley/gen/go => ../../gen/go
# Sus dependencias (protobuf y grpc) las fija el go.mod de cada servicio.
# Su go.mod sale de proto/go.mod.tmpl, la misma plantilla que usan el
# Dockerfile del hub y el CI.
cp "${ROOT}/proto/go.mod.tmpl" "${ROOT}/gen/go/go.mod"

echo "==> Código generado en ${ROOT}/gen (go, python, ts)"
