# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
# Imagen del servicio de chat en tiempo real (linux/amd64 en el pipeline).
#
# El contexto de construcción es la raíz del repositorio:
#
#   docker build -f src/chat/Dockerfile -t hydrovalley-chat .

# --- 1. Código generado desde proto/ ---------------------------------------
FROM bufbuild/buf:1.73.0@sha256:75d8f756f0919dcded71e67968a91adfe55fe4dd62c4900c57bbfcd7fe625608 AS proto
WORKDIR /workspace
COPY proto/ proto/
RUN buf generate --template proto/buf.gen.yaml

# --- 2. Compilación ---------------------------------------------------------
FROM golang:1.26@sha256:6c2a5538f964f1c82f97ad14988bf05de100d922d159d0e398b54c7b0ca0c6c9 AS build
WORKDIR /workspace
COPY --from=proto /workspace/gen/go gen/go
COPY proto/go.mod.tmpl gen/go/go.mod
COPY src/chat/go.mod src/chat/go.sum src/chat/
WORKDIR /workspace/src/chat
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src/chat/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/chat ./cmd/chat

# --- 3. Imagen final, sin toolchain y con usuario sin privilegios ---------
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/chat /usr/local/bin/chat
ENV CHAT_HTTP_LISTEN=:8082 CHAT_GRPC_LISTEN=:50051
USER nonroot
EXPOSE 8082 50051
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/chat", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/chat"]
