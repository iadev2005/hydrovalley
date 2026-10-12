# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
# Imagen del simulador del valle (linux/amd64 en el pipeline).
#
# El contexto de construcción es la raíz del repositorio, porque build.rs
# compila los contratos de proto/:
#
#   docker build -f src/world/Dockerfile -t hydrovalley-world .
#
# El código de Rust se genera en la compilación con tonic-build y el protoc
# de protoc-bin-vendored: no hace falta protoc ni buf.

# --- 1. Compilación --------------------------------------------------------
FROM rust:1.97-slim-trixie@sha256:8e8cf8f7fd54a2d23d5a743b3a03f56e26b6c774276c33fa0595111704ebb15c AS build
WORKDIR /workspace
# Misma estructura que el repositorio: build.rs busca ../../proto.
COPY proto/ proto/
COPY src/world/ src/world/
WORKDIR /workspace/src/world
# Cachés de BuildKit para el registro de crates y target/; el binario se
# copia fuera de la caché para la etapa final.
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/workspace/src/world/target \
    cargo build --release --locked --bin world \
    && cp target/release/world /workspace/world

# --- 2. Imagen final, sin toolchain y con usuario sin privilegios ---------
FROM gcr.io/distroless/cc-debian13:nonroot@sha256:54df941ed0d06a1bd95ef5e0ce391fd8d9f94b64782dc9a60062727849ee3f97
COPY --from=build /workspace/world /usr/local/bin/world
# Sonda estándar de grpc.health.v1 (binario estático): la imagen distroless
# no trae shell ni curl.
COPY --from=ghcr.io/grpc-ecosystem/grpc-health-probe:v0.4.57@sha256:f77f1257805ecb57f1f0c36c5825d3c2e47aa81cce87128675e768463ed6487a /ko-app/grpc-health-probe /usr/local/bin/grpc_health_probe
ENV WORLD_LISTEN=0.0.0.0:50051
USER nonroot
EXPOSE 50051
# SERVING solo después de cargar la instantánea (o NOT_FOUND la primera vez).
# El puerto es el de WORLD_LISTEN por defecto; si se cambia, hay que
# sobrescribir el healthcheck en Compose.
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/grpc_health_probe", "-addr=127.0.0.1:50051", "-connect-timeout=2s", "-rpc-timeout=2s"]
# PERSISTENCE_ADDR se pasa al ejecutar el contenedor (p. ej. persistence:50051).
ENTRYPOINT ["/usr/local/bin/world"]
