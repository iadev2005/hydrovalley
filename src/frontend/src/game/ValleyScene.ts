import Phaser from "phaser";
import { type StateFrame } from "../logic/play.ts";
import { buildTiles, type Tile } from "../logic/valley.ts";
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
  lastStage: number;
  lastHydrated: boolean;
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

    // 2. Instalaciones de las 3R
    this.load.image("sprinkler", "/assets/installations/sprinkler.png");
    this.load.image("drip", "/assets/installations/drip.png");
    this.load.image("treatment_plant", "/assets/installations/treatment_plant.png");

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
          lastStage: 1,
          lastHydrated: false,
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
    cam.setZoom(0.5);

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

    // 4. Zoom con rueda del ratón
    this.input.on(
      "wheel",
      (
        _pointer: Phaser.Input.Pointer,
        _gameObjects: unknown,
        _deltaX: number,
        deltaY: number,
      ) => {
        const oldZoom = cam.zoom;
        const zoomFactor = deltaY > 0 ? 0.9 : 1.1;
        const nextZoom = Phaser.Math.Clamp(oldZoom * zoomFactor, 0.4, 2.5);
        if (nextZoom !== oldZoom) {
          cam.setZoom(nextZoom);
        }
      },
    );
  }

  /**
   * Resuelve el clic del puntero convirtiéndolo a coordenadas de casilla.
   */
  private handlePointerClick(pointer: Phaser.Input.Pointer): void {
    const cam = this.cameras.main;
    const worldPoint = cam.getWorldPoint(pointer.x, pointer.y);

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
    cam.setZoom(0.5);
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

    const domainTiles = buildTiles(state);
    if (domainTiles.length === 0) return;

    for (let i = 0; i < domainTiles.length; i += 1) {
      const tileData: Tile | undefined = domainTiles[i];
      const view: TileView | undefined = this.tiles[i];
      if (!tileData || !view) continue;

      this.updateSingleTile(view, tileData);
    }
  }

  private updateSingleTile(view: TileView, tile: Tile): void {
    // 1. Actualización diferencial de etapa de suelo y vegetación
    if (view.lastStage !== tile.stage) {
      view.lastStage = tile.stage;
      switch (tile.stage) {
        case 1: // Tierra seca
          view.baseSprite.setTexture("tile_1_dry");
          view.overlaySprite.setVisible(false);
          break;
        case 2: // Pasto
          view.baseSprite.setTexture("tile_2_grass");
          view.overlaySprite.setVisible(false);
          break;
        case 3: // Arbusto
          view.baseSprite.setTexture("tile_2_grass");
          view.overlaySprite
            .setTexture("tile_3_bush")
            .setOrigin(0.5, 0.75)
            .setVisible(true);
          break;
        case 4: // Flores
          view.baseSprite.setTexture("tile_2_grass");
          view.overlaySprite
            .setTexture("tile_4_flowers")
            .setOrigin(0.5, 0.8)
            .setVisible(true);
          break;
        case 5: // Árbol
          view.baseSprite.setTexture("tile_2_grass");
          view.overlaySprite
            .setTexture("tile_5_tree")
            .setOrigin(0.5, 0.95)
            .setVisible(true);
          break;
        default:
          view.baseSprite.setTexture("tile_1_dry");
          view.overlaySprite.setVisible(false);
          break;
      }
    }

    // 2. Tinte por humedad (humedad > 0 en pasto da un tono más hidratado)
    const isHydrated = tile.stage >= 2 && tile.humidity > 40n;
    if (view.lastHydrated !== isHydrated) {
      view.lastHydrated = isHydrated;
      if (isHydrated) {
        view.baseSprite.setTint(0xccffcc);
      } else {
        view.baseSprite.clearTint();
      }
    }

    // 3. Instalación
    const instKind = tile.installation ? tile.installation.kind : "";
    if (view.lastInstallationKind !== instKind) {
      view.lastInstallationKind = instKind;
      if (instKind) {
        if (this.textures.exists(instKind)) {
          view.installationSprite.setTexture(instKind);
        } else {
          view.installationSprite.setTexture("sprinkler");
        }

        // Las instalaciones verticales 32x32 (potabilizadora y aspersor) tienen su
        // base en y=24: se anclan con origin (0.5, 0.75) para que su rombo base calce
        // con la casilla y la estructura se eleve sin enterrarse en el suelo.
        // El goteo (32x16) es plano al ras del suelo y usa origin (0.5, 0.5).
        if (instKind === "treatment_plant" || instKind === "sprinkler") {
          view.installationSprite.setOrigin(0.5, 0.75);
        } else {
          view.installationSprite.setOrigin(0.5, 0.5);
        }

        view.installationSprite.setVisible(true);
      } else {
        view.installationSprite.setVisible(false);
      }
    }

    // 4. Fuego (creación perezosa de fireBadgeText solo para casillas en llamas)
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
