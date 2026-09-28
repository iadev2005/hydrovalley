# Imagen de la persistencia (linux/amd64 en el pipeline).
#
# El contexto de construcción es la raíz del repositorio:
#
#   docker build -f src/persistence/Dockerfile -t hydrovalley-persistence .
#
# El código de Python se genera aquí mismo a partir de proto/ con la misma
# versión de buf que scripts/gen-proto.sh y el CI, así que la imagen no
# depende de que gen/ exista en la máquina que construye (necesita acceso
# a buf.build para los plugins remotos). Solo se copia gen/python.

# --- 1. Código generado desde proto/ ---------------------------------------
FROM bufbuild/buf:1.73.0@sha256:75d8f756f0919dcded71e67968a91adfe55fe4dd62c4900c57bbfcd7fe625608 AS proto
WORKDIR /workspace
COPY proto/ proto/
RUN buf generate --template proto/buf.gen.yaml

# --- 2. Dependencias con uv (uv.lock, sin las de desarrollo) --------------
FROM python:3.12-slim@sha256:f77ac9e44ae96ef2c90b8053ea08c31f8be030f824196b0ae4db6d462c84e51f AS deps
COPY --from=ghcr.io/astral-sh/uv:0.12.3@sha256:2d890623d310b57771ce840f0da5eed5fc6d657da05ffaa45d82797b53fa3abc /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never \
    UV_PROJECT_ENVIRONMENT=/app/.venv
WORKDIR /app
COPY src/persistence/pyproject.toml src/persistence/uv.lock ./
RUN uv sync --frozen --no-dev

# --- 3. Imagen final, sin uv y con usuario sin privilegios ----------------
FROM python:3.12-slim@sha256:f77ac9e44ae96ef2c90b8053ea08c31f8be030f824196b0ae4db6d462c84e51f
RUN useradd --system --uid 10001 --no-create-home persistence
WORKDIR /app
COPY --from=deps /app/.venv /app/.venv
COPY --from=proto /workspace/gen/python /app/gen
COPY src/persistence/persistence /app/persistence
COPY src/persistence/migrations /app/migrations
# Sonda estándar de grpc.health.v1 (binario estático), la misma que usa el
# simulador.
COPY --from=ghcr.io/grpc-ecosystem/grpc-health-probe:v0.4.57@sha256:f77f1257805ecb57f1f0c36c5825d3c2e47aa81cce87128675e768463ed6487a /ko-app/grpc-health-probe /usr/local/bin/grpc_health_probe
ENV PATH=/app/.venv/bin:$PATH \
    PYTHONPATH=/app:/app/gen \
    PYTHONUNBUFFERED=1 \
    PERSISTENCE_LISTEN=0.0.0.0:50051
USER persistence
EXPOSE 50051
# SERVING solo con la base de datos conectada y las migraciones aplicadas.
# El puerto es el de PERSISTENCE_LISTEN por defecto; si se cambia, hay que
# sobrescribir el healthcheck en Compose.
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD ["grpc_health_probe", "-addr=127.0.0.1:50051", "-connect-timeout=2s", "-rpc-timeout=2s"]
# DATABASE_URL se pasa al ejecutar el contenedor (nunca en la imagen).
CMD ["python", "-m", "persistence"]
