# Recursos Gráficos del Valle Isométrico (Hydrovalley)

Este directorio documenta los recursos gráficos utilizados por la escena isométrica de Phaser (tarea A3, RF-03, RF-04). Los archivos estáticos en tiempo de ejecución se sirven desde [`src/frontend/public/assets/`](../public/assets/).

---

## 1. Arquitectura Visual de Capas (Rombo Base + Elementos Superpuestos)

Para lograr una perspectiva isométrica limpia y fiel al documento del proyecto, el terreno se compone de dos capas lógicas:
1. **Capa de Suelo Base (`Tile`)**: Rombos planos de proporción estándar 2:1 ($32 \times 16\text{ px}$) sin zócalo vertical inferior, formando un mosaico continuo de $32 \times 32$ casillas.
2. **Capa de Elementos / Vegetación / Fuego / Instalaciones (`Overlay`)**: Objetos verticales con fondo transparente anclados en la base del rombo (eje $X$ centrado, eje $Y$ en el borde inferior), que crecen hacia arriba según la etapa, fuego o instalación.

---

## 2. Catálogo de Recursos

### 2.1 Terreno e Ilustraciones por Etapa (`tiles/`)

| Archivo | Etapa (`TileStage`) | Dimensiones | Capa | Descripción visual |
| :--- | :--- | :--- | :--- | :--- |
| `tile_1_dry.png` | 1 (Tierra seca) | $32 \times 16\text{ px}$ | Base (Suelo) | Rombo plano árido en tono ocre/tierra con borde pixelado. |
| `tile_2_grass.png` | 2 (Pasto) | $32 \times 16\text{ px}$ | Base (Suelo) | Rombo plano verde claro que cubre la casilla. |
| `tile_3_bush.png` | 3 (Arbusto) | $32 \times 32\text{ px}$ | Overlay (Planta) | Arbusto denso de follaje verde que se apoya sobre el pasto. |
| `tile_4_flowers.png` | 4 (Flores) | $32 \times 32\text{ px}$ | Overlay (Planta) | Ramillete de flores rosadas con tallo que se apoya sobre el pasto. |
| `tile_5_tree.png` | 5 (Árbol) | $64 \times 80\text{ px}$ | Overlay (Planta) | Árbol frondoso con tronco de madera y copa escalonada anclado al centro del pasto. |

### 2.2 Instalaciones de las 3R (`installations/`)

| Archivo | Instalación | Dimensiones | Capa | Descripción visual |
| :--- | :--- | :--- | :--- | :--- |
| `sprinkler.png` | `sprinkler` (Aspersor - Reutilizar) | $32 \times 32\text{ px}$ | Overlay | Aspersor metálico con boquilla giratoria y agua pulverizada. |
| `drip.png` | `drip` (Riego por goteo - Reducir) | $32 \times 16\text{ px}$ | Overlay | Tubería diagonal con válvula y gotas de precisión que bordea el rombo. |
| `treatment_plant.png` | `treatment_plant` (Planta - Reciclar) | $32 \times 32\text{ px}$ | Overlay | Depósito de filtrado y tratamiento con grifería sobre suelo técnico. |

### 2.3 Efectos, Fuego e Indicadores (`effects/`)

| Archivo | Uso | Dimensiones | Capa | Descripción visual |
| :--- | :--- | :--- | :--- | :--- |
| `fire.png` | Fuego estático base | $32 \times 32\text{ px}$ | Overlay | Hoguera con leña quemada, brasas y llamas ascendentes con humo. |
| `fire_sheet.png` | Fuego animado | $128 \times 32\text{ px}$ | Overlay (4 frames $32 \times 32$) | Hoja de sprites con 4 fotogramas para animación de llamas y brasas pulsantes. |
| `droplet.png` | Gota en el aire (acción pendiente) | $16 \times 16\text{ px}$ | Entidad flotante | Gota de agua en trayectoria aérea. |
| `splash.png` | Salpicadura de gota aplicada | $32 \times 16\text{ px}$ | Overlay en suelo | Onda expansiva de impacto de agua sobre el rombo. |
| `tile_selector.png` | Cursor de selección | $32 \times 16\text{ px}$ | Overlay en suelo | Rombo amarillo brillante para resaltar la casilla seleccionada. |

---

## 3. Licencias y Procedencia

Conforme a las reglas del repositorio (`CLAUDE.md`) y los criterios de aceptación de la tarea A3:

1. **Paquete base y arte original**:
   - **Obra base**: *Simple Cozy - Farm Assets - Free Pack*
   - **Autor original**: [Aibyzz](https://aibyzz.itch.io/) en itch.io.
   - **Licencia**: Libre (Free Pack en itch.io para uso en proyectos de juegos).
2. **Adaptación y edición para Hydrovalley**:
   - **Adaptador**: Fer2703 (diseño, composición por capas de 2:1 a $32 \times 16$, adaptación de elementos de las 3R y selector de casillas).
   - **Licencia derivada**: CC0 1.0 Universal / Libre distribución para el proyecto educativo Hydrovalley.
3. **Efectos generados**:
   - `fire_sheet.png`, `droplet.png`, `splash.png`: Adaptados y generados para las animaciones y feedback visual de Hydrovalley (CC0).
