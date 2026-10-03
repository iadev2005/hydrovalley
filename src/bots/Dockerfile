# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
# Imagen de los bots (linux/amd64 en el pipeline).
#
# El contexto de construcción es la raíz del repositorio (igual que el hub):
#
#   docker build -f src/bots/Dockerfile -t hydrovalley-bots .
#
# A diferencia del hub, no hay etapa de generación de código: los marcos de
# /play son JSON plano y se escriben a mano (internal/client/frames.go), así
# que el módulo no depende de proto/ ni de gen/.

# --- 1. Compilación ---------------------------------------------------------
FROM golang:1.26@sha256:6c2a5538f964f1c82f97ad14988bf05de100d922d159d0e398b54c7b0ca0c6c9 AS build
WORKDIR /workspace/src/bots
COPY src/bots/go.mod src/bots/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src/bots/ ./
# Binario estático (sin cgo) para la imagen distroless/static.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bots ./cmd/bots

# --- 2. Imagen final, sin toolchain y con usuario sin privilegios ---------
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/bots /usr/local/bin/bots
USER nonroot
# Los bots se lanzan a demanda; sin HEALTHCHECK porque no escuchan en ningún puerto.
# HV_BASE_URL, HV_ADMIN_USERNAME y HV_ADMIN_PASSWORD se pasan al ejecutar.
ENTRYPOINT ["/usr/local/bin/bots"]
CMD ["neighbors"]
