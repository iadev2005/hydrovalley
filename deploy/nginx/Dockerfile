# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
# Imagen del proxy de entrada con el frontend dentro (linux/amd64 en el
# pipeline).
#
# El contexto de construcción es la raíz del repositorio, porque la imagen
# incluye los archivos de src/frontend:
#
#   docker build -f deploy/nginx/Dockerfile -t hydrovalley-proxy .
#
# El frontend no es un servicio (RES-03): son archivos estáticos que sirve
# este mismo contenedor. Por eso el Dockerfile vive con la configuración de
# nginx y no en src/frontend. Compila el frontend en una etapa Node con Vite
# y pnpm, y copia dist/ a la etapa final de nginx.

# Etapa de compilación del frontend con Node y pnpm.
FROM node:22-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402 AS frontend-builder
RUN corepack enable pnpm
WORKDIR /build
COPY src/frontend/package.json src/frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY src/frontend/ ./
RUN pnpm run build

# nginx oficial sin privilegios: usuario 101 y puerto 8080.
FROM nginxinc/nginx-unprivileged:1.30-alpine@sha256:ed04ec1ff34502c339ee5c3ae3f855442398edc1d05591e2b98981dcbbd20b1e

# Destinos por defecto del plan B; el plan A los cambia por variables de
# entorno sin reconstruir la imagen.
ENV API_ADDR=api:3000 \
    HUB_ADDR=hub:8081 \
    NGINX_RESOLVER=127.0.0.11 \
    NGINX_ENVSUBST_FILTER="^(API_ADDR|HUB_ADDR|NGINX_RESOLVER)$"

COPY deploy/nginx/templates/ /etc/nginx/templates/
COPY --from=frontend-builder /build/dist/ /usr/share/nginx/html/

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
