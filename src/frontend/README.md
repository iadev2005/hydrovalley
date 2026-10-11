# Frontend

Frontend de Hydrovalley desarrollado con **Vite**, **React 19** y **TypeScript estricto** (`tsc -b`). No es un servicio independiente (RES-03): Nginx sirve los activos estáticos compilados directamente desde su propia imagen mediante una compilación multi-etapa (`deploy/nginx/Dockerfile`).

La aplicación es multi-página (MPA) y cuenta con dos puntos de entrada independientes:
1. `index.html` → Interfaz principal del juego (`/src/main.tsx`).
2. `sistema.html` → Panel de observabilidad para el profesor (`/src/system.tsx`).

---

## Características y Funcionalidades

### 1. Autenticación y Sesión
- Panel de acceso con pestañas para **Registrarse** y **Entrar** contra `POST /api/auth/register` y `POST /api/auth/login`.
- Manejo estricto de errores HTTP (400, 401 credenciales incorrectas, 409 usuario duplicado, 429 límite de tasa, 503 servicio no disponible).
- Persistencia segura de sesión (`token`, `expiresAt`, `username`) en `localStorage`. La contraseña nunca se almacena y el token JWT no se imprime en consola.
- Respeto del encabezado `Retry-After` ante respuestas `429 Too Many Requests`, bloqueando los botones durante el tiempo indicado (5 s por defecto).
- Cierre de sesión manual o automático si la sesión vence.

### 2. Conexión WebSocket y Protocolo de Juego (`/play`)
- Conexión WebSocket nativa hacia `/play?token=…&map=true` en el mismo origen.
- Protocolo binario/JSON conforme a `play.proto`:
  - Manejo de cierre **4401** (token inválido o expirado): borra la sesión y notifica al usuario sin reintentar.
  - Reconexión resiliente ante cualquier otra desconexión mediante retroceso exponencial (*exponential backoff*) y *full jitter* (base 500 ms, techo 10 s; conexiones exitosas de ≥ 5 s reinician el temporizador).
  - Cola de acciones pendientes con reenvío automático al restablecer la conexión (regla 2 de `CLAUDE.md`).
  - Deduplicación y traducción de respuestas del hub: `applied`, `duplicate` (se asume aplicada), `rejected` con motivo, `expired`, `rate_limited`, `paused` y `forbidden`.

### 3. Vista del Valle e Interacción
- Componente `<ValleyCanvas />` que aloja un lienzo 2D de 32 × 32 casillas encapsulado con `useRef`, desacoplado y preparado para su sustitución por Phaser en la tarea A3.
- Visualización de etapas de vegetación (tierra seca, pasto, arbusto, flores, árbol), fuego activo e instalaciones (A: Aspersor, G: Goteo, P: Planta de tratamiento).
- Panel de recursos: agua disponible, capacidad del depósito, hojas ganadas y costos dinámicos de las siguientes instalaciones.
- Botones de acción: **Soltar gota**, colocar instalaciones en la casilla seleccionada y adquirir mejoras de depósito/gota.
- **Panel de administración (RF-31)**: visible solo para usuarios con rol `admin` (leído de la carga útil del JWT; validado autoritativamente por el hub), permitiendo activar/desactivar el modo demo y reiniciar el valle previa confirmación modal.

### 4. Ranking y Reportes 3R (RF-10, RF-11, RF-12)
- Consulta periódica (cada 10 s) a `GET /api/ranking?limit=10` y `GET /api/report?scope=me|global` con cabecera `Authorization: Bearer <token>`.
- Tabla de posiciones destacando al jugador actual.
- Reporte detallado en pestañas (**Mis 3R** y **Valle**):
  - Métricas de Reducir, Reutilizar, Reciclar, agua recargada/gastada y balance de hojas.
  - Los enteros de 64 bits (`u64`) se procesan como cadenas de texto (`string`) en la capa de lógica para prevenir pérdida de precisión sobre $2^{53}-1$ (IEEE 754).
  - Pausa de consultas cuando la pestaña está en segundo plano (`document.visibilityState === "hidden"`) o al cerrar sesión.

### 5. Panel del Sistema (RNF-08)
- Página dedicada en `/sistema.html` accesible mediante enlace en el pie de página de `index.html`.
- Sondeo cada 2 s a `GET /api/system` (límite API: 120 req/min por IP).
- Semáforo y estado textual («En línea» / «Caído» con motivo) para los 5 microservicios (`gateway`, `auth`, `hub`, `world`, `persistence`).
- Métricas del hub: jugadores conectados, goroutines, acciones por segundo (admitidas/aplicadas), latencia p50 y p95, tick actual, duración de `Step`, fallos acumulados, longitud de cola, pausa y estado del *circuit breaker*.
- Métricas del valle: humedad total y aportada, gotas aceptadas e incendios activos.
- Estrategia de retroceso ante fallos de red o errores HTTP (intervalos escalonados de 4s, 8s, 16s y 30s) y respeto a `Retry-After` en respuestas 429.

---

## Estructura del Proyecto

```
src/frontend/
├── index.html                  # Punto de entrada HTML del juego
├── sistema.html                # Punto de entrada HTML del panel del sistema
├── styles.css                  # Estilos globales y paleta visual retro-pixel
├── vite.config.ts              # Configuración multi-página, proxy y setup de tests
├── tsconfig.json               # Configuración base de TypeScript
├── tsconfig.app.json           # Configuración TS para la aplicación (strict)
├── tsconfig.node.json          # Configuración TS para scripts y Vite
├── eslint.config.js            # Reglas de ESLint para TypeScript y React Hooks
├── package.json                # Scripts y dependencias (pnpm)
├── public/                     # Activos estáticos públicos (preparado para sprites A3)
└── src/
    ├── main.tsx                # Montaje de React para index.html (GameApp)
    ├── system.tsx              # Montaje de React para sistema.html (SystemApp)
    ├── test-setup.ts           # Configuración de cleanup para Vitest
    ├── containers/             # Contenedores con gestión de estado
    │   ├── GameApp.tsx         # Contenedor principal del juego
    │   ├── GameApp.test.tsx
    │   ├── SystemApp.tsx       # Contenedor del panel del sistema
    │   └── SystemApp.test.tsx
    ├── components/             # Componentes presentacionales
    │   ├── AdminPanel.tsx      # Controles de administración (modo demo / reinicio)
    │   ├── AuthPanel.tsx       # Formulario de registro y login
    │   ├── LogConsole.tsx      # Consola de eventos y avisos
    │   ├── RankingTable.tsx    # Tabla del ranking de jugadores
    │   ├── ReportPanel.tsx     # Panel de reportes de sostenibilidad 3R
    │   ├── SessionBar.tsx      # Barra de usuario activo y botón salir
    │   ├── SystemDashboard.tsx # Métricas de servicios y componentes del sistema
    │   ├── ValleyCanvas.tsx    # Canvas 2D interactivo (32x32) con useRef
    │   ├── ValleyControls.tsx  # Botones de acciones del juego y costes
    │   └── *.test.tsx          # Pruebas unitarias de componentes
    ├── hooks/                  # Custom Hooks para ciclo de vida y red
    │   ├── useGameSocket.ts    # WebSocket /play, reconexión y cola de acciones
    │   ├── useReports.ts       # Sondeo de ranking y reportes
    │   ├── useSession.ts       # Estado de sesión y persistencia local
    │   ├── useSystemMetrics.ts # Sondeo y reintentos del panel del sistema
    │   └── *.test.ts           # Pruebas unitarias de hooks
    └── logic/                  # Lógica de dominio pura sin dependencias de DOM
        ├── auth.ts             # Funciones de login, registro y parsing de JWT
        ├── play.ts             # Protocolo WebSocket, backoff, UUIDs y frames
        ├── reports.ts          # Procesamiento numérico seguro de reportes 3R
        ├── system.ts           # Normalización de métricas de salud de servicios
        ├── valley.ts           # Renderizado de celdas del valle y costes
        └── *.test.ts           # Pruebas unitarias de lógica de negocio
```

---

## Comandos y Desarrollo

Se utiliza **pnpm** como gestor de paquetes.

```bash
# Instalar dependencias
pnpm install

# Servidor de desarrollo con recarga en caliente (HMR) y proxy
pnpm run dev

# Ejecutar verificación de tipos TypeScript (modo estricto)
pnpm run typecheck

# Ejecutar linter (ESLint)
pnpm run lint

# Ejecutar suite de pruebas unitarias y de integración (Vitest)
pnpm test

# Compilar para producción (genera carpeta dist/ con index.html y sistema.html)
pnpm run build

# Previsualizar la compilación de producción localmente
pnpm run preview
```

---

## Despliegue en Docker

El frontend se compila dentro de la imagen de Nginx mediante una construcción multi-etapa definida en `deploy/nginx/Dockerfile`:

1. **Etapa constructora (`frontend-builder`)**:
   - Imagen base: `node:22-alpine` fijada con digest seguro (`@sha256:...`).
   - Activa Corepack para pnpm y corre `pnpm install --frozen-lockfile`.
   - Ejecuta `pnpm run build` produciendo el directorio `/build/dist/`.
2. **Etapa final de producción (`nginx`)**:
   - Copia los artefactos compilados desde `/build/dist/` hacia `/usr/share/nginx/html/`.
   - Nginx sirve los activos estáticos y actúa como proxy inverso hacia la API (`/api/`) y el Hub (`/play`).
