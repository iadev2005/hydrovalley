# Plan de Expansión: Terreno, Durabilidad, Contaminación y Sprites

Este documento consolida la propuesta de expansión para **Hydrovalley**, integrando variedad ambiental, durabilidad de instalaciones, rebalanceo de efectos de las 3R, amenaza urbana (basura) y la lista técnica de activos gráficos requeridos.

---

## 1. Resumen de Mecánicas y Propósito

| Elemento | Tipo | Función Principal | Aporte al Sistema Distribuido |
| :--- | :--- | :--- | :--- |
| **Ríos / Lagos** | Terreno natural | Casillas de agua no cultivables; mitigan evaporación e incendios en orillas. | Diversidad visual y zonificación estratégica en el autómata celular. |
| **Rocas** | Terreno natural | Obstáculos inamovibles que bloquean cultivo e instalaciones. | Restricción espacial para la colocación de instalaciones $3 \times 3$ y $5 \times 5$. |
| **Durabilidad de Instalaciones** | Ciclo de vida | Desgaste finito por uso o tiempo; la instalación se destruye al llegar a cero. | Mantiene activa la economía de hojas y evita saturación permanente del mapa compartido. |
| **Riego por Goteo (Mejorado)** | Instalación (Reducir) | Escudo hídrico $5 \times 5$: anula marchitamiento, suprime ignición y duplica humedad de gotas. | Efecto pasivo tangible y de alto impacto defensivo. |
| **Planta de Tratamiento (Mejorada)** | Instalación (Reciclar) | Triplica recarga de depósito y emite pulso periódico ($3 \times 3$) que sube $+1$ etapa vegetal. | Refuerzo notorio al jugador activo y soporte a plantaciones adyacentes. |
| **Edificios Urbanos** | Amenaza ambiental | Aparecen tras cierto progreso; arrojan basura a casillas vecinas periódicamente. | Generador continuo de entropía en el autómata celular. |
| **Basura (3 Estados)** | Amenaza ambiental | Destruye plantas/instalaciones. Si llega a Nivel 3, desborda e incrementa basura alrededor. | Competencia en tiempo real: clics de limpieza concurrentes con resolución determinista. |
| **Cesto de Basura** | Instalación (Limpieza) | Contraparte del aspersor: limpia 1 nivel de basura en $3 \times 3$ automáticamente. | Automatización defensiva de las 3R para contención de residuos. |
| **Crecimiento Inmediato (1 Gota = 1 Etapa)** | Balance de juego | 1 gota manual aporta la humedad requerida para subir directamente la etapa vegetal. | *Game feel* reactivo y progreso visual acelerado para pruebas y demostraciones. |

---

## 2. Plan de Implementación por Fases

El despliegue se organiza en 4 fases progresivas para minimizar riesgos sobre los contratos gRPC y la simulación autoritativa.

### Fase 1: Progreso Visual Rápido y Rediseño de Efectos (Riego y Planta)
- **Alcance:** Nivel 1 / Configuración y lógica de simulación.
- **Cambios en Catálogo (`catalog/catalog.json`):**
  - Ajustar `rules.drop.humidity_points` a `100` (o reducir `growth.humidity_per_stage_points` a `25`), permitiendo avance de etapa con 1 sola gota.
  - Actualizar parámetros de `installations.treatment_plant` y `installations.drip`.
- **Cambios en Simulador (`src/world/src/domain/` y `src/world-go/`):**
  - **Riego por Goteo:** En la evaluación de evaporación e incendios, verificar si la casilla está en el área $5 \times 5$ de un goteo activo para pausar `dry_ticks`, suprimir probabilidad de fuego y duplicar aporte de humedad por gota.
  - **Planta de Tratamiento:** Añadir lógica de pulso periódico en ticks que incremente la etapa vegetal de casillas vecinas ($3 \times 3$) y acredite hojas al propietario.
- **Frontend (`src/frontend/`):**
  - Aplicar sombreado de suelo húmedo en el área activa de goteo y emitir efecto visual de pulso (`splash.png`) desde la planta de tratamiento.

### Fase 2: Sistema de Durabilidad de Instalaciones
- **Alcance:** Modificación de contratos e invariantes de simulación.
- **Contratos Protobuf (`proto/hydrovalley/world/v1/world.proto` y `persistence.proto`):**
  - Agregar campo `uint32 remaining_durability = 3;` en el mensaje `Installation`.
  - Agregar `durability` base por instalación en `catalog/catalog.json`.
  - Regenerar código mediante `scripts/gen-proto.sh`.
- **Simulador (`src/world/src/domain/tile.rs`):**
  - Inicializar la durabilidad al ejecutar `PlaceInstallationCommand`.
  - Decrementar durabilidad por ciclo de trabajo o intervalo de tiempo.
  - Desinstalar automáticamente (`tile.installation = None`) al agotarse la durabilidad.
- **Hub y Cliente (`src/hub/` y `src/frontend/`):**
  - Serializar y propagar `remaining_durability` en el payload de estado del WebSocket `/play`.
  - Mostrar barra o indicador numérico de desgaste al seleccionar la casilla en `ValleyControls.tsx`.

### Fase 3: Terreno Natural (Agua y Rocas)
- **Alcance:** Generación de mapa y reglas de restricción de casillas.
- **Contratos Protobuf (`world.proto`):**
  - Incorporar identificadores de terreno:
    - `TILE_STAGE_WATER = 6;`
    - `TILE_STAGE_ROCK = 7;`
- **Generador de Mundo (`src/world/src/domain/world.rs` y `rng.rs`):**
  - Generar de forma determinista (mediante la semilla RNG de la instantánea) franjas de río/lago y dispersión de rocas al crear un mundo nuevo o recibir `ResetValleyCommand`.
  - Validar comandos de jugador: rechazar con `REJECT_REASON_TILE_OCCUPIED` cualquier intento de regar o edificar sobre agua o roca.
  - La orilla del agua suprime incendios e incrementa la retención de humedad en casillas adyacentes.
- **Renderizado (`ValleyScene.ts` y `ValleyCanvas.tsx`):**
  - Dibujar `tile_water.png` en capa base y `tile_rock.png` en capa overlay.
  - Añadir soporte de color correspondiente en la vista cenital de emergencia (Canvas 2D).

### Fase 4: Presión Urbana, Basura en 3 Estados y Cesto de Reciclaje
- **Alcance:** Autómata celular avanzado, nuevos comandos y nuevo elemento de tienda.
- **Contratos Protobuf (`world.proto`):**
  - En `Tile`: agregar `bool has_building = 6;` y `uint32 trash_level = 7;` (valores 0 a 3).
  - En `InstallationKind`: agregar `INSTALLATION_KIND_TRASH_BIN = 4;`.
  - En `Command.kind`: agregar `CleanTrashCommand clean_trash = 8;` con `uint32 x` y `uint32 y`.
- **Lógica de Simulación en Rust (`src/world/src/domain/`):**
  - **Aparición de Edificios:** Generación de estructuras de 1 casilla tras alcanzar umbral de progreso. Inmunes a fuego y basura.
  - **Emisión de Basura:** En ciclos fijos, el edificio suma $+1$ a `trash_level` en una casilla vecina ($3 \times 3$), eliminando plantas e instalaciones preexistentes.
  - **Propagación en Nivel 3:** Una casilla con `trash_level == 3` incrementa en $+1$ la basura de casillas adyacentes no protegidas.
  - **Comando de Limpieza (`clean_trash`):** Cada ejecución reduce en 1 el `trash_level`. Al llegar a 0, la casilla retorna a tierra seca y otorga hojas de incentivo.
  - **Cesto de Basura:** Cada ciclo reduce automáticamente 1 nivel de basura en su radio $3 \times 3$ y descuenta 1 punto de su durabilidad.
- **Hub y Cliente (`src/hub/` y `src/frontend/`):**
  - Procesar `type: "clean_trash"` en el Hub Go con control de idempotencia (`action_id`).
  - Si el usuario hace clic sobre una casilla con `trash_level > 0`, emitir acción de limpieza.
  - Incorporar el botón de compra del Cesto de Basura en la tienda de instalaciones.

---

## 3. Catálogo Técnico de Sprites Nuevos

Para asegurar la coherencia visual con la arquitectura de capas documentada en `src/frontend/assets/README.md` (isometría 2:1, rombo base de $32 \times 16\text{ px}$, overlays transparentes alineados al pie):

| # | Archivo | Destino | Dimensiones | Capa | Descripción Técnica y Estilo |
| :-: | :--- | :--- | :--- | :--- | :--- |
| **1** | `tile_water.png` | `public/assets/tiles/` | $32 \times 16\text{ px}$ | Base (Suelo) | Rombo plano isométrico de agua azul con destellos pixelados, idéntico en escala a `tile_1_dry.png`. |
| **2** | `tile_rock.png` | `public/assets/tiles/` | $32 \times 32\text{ px}$ | Overlay (Terreno) | Formación rocosa gris/musgosa anclada en la parte inferior para encajar sobre el rombo de suelo. |
| **3** | `building_house.png` | `public/assets/tiles/` | $32 \times 48\text{ px}$ *(o $32 \times 32\text{ px}$)* | Overlay (Estructura) | Casa o edificio urbano estilizado en perspectiva isométrica, con base encajada en 1 casilla. |
| **4** | `trash_1_small.png` | `public/assets/tiles/` | $32 \times 32\text{ px}$ | Overlay (Desecho) | **Nivel 1:** Pila pequeña de desperdicios (bolsas y latas dispersas con suelo visible debajo). |
| **5** | `trash_2_medium.png` | `public/assets/tiles/` | $32 \times 32\text{ px}$ | Overlay (Desecho) | **Nivel 2:** Montón mediano de cajas y bolsas acumuladas cubriendo el centro del rombo. |
| **6** | `trash_3_large.png` | `public/assets/tiles/` | $32 \times 32\text{ px}$ | Overlay (Desecho) | **Nivel 3 (Crítico):** Montículo grande desbordado con vapores/moscas, indicando riesgo inminente de propagación. |
| **7** | `trash_bin.png` | `public/assets/installations/` | $32 \times 32\text{ px}$ | Overlay (Instalación) | Contenedor de reciclaje verde/metálico con icono distintivo, coordinado en escala con `sprinkler.png`. |
| **8** | `clean_sparkle.png` *(Opcional)* | `public/assets/effects/` | $16 \times 16\text{ px}$ *(o $32 \times 16\text{ px}$)* | Efecto visual | Destello o partículas de limpieza mostradas al reducir un nivel de basura. |

### Especificaciones para el Diseñador Gráfico
- **Formato:** PNG con canal alfa (transparencia obligatoria en capas overlay).
- **Paleta y Trazado:** Pixel art alineado a la estética de *Simple Cozy - Farm Assets*.
- **Punto de Anclaje:** Objetos de $32 \times 32\text{ px}$ deben tener su área de contacto centrada horizontalmente y apoyada en el borde inferior del sprite.

---

## 4. Estado de la implementación

Las cuatro fases están integradas en la rama `feature/gameplay-expansion` (en evaluación; ver `docs/tareas-equipo.md`, B9), con estas diferencias respecto al plan:

- **Cifras en el catálogo.** Además de las `durability` y del cesto (`trash_bin`), el terreno (`terrain.*`), la presión urbana (`urban.*`) y el desgaste del goteo (`drip.wear_interval_seconds`) están en `catalog/catalog.json`; no hay cifras escritas en el simulador. Los valores elegidos por el equipo (gota de 100, planta de 3 L, durabilidad 30...) están en D-01, sección «Valores elegidos por el equipo para la jugabilidad».
- **Terreno.** Se sortea con el generador con semilla del mundo (regla 6), al crear y al reiniciar el valle, no con un generador aparte. No hay `persistence.proto` que cambiar: la durabilidad, el edificio y la basura viajan en `world.proto` (campos aditivos).
- **Basura.** Una gota sobre una casilla con basura se rechaza (`TILE_OCCUPIED`); los aspersores y la planta no hacen crecer casillas con basura o edificio; `clean_trash` comparte con la gota el límite de 15 por segundo.
- **Pruebas.** Guion `10-expansion-jugabilidad` de la conformidad Rust ↔ Go y `tests/e2e/gameplay-expansion.test.mjs`.
