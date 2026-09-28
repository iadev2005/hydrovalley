# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
# Imagen del hub en tiempo real (linux/amd64 en el pipeline).
#
# El contexto de construcción es la raíz del repositorio:
#
#   docker build -f src/hub/Dockerfile -t hydrovalley-hub .
#
# El código de Go se genera aquí mismo a partir de proto/ con la misma
# versión de buf que scripts/gen-proto.sh y el CI, así que la imagen no
# depende de que gen/ exista en la máquina que construye (necesita acceso
# a buf.build para los plugins remotos). Solo se copia gen/go.

# --- 1. Código generado desde proto/ ---------------------------------------
FROM bufbuild/buf:1.73.0@sha256:75d8f756f0919dcded71e67968a91adfe55fe4dd62c4900c57bbfcd7fe625608 AS proto
WORKDIR /workspace
COPY proto/ proto/
RUN buf generate --template proto/buf.gen.yaml

# --- 2. Compilación ---------------------------------------------------------
FROM golang:1.26@sha256:6c2a5538f964f1c82f97ad14988bf05de100d922d159d0e398b54c7b0ca0c6c9 AS build
WORKDIR /workspace
# Misma estructura que el repositorio: go.mod usa replace ../../gen/go.
COPY --from=proto /workspace/gen/go gen/go
# El go.mod de gen/go sale de la misma plantilla que usan
# scripts/gen-proto.sh y el CI.
COPY proto/go.mod.tmpl gen/go/go.mod
COPY src/hub/go.mod src/hub/go.sum src/hub/
WORKDIR /workspace/src/hub
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src/hub/ ./
# Binario estático (sin cgo) para la imagen distroless/static.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hub ./cmd/hub

# --- 3. Imagen final, sin toolchain y con usuario sin privilegios ---------
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/hub /usr/local/bin/hub
ENV HUB_LISTEN=:8081
USER nonroot
EXPOSE 8081
# La imagen no trae curl: el propio binario consulta /healthz.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/hub", "healthcheck"]
# WORLD_ADDR se pasa al ejecutar el contenedor (p. ej. world:50051).
ENTRYPOINT ["/usr/local/bin/hub"]
