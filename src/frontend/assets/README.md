# Recursos Gráficos del Valle Isométrico (Hydrovalley)

Este directorio documenta los recursos gráficos utilizados por la escena isométrica de Phaser (tarea A3, RF-03, RF-04) y la expansión de terreno, durabilidad y amenaza urbana (Sección 3). Los archivos estáticos en tiempo de ejecución se sirven desde [`src/frontend/public/assets/`](../public/assets/).

---

## 1. Arquitectura Visual de Capas (Rombo Base + Elementos Superpuestos)

Para lograr una perspectiva isométrica limpia y fiel al documento del proyecto, el terreno se compone de dos capas lógicas:
1. **Capa de Suelo Base (`Tile`)**: Rombos planos de proporción estándar 2:1 ($32 \times 16\text{ px}$) sin zócalo vertical inferior, formando un mosaico continuo de $32 \times 32$ casillas (288 píxeles activos canónicos por rombo).
2. **Capa de Elementos / Vegetación / Fuego / Instalaciones / Residuos (`Overlay`)**: Objetos verticales con fondo transparente anclados en la base del rombo (eje $X$ centrado en $x=16$, eje $Y$ en el centro de apoyo $y=24$, anclaje canónico `setOrigin(0.5, 0.75)` para lienzos $32 \times 32\text{ px}$), que crecen hacia arriba según la etapa, fuego, residuo o instalación.

---

## 2. Catálogo de Recursos

### 2.1 Terreno e Ilustraciones por Etapa (`tiles/`)

| Archivo | Etapa (`TileStage`) | Dimensiones | Capa | Anclaje | Descripción visual |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `tile_1_dry.png` | 1 (Tierra seca) | $32 \times 16\text{ px}$ | Base (Suelo) | `(0.5, 0.5)` | Rombo plano árido en tono ocre/tierra con borde pixelado. |
| `tile_2_grass.png` | 2 (Pasto) | $32 \times 16\text{ px}$ | Base (Suelo) | `(0.5, 0.5)` | Rombo plano verde claro que cubre la casilla. |
| `tile_3_bush.png` | 3 (Arbusto) | $32 \times 32\text{ px}$ | Overlay (Planta) | `(0.5, 0.75)` | Arbusto denso de follaje verde que se apoya sobre el pasto. |
| `tile_4_flowers.png` | 4 (Flores) | $32 \times 32\text{ px}$ | Overlay (Planta) | `(0.5, 0.8)` | Ramillete de flores rosadas con tallo que se apoya sobre el pasto. |
| `tile_5_tree.png` | 5 (Árbol) | $64 \times 80\text{ px}$ | Overlay (Planta) | `(0.5, 0.95)` | Árbol frondoso con tronco de madera y copa escalonada anclado al centro del pasto. |
| `tile_water.png` | 6 (Agua) | $32 \times 16\text{ px}$ | Base (Suelo) | `(0.5, 0.5)` | Rombo plano de agua azul con microondas isométricas y destellos blancos; mitiga evaporación. |
| `tile_rock.png` | 7 (Roca) | $32 \times 32\text{ px}$ | Overlay (Obstáculo) | `(0.5, 0.75)` | Formación rocosa gris multifacetada con musgo que bloquea cultivo e instalaciones. |
| `tile_polluted_soil.png` | Suelo bajo la basura (nivel de basura > 0; la etapa 8 está reservada) | $32 \times 16\text{ px}$ | Base (Suelo) | `(0.5, 0.5)` | Rombo árido degradado con vetas verdes de lixiviado y residuos enterrados (chapa, vidrio). |

> El simulador solo emite las etapas 1 a 7. La basura es un nivel por casilla (`trash`, 0 a 3), no una etapa: la escena dibuja `tile_polluted_soil.png` bajo la pila de basura. La etapa 8 queda mapeada al mismo sprite por contrato de arte, pero hoy no se produce. `trash_water.png` está reservado: la basura nunca cae en el agua. En la vista cenital de emergencia cada elemento tiene su propio color (`ValleyCanvas.tsx`).

### 2.2 Amenaza Urbana y Ciclo de Residuos (`tiles/`)

| Archivo | Elemento | Dimensiones | Capa | Anclaje | Descripción visual |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `building_house.png` | Edificio urbano | $32 \times 32\text{ px}$ | Overlay (Estructura) | `(0.5, 0.75)` | Casa isométrica cuadrada con base 2:1, techo en azotea romboide, puerta y ventana en perspectiva oblicua y chimenea con humo. |
| `trash_1_small.png` | Basura Nivel 1 | $32 \times 32\text{ px}$ | Overlay (Desecho) | `(0.5, 0.75)` | Desechos dispersos (lata de refresco roja, cartón marrón arrugado y botella plástica transparente). |
| `trash_2_medium.png` | Basura Nivel 2 | $32 \times 32\text{ px}$ | Overlay (Desecho) | `(0.5, 0.75)` | Pila acumulada en el centro del rombo (bolsas negras atadas, cajas de embalaje y latas). |
| `trash_3_large.png` | Basura Nivel 3 (Crítico)| $32 \times 32\text{ px}$ | Overlay (Desecho) | `(0.5, 0.75)` | Montículo desbordante con bidón oxidado, lixiviados viscosos, vapores tóxicos ondulantes y moscas. |
| `trash_water.png` | Basura acuática | $32 \times 32\text{ px}$ | Overlay sobre Agua | `(0.5, 0.75)` | Mancha tornasolada de hidrocarburo con bidón semihundido, bolsa flotante y estelas hídricas en el río. |

### 2.3 Instalaciones de las 3R (`installations/`)

| Archivo | Instalación | Dimensiones | Capa | Anclaje | Descripción visual |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `sprinkler.png` | `sprinkler` (Aspersor - Reutilizar) | $32 \times 32\text{ px}$ | Overlay | `(0.5, 0.75)` | Aspersor metálico con boquilla giratoria y agua pulverizada. |
| `drip.png` | `drip` (Riego por goteo - Reducir) | $32 \times 16\text{ px}$ | Overlay | `(0.5, 0.5)` | Tubería diagonal con válvula y gotas de precisión que bordea el rombo. |
| `treatment_plant.png` | `treatment_plant` (Planta - Reciclar) | $32 \times 32\text{ px}$ | Overlay | `(0.5, 0.75)` | Depósito de filtrado y tratamiento con grifería sobre suelo técnico. |
| `trash_bin.png` | `trash_bin` (Cesto de reciclaje) | $32 \times 32\text{ px}$ | Overlay | `(0.5, 0.75)` | Contenedor cilíndrico verde con aros metálicos, pedal y símbolo frontal de las 3 flechas. |

### 2.4 Efectos, Fuego e Indicadores (`effects/`)

| Archivo | Uso | Dimensiones | Capa | Anclaje | Descripción visual |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `fire.png` | Fuego estático base | $32 \times 32\text{ px}$ | Overlay | `(0.5, 0.75)` | Hoguera con leña quemada, brasas y llamas ascendentes con humo. |
| `fire_sheet.png` | Fuego animado | $128 \times 32\text{ px}$ | Overlay (4 frames $32 \times 32$) | `(0.5, 0.75)` | Hoja de sprites con 4 fotogramas para animación de llamas y brasas pulsantes. |
| `droplet.png` | Gota en el aire (acción pendiente) | $16 \times 16\text{ px}$ | Entidad flotante | `(0.5, 0.5)` | Gota de agua en trayectoria aérea. |
| `splash.png` | Salpicadura de gota aplicada | $32 \times 16\text{ px}$ | Overlay en suelo | `(0.5, 0.5)` | Onda expansiva de impacto de agua sobre el rombo. |
| `tile_selector.png` | Cursor de selección | $32 \times 16\text{ px}$ | Overlay en suelo | `(0.5, 0.5)` | Rombo amarillo brillante para resaltar la casilla seleccionada. |
| `clean_sparkle.png` | Efecto de limpieza de basura | $16 \times 16\text{ px}$ | Efecto visual | `(0.5, 0.5)` | Estrella en cruz de 4 puntas en cian y blanco con chispas doradas de éxito. |

---

## 3. Automatización, Validación y Calidad (QA)

El repositorio cuenta con herramientas dedicadas para garantizar la integridad visual del catálogo:
- **Tokens de Diseño:** [`src/frontend/assets/design_tokens.json`](design_tokens.json) documenta 118 colores autorizados clasificados en 5 familias cromáticas y las ecuaciones del rombo isométrico 2:1.
- **Validador Estricto:** [`scripts/validate_assets.py`](../../../scripts/validate_assets.py) (ejecutable con `pnpm run validate:assets`) verifica dimensiones, canal alfa, cajas de colisión y cumplimiento de paleta.
- **Generador Reproducible:** [`scripts/generate_sprites.py`](../../../scripts/generate_sprites.py) genera los 10 nuevos sprites directamente desde código fuente.
- **Prueba en Vitest:** [`src/frontend/src/logic/assets.test.ts`](../src/logic/assets.test.ts) valida automáticamente las cabeceras PNG y el reporte de calidad en cada corrida de `pnpm test`.

---

## 4. Licencias y Procedencia

Conforme a las reglas del repositorio (`CLAUDE.md`) y los criterios de aceptación de la tarea A3:

1. **Paquete base y arte original**:
   - **Obra base**: *Simple Cozy - Farm Assets - Free Pack*
   - **Autor original**: [Aibyzz](https://aibyzz.itch.io/) en itch.io.
   - **Licencia**: Libre (Free Pack en itch.io para uso en proyectos de juegos).
2. **Adaptación, nuevos sprites y derivación para Hydrovalley**:
   - **Adaptador y autor de derivaciones**: Fer2703 (diseño, composición por capas de 2:1 a $32 \times 16$, adaptación de elementos de las 3R, terreno natural, amenaza urbana y residuos).
   - **Licencia derivada**: CC0 1.0 Universal / Libre distribución para el proyecto educativo Hydrovalley.
3. **Efectos generados**:
   - `fire_sheet.png`, `droplet.png`, `splash.png`: Adaptados y generados para las animaciones y feedback visual de Hydrovalley (CC0).
4. **Sprites generados por el equipo**: los 10 archivos `tile_water.png`, `tile_rock.png`, `tile_polluted_soil.png`, `building_house.png`, `trash_1_small.png`, `trash_2_medium.png`, `trash_3_large.png`, `trash_water.png`, `trash_bin.png` y `clean_sparkle.png` los produce `scripts/generate_sprites.py` (verificado: es el conjunto exacto de PNG que escribe ese script).
   - **Fuente**: generado por el equipo con `scripts/generate_sprites.py` (autor: Fer2703), siguiendo `docs/plan-expansion-mecanicas.md` (rombo de $32 \times 16$, overlays alineados al pie, paleta de `design_tokens.json`).
   - **Licencia**: CC0 1.0 Universal / Libre distribución para el proyecto educativo Hydrovalley, como el resto de las derivaciones del equipo.
5. **Herramientas de validación**: `design_tokens.json` (paleta de referencia) y `scripts/validate_assets.py` (dimensiones, proporción 2:1, canal alfa y paleta) son del equipo (`feature/new-assets`); necesitan Pillow.
