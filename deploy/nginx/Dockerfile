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
FROM nginxinc/nginx-unprivileged:1.30-alpine@sha256:f5116ed4606f3eb015dd4a8e28cb9b351476dc0ad4bc6e9f93b19cec130cdf03

# Destinos por defecto del plan B; el plan A los cambia por variables de
# entorno sin reconstruir la imagen.
ENV API_ADDR=api:3000 \
    HUB_ADDR=hub:8081 \
    CHAT_ADDR=chat:8082 \
    NGINX_RESOLVER=127.0.0.11 \
    TLS_CERT_FILE=/etc/nginx/tls/tls.crt \
    TLS_KEY_FILE=/etc/nginx/tls/tls.key \
    NGINX_ENVSUBST_FILTER="^(API_ADDR|HUB_ADDR|CHAT_ADDR|NGINX_RESOLVER|TLS_CERT_FILE|TLS_KEY_FILE)$"

# TLS (RNF-12): openssl solo para generar, al arrancar el contenedor, un
# certificado autofirmado si no se monta uno. La clave nunca se genera al
# construir la imagen, que se publica en ghcr.io. /etc/nginx/tls es del
# usuario 101 para poder escribir ahí; un volumen de solo lectura montado
# encima lo sustituye.
USER root
RUN apk add --no-cache openssl \
    && mkdir -p /etc/nginx/tls \
    && chown 101:101 /etc/nginx/tls
USER 101

COPY deploy/nginx/templates/ /etc/nginx/templates/
COPY --chmod=0755 deploy/nginx/docker-entrypoint.d/ /docker-entrypoint.d/
COPY --from=frontend-builder /build/dist/ /usr/share/nginx/html/

# 8080 es HTTPS. El 8079 (HTTP, solo 127.0.0.1) existe para el healthcheck y
# no se publica.
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8079/healthz"]
