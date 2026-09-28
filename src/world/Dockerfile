# syntax=docker/dockerfile:1
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
FROM rust:1.97-slim-trixie AS build
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
FROM gcr.io/distroless/cc-debian13:nonroot
COPY --from=build /workspace/world /usr/local/bin/world
# Sonda estándar de grpc.health.v1 (binario estático): la imagen distroless
# no trae shell ni curl.
COPY --from=ghcr.io/grpc-ecosystem/grpc-health-probe:v0.4.57 /ko-app/grpc-health-probe /usr/local/bin/grpc_health_probe
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
