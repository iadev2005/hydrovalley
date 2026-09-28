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
ENV WORLD_LISTEN=0.0.0.0:50051
USER nonroot
EXPOSE 50051
# PERSISTENCE_ADDR se pasa al ejecutar el contenedor (p. ej. persistence:50051).
ENTRYPOINT ["/usr/local/bin/world"]
