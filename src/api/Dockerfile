# syntax=docker/dockerfile:1
# Imagen de la API (linux/amd64 en el pipeline).
#
# El contexto de construcción es la raíz del repositorio, como en los demás
# servicios (el cliente gRPC de AccountService necesitará proto/):
#
#   docker build -f src/api/Dockerfile -t hydrovalley-api .

# --- 1. Compilación de TypeScript -----------------------------------------
FROM node:22-trixie-slim AS build
WORKDIR /app
COPY src/api/package.json src/api/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --ignore-scripts
COPY src/api/tsconfig.json src/api/tsconfig.build.json ./
COPY src/api/src ./src
RUN npm run build

# --- 2. Dependencias de producción -----------------------------------------
FROM node:22-trixie-slim AS deps
WORKDIR /app
COPY src/api/package.json src/api/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --omit=dev --ignore-scripts

# --- 3. Imagen final, sin herramientas de compilación y sin root ----------
FROM node:22-trixie-slim
ENV NODE_ENV=production \
    API_LISTEN=0.0.0.0:3000
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY src/api/package.json ./
# Usuario sin privilegios que trae la imagen oficial de Node.
USER node
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["node", "dist/healthcheck.js"]
CMD ["node", "dist/main.js"]
