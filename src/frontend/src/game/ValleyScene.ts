import Phaser from "phaser";
import { type StateFrame, type PlacedInstallation } from "../logic/play.ts";
import { buildTiles, isTileInDripArea, type Tile } from "../logic/valley.ts";
import {
  gridToIsometric,
  isometricToGrid,
  getIsometricDepth,
  ISO_TILE_WIDTH,
  ISO_TILE_HEIGHT,
  VALLEY_GRID_SIZE,
} from "../logic/isometric.ts";

export interface TileView {
  x: number;
  y: number;
  screenX: number;
  screenY: number;
  depth: number;
  baseSprite: Phaser.GameObjects.Image;
  overlaySprite: Phaser.GameObjects.Image;
  installationSprite: Phaser.GameObjects.Image;
  fireSprite: Phaser.GameObjects.Sprite;
  fireBadgeText: Phaser.GameObjects.Text | null;
  lastGroundKey: string;
  lastTrash: number;
  lastInstallationKind: string;
  lastFireDrops: number;
}

interface PendingDropItem {
  gridX: number;
  gridY: number;
  droplet: Phaser.GameObjects.Image;
  targetX: number;
  targetY: number;
  floatTween: Phaser.Tweens.Tween;
}

const MAX_VISUAL_PENDING_DROPS = 24;

export type TileSelectedCallback = (coord: { x: number; y: number }) => void;

export class ValleyScene extends Phaser.Scene {
  public static readonly KEY = "ValleyScene";

  private tiles: TileView[] = [];
  private selectorSprite: Phaser.GameObjects.Image | null = null;
  private originX = 512;
  private originY = 60;
  private isMapReady = false;

  private pendingState: StateFrame | null = null;
  private pendingSelected: { x: number; y: number } | null = null;
  private tileSelectedCallback: TileSelectedCallback | null = null;

  // Interacción de cámara
  private isDragging = false;
  private dragStartX = 0;
  private dragStartY = 0;
  private dragStartScrollX = 0;
  private dragStartScrollY = 0;
  private hasMovedPastThreshold = false;
  private readonly DRAG_THRESHOLD = 6;

  // Acciones pendientes ("gota en el aire") y efectos en resolución
  private pendingDrops: Map<string, PendingDropItem> = new Map();
  private activeEffects: Set<Phaser.GameObjects.Image> = new Set();
  private currentInstallations: PlacedInstallation[] = [];

  constructor() {
    super({ key: ValleyScene.KEY });
  }

  public preload(): void {
    // 1. Terreno base y vegetación por etapas
    this.load.image("tile_1_dry", "/assets/tiles/tile_1_dry.png");
    this.load.image("tile_2_grass", "/assets/tiles/tile_2_grass.png");
    this.load.image("tile_3_bush", "/assets/tiles/tile_3_bush.png");
    this.load.image("tile_4_flowers", "/assets/tiles/tile_4_flowers.png");
    this.load.image("tile_5_tree", "/assets/tiles/tile_5_tree.png");

    // 2. Instalaciones de las 3R y reciclaje
    this.load.image("sprinkler", "/assets/installations/sprinkler.png");
    this.load.image("drip", "/assets/installations/drip.png");
    this.load.image("treatment_plant", "/assets/installations/treatment_plant.png");
    this.load.image("trash_bin", "/assets/installations/trash_bin.png");

    // 2b. Terreno natural, estructuras urbanas y basura
    this.load.image("building_house", "/assets/tiles/building_house.png");
    this.load.image("trash_1_small", "/assets/tiles/trash_1_small.png");
    this.load.image("trash_2_medium", "/assets/tiles/trash_2_medium.png");
    this.load.image("trash_3_large", "/assets/tiles/trash_3_large.png");
    this.load.image("tile_polluted_soil", "/assets/tiles/tile_polluted_soil.png");
    this.load.image("tile_water", "/assets/tiles/tile_water.png");
    this.load.image("tile_rock", "/assets/tiles/tile_rock.png");
    this.load.image("clean_sparkle", "/assets/effects/clean_sparkle.png");

    // 3. Fuego, gotas y efectos
    this.load.spritesheet("fire_sheet", "/assets/effects/fire_sheet.png", {
      frameWidth: 32,
      frameHeight: 32,
    });
    this.load.image("fire", "/assets/effects/fire.png");
    this.load.image("droplet", "/assets/effects/droplet.png");
    this.load.image("splash", "/assets/effects/splash.png");
    this.load.image("tile_selector", "/assets/effects/tile_selector.png");
  }

  public create(): void {
    this.originX = this.scale.width / 2;
    this.originY = 50;

    // Animación cíclica de llamas
    if (!this.anims.exists("fire_burn")) {
      this.anims.create({
        key: "fire_burn",
        frames: this.anims.generateFrameNumbers("fire_sheet", { start: 0, end: 3 }),
        frameRate: 8,
        repeat: -1,
      });
    }

    // Inicializar el pool de 1024 casillas (32x32) una sola vez
    this.tiles = [];
    for (let y = 0; y < VALLEY_GRID_SIZE; y += 1) {
      for (let x = 0; x < VALLEY_GRID_SIZE; x += 1) {
        const screenPos = gridToIsometric(
          x,
          y,
          this.originX,
          this.originY,
          ISO_TILE_WIDTH,
          ISO_TILE_HEIGHT,
        );
        const depth = getIsometricDepth(x, y, VALLEY_GRID_SIZE);

        // 1. Suelo base (rombo plano 32x16)
        const baseSprite = this.add
          .image(screenPos.x, screenPos.y, "tile_1_dry")
          .setOrigin(0.5, 0.5)
          .setDepth(depth);

        // 2. Vegetación superpuesta (arbusto, flores, árbol)
        const overlaySprite = this.add
          .image(screenPos.x, screenPos.y, "tile_3_bush")
          .setVisible(false)
          .setDepth(depth + 0.1);

        // 3. Instalación de las 3R
        const installationSprite = this.add
          .image(screenPos.x, screenPos.y, "sprinkler")
          .setOrigin(0.5, 0.75)
          .setVisible(false)
          .setDepth(depth + 0.2);

        // 4. Fuego animado (32x32 con base romboide en y=24: anclado en 0.75 calza perfecto con la casilla)
        const fireSprite = this.add
          .sprite(screenPos.x, screenPos.y, "fire_sheet")
          .setOrigin(0.5, 0.75)
          .setVisible(false)
          .setDepth(depth + 0.3);

        // Nota: fireBadgeText se instancia bajo demanda únicamente en las casillas
        // en llamas para evitar crear 1024 lienzos Canvas2D y 1024 texturas WebGL
        // al iniciar la escena (causa principal de pérdida de contexto WebGL).
        this.tiles.push({
          x,
          y,
          screenX: screenPos.x,
          screenY: screenPos.y,
          depth,
          baseSprite,
          overlaySprite,
          installationSprite,
          fireSprite,
          fireBadgeText: null,
          lastGroundKey: "",
          lastTrash: 0,
          lastInstallationKind: "",
          lastFireDrops: -1,
        });
      }
    }

    // Cursor selector
    this.selectorSprite = this.add
      .image(0, 0, "tile_selector")
      .setOrigin(0.5, 0.5)
      .setVisible(false)
      .setDepth(10000);

    // Configuración de controles de cámara y clics
    this.setupCameraAndInput();

    this.events.once(Phaser.Scenes.Events.SHUTDOWN, () => {
      this.clearPendingDrops();
    });

    // Pulso periódico visual de las plantas de tratamiento (cada 2 segundos)
    this.time.addEvent({
      delay: 2000,
      loop: true,
      callback: () => {
        this.emitTreatmentPlantPulses();
      },
    });

    this.isMapReady = true;

    // Aplicar estado o selección pendientes si llegaron antes de create()
    if (this.pendingState) {
      this.updateState(this.pendingState);
      this.pendingState = null;
    }
    if (this.pendingSelected) {
      this.setSelectedTile(this.pendingSelected);
      this.pendingSelected = null;
    }
  }

  /**
   * Configura arrastre (pan), zoom con rueda y detección de clic sobre casillas.
   */
  private setupCameraAndInput(): void {
    const cam = this.cameras.main;
    cam.setZoom(0.65);

    // Centrar inicialmente en el punto medio del valle
    const centerY = this.originY + (VALLEY_GRID_SIZE * ISO_TILE_HEIGHT) / 2;
    cam.centerOn(this.originX, centerY);

    // 1. Inicio de arrastre
    this.input.on("pointerdown", (pointer: Phaser.Input.Pointer) => {
      if (pointer.button === 0 || pointer.button === 1) {
        this.isDragging = true;
        this.dragStartX = pointer.x;
        this.dragStartY = pointer.y;
        this.dragStartScrollX = cam.scrollX;
        this.dragStartScrollY = cam.scrollY;
        this.hasMovedPastThreshold = false;
      }
    });

    // 2. Movimiento de arrastre (Pan)
    this.input.on("pointermove", (pointer: Phaser.Input.Pointer) => {
      if (this.isDragging && pointer.isDown) {
        const dx = pointer.x - this.dragStartX;
        const dy = pointer.y - this.dragStartY;

        if (
          !this.hasMovedPastThreshold &&
          (Math.abs(dx) > this.DRAG_THRESHOLD || Math.abs(dy) > this.DRAG_THRESHOLD)
        ) {
          this.hasMovedPastThreshold = true;
        }

        if (this.hasMovedPastThreshold) {
          cam.scrollX = this.dragStartScrollX - dx / cam.zoom;
          cam.scrollY = this.dragStartScrollY - dy / cam.zoom;
        }
      }
    });

    // 3. Fin de arrastre o Clic
    this.input.on("pointerup", (pointer: Phaser.Input.Pointer) => {
      if (this.isDragging) {
        this.isDragging = false;
        if (!this.hasMovedPastThreshold && pointer.button === 0) {
          this.handlePointerClick(pointer);
        }
      }
    });

  }

  /** Ajusta el zoom desde el contenedor del mapa sin desplazar la página. */
  public zoomBy(deltaY: number): void {
    const cam = this.cameras.main;
    const zoomFactor = deltaY > 0 ? 0.9 : 1.1;
    cam.setZoom(Phaser.Math.Clamp(cam.zoom * zoomFactor, 0.4, 2.5));
  }

  /**
   * Resuelve el clic del puntero convirtiéndolo a coordenadas de casilla.
   */
  private handlePointerClick(pointer: Phaser.Input.Pointer): void {
    const cam = this.cameras.main;
    const canvas = this.game.canvas;
    const rect = canvas.getBoundingClientRect();
    const gameWidth = this.scale.width;
    const gameHeight = this.scale.height;
    const scale = Math.min(rect.width / gameWidth, rect.height / gameHeight);
    const offsetX = (rect.width - gameWidth * scale) / 2;
    const offsetY = (rect.height - gameHeight * scale) / 2;
    const event = pointer.event;
    const hasClientCoordinates = event !== undefined && "clientX" in event && "clientY" in event;
    const clientX = hasClientCoordinates
      ? event.clientX
      : rect.left + pointer.x * (rect.width / gameWidth);
    const clientY = hasClientCoordinates
      ? event.clientY
      : rect.top + pointer.y * (rect.height / gameHeight);
    const gameX = (clientX - rect.left - offsetX) / scale;
    const gameY = (clientY - rect.top - offsetY) / scale;
    const worldPoint = cam.getWorldPoint(gameX, gameY);

    const tile = isometricToGrid(
      worldPoint.x,
      worldPoint.y,
      this.originX,
      this.originY,
      VALLEY_GRID_SIZE,
      ISO_TILE_WIDTH,
      ISO_TILE_HEIGHT,
    );

    if (tile) {
      this.setSelectedTile(tile);
      this.events.emit("tile-selected", tile);
      if (this.tileSelectedCallback) {
        this.tileSelectedCallback(tile);
      }
    }
  }

  /**
   * Registra el callback invocado cuando el usuario hace clic sobre una casilla.
   */
  public onSelectTile(cb: TileSelectedCallback): void {
    this.tileSelectedCallback = cb;
  }

  /**
   * Centra la cámara de nuevo en el centro del valle y restablece el zoom a 1x.
   */
  public resetCamera(): void {
    const cam = this.cameras.main;
    const centerY = this.originY + (VALLEY_GRID_SIZE * ISO_TILE_HEIGHT) / 2;
    cam.setZoom(0.65);
    cam.centerOn(this.originX, centerY);
  }

  /**
   * Muestra una gota "en el aire" flotando sobre la casilla destino mientras la
   * acción está pendiente (RF-04). Limita las gotas visuales simultáneas para evitar
   * saturar el motor de tweens/WebGL cuando se envían ráfagas de gotas.
   */
  public spawnPendingDrop(actionId: string, gridX: number, gridY: number): void {
    if (!this.isMapReady || !this.sys?.isActive()) return;

    // Si ya existe una gota para este actionId, no duplicar
    if (this.pendingDrops.has(actionId)) return;

    // Si ya hay una gota flotando sobre esta misma casilla o se alcanzó el tope global,
    // descartar la más antigua para mantener el número de GameObjects y tweens acotado.
    for (const [existingId, existingItem] of this.pendingDrops.entries()) {
      if (existingItem.gridX === gridX && existingItem.gridY === gridY) {
        existingItem.floatTween.stop();
        existingItem.droplet.destroy();
        this.pendingDrops.delete(existingId);
        break;
      }
    }
    while (this.pendingDrops.size >= MAX_VISUAL_PENDING_DROPS) {
      const oldestKey = this.pendingDrops.keys().next().value;
      if (oldestKey === undefined) break;
      const oldest = this.pendingDrops.get(oldestKey);
      if (oldest) {
        oldest.floatTween.stop();
        oldest.droplet.destroy();
      }
      this.pendingDrops.delete(oldestKey);
    }

    const screenPos = gridToIsometric(
      gridX,
      gridY,
      this.originX,
      this.originY,
      ISO_TILE_WIDTH,
      ISO_TILE_HEIGHT,
    );
    const depth = getIsometricDepth(gridX, gridY, VALLEY_GRID_SIZE) + 500;

    // La gota aparece a 30 px sobre la casilla y oscila suavemente en el aire
    const droplet = this.add
      .image(screenPos.x, screenPos.y - 30, "droplet")
      .setOrigin(0.5, 0.5)
      .setDepth(depth);

    const floatTween = this.tweens.add({
      targets: droplet,
      y: screenPos.y - 20,
      duration: 300,
      ease: "Sine.easeInOut",
      yoyo: true,
      repeat: -1,
    });

    this.pendingDrops.set(actionId, {
      gridX,
      gridY,
      droplet,
      targetX: screenPos.x,
      targetY: screenPos.y,
      floatTween,
    });
  }

  /**
   * Resuelve una acción pendiente:
   * - "applied" o "duplicate": la gota cae al suelo e impacta con salpicadura (splash).
   * - "rejected" o "expired": la gota se evapora hacia arriba con fade out.
   */
  public resolvePendingDrop(actionId: string, status: string): void {
    const item = this.pendingDrops.get(actionId);
    if (!item) return;

    this.pendingDrops.delete(actionId);
    item.floatTween.stop();

    if (!this.sys?.isActive()) {
      item.droplet.destroy();
      return;
    }

    this.activeEffects.add(item.droplet);

    if (status === "applied" || status === "duplicate") {
      // Impacto y salpicadura
      const targetDepth = item.droplet.depth;
      this.tweens.add({
        targets: item.droplet,
        y: item.targetY,
        duration: 150,
        ease: "Cubic.easeIn",
        onComplete: () => {
          this.activeEffects.delete(item.droplet);
          item.droplet.destroy();

          if (!this.sys?.isActive()) return;

          const splash = this.add
            .image(item.targetX, item.targetY, "splash")
            .setOrigin(0.5, 0.5)
            .setScale(0.5)
            .setAlpha(1.0)
            .setDepth(targetDepth);
          this.activeEffects.add(splash);

          this.tweens.add({
            targets: splash,
            scale: 1.2,
            alpha: 0,
            duration: 350,
            ease: "Quad.easeOut",
            onComplete: () => {
              this.activeEffects.delete(splash);
              splash.destroy();
            },
          });
        },
      });
    } else {
      // Rechazada o expirada: se evapora
      this.tweens.add({
        targets: item.droplet,
        y: item.droplet.y - 15,
        alpha: 0,
        duration: 300,
        ease: "Quad.easeOut",
        onComplete: () => {
          this.activeEffects.delete(item.droplet);
          item.droplet.destroy();
        },
      });
    }
  }

  /**
   * Limpia todas las gotas pendientes y efectos en curso (por ejemplo, al desconectarse).
   */
  public clearPendingDrops(): void {
    for (const item of this.pendingDrops.values()) {
      item.floatTween.stop();
      item.droplet.destroy();
    }
    this.pendingDrops.clear();

    for (const effect of this.activeEffects) {
      this.tweens?.killTweensOf(effect);
      effect.destroy();
    }
    this.activeEffects.clear();
  }

  /**
   * Actualiza el estado visual de las 1024 casillas a partir del StateFrame difundido por el hub.
   * Realiza actualización diferencial en el pool de sprites sin destruir ningún GameObject.
   */
  public updateState(state: StateFrame | null): void {
    if (!this.isMapReady) {
      this.pendingState = state;
      return;
    }
    if (!state || state.stages.length === 0) return;

    this.currentInstallations = state.installations;

    // Coordenadas de los goteos activos para calcular el sombreado 5x5
    const dripPositions = state.installations
      .filter((inst) => inst.installation === "drip")
      .map((inst) => ({ x: inst.x, y: inst.y }));

    const domainTiles = buildTiles(state);
    if (domainTiles.length === 0) return;

    for (let i = 0; i < domainTiles.length; i += 1) {
      const tileData: Tile | undefined = domainTiles[i];
      const view: TileView | undefined = this.tiles[i];
      if (!tileData || !view) continue;

      const inDripArea = isTileInDripArea(tileData.x, tileData.y, dripPositions);

      this.updateSingleTile(view, tileData, inDripArea);
    }
  }

  /**
   * Emite ondas de pulso visual desde todas las plantas de tratamiento activas.
   */
  private emitTreatmentPlantPulses(): void {
    if (!this.isMapReady || !this.currentInstallations) return;
    for (const inst of this.currentInstallations) {
      if (inst.installation === "treatment_plant") {
        this.spawnPulseEffect(inst.x, inst.y);
      }
    }
  }

  /**
   * Efecto visual de pulso expansivo de agua con splash.png cubriendo el área 3x3.
   */
  private spawnPulseEffect(gridX: number, gridY: number): void {
    const screenPos = gridToIsometric(
      gridX,
      gridY,
      this.originX,
      this.originY,
      ISO_TILE_WIDTH,
      ISO_TILE_HEIGHT,
    );
    const depth = getIsometricDepth(gridX, gridY, VALLEY_GRID_SIZE) + 10;

    // Onda primaria de pulso hídrico
    const pulse1 = this.add
      .image(screenPos.x, screenPos.y, "splash")
      .setOrigin(0.5, 0.5)
      .setDepth(depth)
      .setScale(0.8)
      .setAlpha(0.9)
      .setTint(0x40c4ff);

    this.tweens.add({
      targets: pulse1,
      scaleX: 2.8,
      scaleY: 2.8,
      alpha: 0,
      duration: 800,
      ease: "Cubic.easeOut",
      onComplete: () => {
        pulse1.destroy();
      },
    });

    // Onda secundaria concéntrica para mayor riqueza visual
    this.time.delayedCall(150, () => {
      if (!this.scene.isActive()) return;
      const pulse2 = this.add
        .image(screenPos.x, screenPos.y, "splash")
        .setOrigin(0.5, 0.5)
        .setDepth(depth)
        .setScale(0.5)
        .setAlpha(0.7)
        .setTint(0x80d8ff);

      this.tweens.add({
        targets: pulse2,
        scaleX: 3.2,
        scaleY: 3.2,
        alpha: 0,
        duration: 900,
        ease: "Quad.easeOut",
        onComplete: () => {
          pulse2.destroy();
        },
      });
    });
  }

  /** Destello breve sobre una casilla a la que se le quitó un nivel de basura. */
  private spawnCleanSparkle(view: TileView): void {
    if (!this.sys?.isActive() || !this.textures.exists("clean_sparkle")) return;
    const sparkle = this.add
      .image(view.screenX, view.screenY - 4, "clean_sparkle")
      .setOrigin(0.5, 0.5)
      .setDepth(view.depth + 0.5);
    this.activeEffects.add(sparkle);
    this.tweens.add({
      targets: sparkle,
      y: view.screenY - 16,
      alpha: 0,
      duration: 600,
      ease: "Quad.easeOut",
      onComplete: () => {
        this.activeEffects.delete(sparkle);
        sparkle.destroy();
      },
    });
  }

  /**
   * Textura y capa superior de una casilla. El suelo base y el overlay se eligen
   * juntos porque la estructura urbana, la basura, el agua y la roca sustituyen
   * a la vegetación.
   */
  private applyGround(view: TileView, tile: Tile, inDripArea: boolean): void {
    const { baseSprite, overlaySprite } = view;
    baseSprite.clearTint();
    overlaySprite.setVisible(false);

    if (tile.has_building) {
      baseSprite.setTexture("tile_1_dry");
      overlaySprite.setTexture("building_house").setOrigin(0.5, 0.75).setVisible(true);
      return;
    }
    const trash = tile.trash_level ?? 0;
    if (trash > 0) {
      baseSprite.setTexture("tile_polluted_soil");
      const trashTexture =
        trash >= 3 ? "trash_3_large" : trash === 2 ? "trash_2_medium" : "trash_1_small";
      overlaySprite.setTexture(trashTexture).setOrigin(0.5, 0.75).setVisible(true);
      return;
    }
    switch (tile.stage) {
      case 2: // Pasto
        baseSprite.setTexture("tile_2_grass");
        break;
      case 3: // Arbusto
        baseSprite.setTexture("tile_2_grass");
        overlaySprite.setTexture("tile_3_bush").setOrigin(0.5, 0.75).setVisible(true);
        break;
      case 4: // Flores
        baseSprite.setTexture("tile_2_grass");
        overlaySprite.setTexture("tile_4_flowers").setOrigin(0.5, 0.8).setVisible(true);
        break;
      case 5: // Árbol
        baseSprite.setTexture("tile_2_grass");
        overlaySprite.setTexture("tile_5_tree").setOrigin(0.5, 0.95).setVisible(true);
        break;
      case 6: // Agua
        baseSprite.setTexture("tile_water");
        return;
      case 7: // Roca
        baseSprite.setTexture("tile_1_dry");
        overlaySprite.setTexture("tile_rock").setOrigin(0.5, 0.75).setVisible(true);
        return;
      default: // Tierra seca
        baseSprite.setTexture("tile_1_dry");
        break;
    }

    // Tinte por sombreado de suelo húmedo (área de goteo 5x5) o humedad alta.
    if (inDripArea) {
      baseSprite.setTint(tile.stage === 1 ? 0x9ab2c0 : 0x80e8c0);
    } else if (tile.stage >= 2 && tile.humidity > 40n) {
      baseSprite.setTint(0xccffcc);
    }
  }

  private updateSingleTile(view: TileView, tile: Tile, inDripArea = false): void {
    // 1. Suelo y capa superior: solo se repinta si cambia algo visible.
    const groundKey = [
      tile.stage,
      tile.has_building ? 1 : 0,
      tile.trash_level ?? 0,
      tile.stage >= 2 && tile.humidity > 40n ? 1 : 0,
      inDripArea ? 1 : 0,
    ].join("|");
    if (view.lastGroundKey !== groundKey) {
      // Destello cuando baja el nivel de basura (a mano o por un cesto), salvo en
      // el primer estado que llega: ahí no se limpió nada.
      const trash = tile.trash_level ?? 0;
      if (view.lastGroundKey !== "" && trash < view.lastTrash) this.spawnCleanSparkle(view);
      view.lastTrash = trash;
      view.lastGroundKey = groundKey;
      this.applyGround(view, tile, inDripArea);
    }

    // 2. Instalación
    const instKind = tile.installation ? tile.installation.kind : "";
    if (view.lastInstallationKind !== instKind) {
      view.lastInstallationKind = instKind;
      if (instKind) {
        view.installationSprite.setTexture(this.textures.exists(instKind) ? instKind : "sprinkler");

        // Las instalaciones verticales 32x32 (planta, aspersor y cesto) tienen su
        // base en y=24: se anclan con origin (0.5, 0.75) para que su rombo base calce
        // con la casilla. El goteo (32x16) es plano al ras del suelo (0.5, 0.5).
        if (instKind === "treatment_plant" || instKind === "sprinkler" || instKind === "trash_bin") {
          view.installationSprite.setOrigin(0.5, 0.75);
        } else {
          view.installationSprite.setOrigin(0.5, 0.5);
        }
        view.installationSprite.setVisible(true);
      } else {
        view.installationSprite.setVisible(false);
      }
    }

    // 3. Fuego (creación perezosa de fireBadgeText solo para casillas en llamas)
    const fireDrops = tile.fire ? tile.fire.drops_received : -1;
    if (view.lastFireDrops !== fireDrops) {
      view.lastFireDrops = fireDrops;
      if (tile.fire) {
        if (!view.fireSprite.anims.isPlaying) {
          view.fireSprite.play("fire_burn", true);
        }
        view.fireSprite.setVisible(true);
        if (!view.fireBadgeText) {
          view.fireBadgeText = this.add
            .text(view.screenX, view.screenY - 24, `${tile.fire.drops_received}/3`, {
              fontSize: "9px",
              fontFamily: "monospace",
              color: "#ffffff",
              backgroundColor: "rgba(0,0,0,0.7)",
              padding: { x: 2, y: 1 },
            })
            .setOrigin(0.5, 0.5)
            .setDepth(view.depth + 0.4);
        } else {
          view.fireBadgeText
            .setText(`${tile.fire.drops_received}/3`)
            .setVisible(true);
        }
      } else {
        if (view.fireSprite.anims.isPlaying) {
          view.fireSprite.stop();
        }
        view.fireSprite.setVisible(false);
        if (view.fireBadgeText) {
          view.fireBadgeText.destroy();
          view.fireBadgeText = null;
        }
      }
    }
  }

  /**
   * Posiciona o desactiva el cursor de selección sobre la casilla indicada.
   */
  public setSelectedTile(coord: { x: number; y: number } | null): void {
    if (!this.isMapReady || !this.selectorSprite) {
      this.pendingSelected = coord;
      return;
    }

    if (!coord) {
      this.selectorSprite.setVisible(false);
      return;
    }

    const pos = gridToIsometric(
      coord.x,
      coord.y,
      this.originX,
      this.originY,
      ISO_TILE_WIDTH,
      ISO_TILE_HEIGHT,
    );
    this.selectorSprite.setPosition(pos.x, pos.y).setVisible(true);
  }

  public getOrigin(): { x: number; y: number } {
    return { x: this.originX, y: this.originY };
  }
}
