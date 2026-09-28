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
FROM bufbuild/buf:1.73.0 AS proto
WORKDIR /workspace
COPY proto/ proto/
RUN buf generate --template proto/buf.gen.yaml

# --- 2. Dependencias con uv (uv.lock, sin las de desarrollo) --------------
FROM python:3.12-slim AS deps
COPY --from=ghcr.io/astral-sh/uv:0.12.3 /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never \
    UV_PROJECT_ENVIRONMENT=/app/.venv
WORKDIR /app
COPY src/persistence/pyproject.toml src/persistence/uv.lock ./
RUN uv sync --frozen --no-dev

# --- 3. Imagen final, sin uv y con usuario sin privilegios ----------------
FROM python:3.12-slim
RUN useradd --system --uid 10001 --no-create-home persistence
WORKDIR /app
COPY --from=deps /app/.venv /app/.venv
COPY --from=proto /workspace/gen/python /app/gen
COPY src/persistence/persistence /app/persistence
COPY src/persistence/migrations /app/migrations
ENV PATH=/app/.venv/bin:$PATH \
    PYTHONPATH=/app:/app/gen \
    PYTHONUNBUFFERED=1 \
    PERSISTENCE_LISTEN=0.0.0.0:50051
USER persistence
EXPOSE 50051
# DATABASE_URL se pasa al ejecutar el contenedor (nunca en la imagen).
CMD ["python", "-m", "persistence"]
