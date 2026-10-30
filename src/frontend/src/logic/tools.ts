// Herramientas del jugador y clic contextual, sin DOM. Con «Interactuar» la
// casilla decide la acción (nunca hay dos posibles a la vez); con una
// instalación elegida cada clic la coloca, como un pincel. Las reglas imitan
// las del simulador (world.rs: `drop`, `clean_trash`, `place_installation`)
// solo para avisar antes de enviar: quien decide sigue siendo el servidor.

import type { StateFrame } from "./play.ts";
import { stageName } from "./valley.ts";

export const INSTALLATION_KINDS = ["sprinkler", "drip", "treatment_plant", "trash_bin"] as const;
export type InstallationKind = (typeof INSTALLATION_KINDS)[number];
export type ToolId = "interact" | InstallationKind;

export interface ToolInfo {
  id: ToolId;
  name: string;
  /** Función en pocas palabras, bajo el nombre. */
  short: string;
  /** Para qué sirve, al pasar el ratón o con la herramienta activa. */
  description: string;
}

// Sin cifras del catálogo: cambian sin tocar código (CLAUDE.md).
export const TOOLS: readonly ToolInfo[] = [
  {
    id: "interact",
    name: "Interactuar",
    short: "Regar · recoger · apagar",
    description:
      "Haz clic en una casilla y se hace la única acción posible: regar si hay tierra o planta, recoger si hay basura y echar agua si hay fuego.",
  },
  {
    id: "sprinkler",
    name: "Aspersor",
    short: "Reutilizar · riega solo",
    description:
      "Riega solo, cada pocos segundos, las casillas de su alrededor. Se gasta con el uso.",
  },
  {
    id: "drip",
    name: "Riego por goteo",
    short: "Reducir · menos evaporación",
    description:
      "En su área el agua se evapora más despacio, las gotas rinden el doble y las plantas quedan protegidas del fuego. Se gasta con el uso.",
  },
  {
    id: "treatment_plant",
    name: "Planta de tratamiento",
    short: "Reciclar · rellena tu agua",
    description: "Devuelve agua a tu depósito cada pocos segundos. Se gasta con el uso.",
  },
  {
    id: "trash_bin",
    name: "Papelera",
    short: "Limpia basura cerca",
    description: "Recoge sola la basura de las casillas de su alrededor. Se gasta con el uso.",
  },
];

export interface UpgradeInfo {
  id: "bigger_drop" | "bigger_tank";
  name: string;
  description: string;
}

export const UPGRADES: readonly UpgradeInfo[] = [
  {
    id: "bigger_drop",
    name: "Gota grande",
    description: "Tus gotas mojan también las casillas de alrededor.",
  },
  { id: "bigger_tank", name: "Depósito mayor", description: "Cabe más agua en tu depósito." },
];

export function isInstallationTool(tool: string): tool is InstallationKind {
  return (INSTALLATION_KINDS as readonly string[]).includes(tool);
}

export function toolName(tool: string): string {
  return TOOLS.find((t) => t.id === tool)?.name ?? tool;
}

const WATER = 6;
const ROCK = 7;

type TileState = Pick<
  StateFrame,
  "width" | "height" | "stages" | "humidity" | "fires" | "installations" | "buildings" | "trash"
>;

interface TileFacts {
  stage: number;
  humidity: bigint;
  building: boolean;
  trash: number;
  fireDrops: number | null;
  installation: { kind: string; durability: number } | null;
}

function factsAt(state: TileState | null, { x, y }: { x: number; y: number }): TileFacts | null {
  if (!state || state.stages.length === 0) return null;
  if (x < 0 || y < 0 || x >= state.width || y >= state.height) return null;
  const idx = y * state.width + x;
  const stage = state.stages[idx];
  if (stage === undefined) return null;
  const fire = state.fires.find((f) => f.x === x && f.y === y);
  const inst = state.installations.find((i) => i.x === x && i.y === y);
  return {
    stage,
    humidity: state.humidity[idx] ?? 0n,
    building: state.buildings[idx] === true,
    trash: state.trash[idx] ?? 0,
    fireDrops: fire ? fire.drops_received : null,
    installation: inst ? { kind: inst.installation, durability: inst.remaining_durability } : null,
  };
}

/** Por qué no se puede hacer nada en la casilla, o `null` si admite acciones. */
function blockedReason(facts: TileFacts): string | null {
  if (facts.building) return "Es un edificio";
  if (facts.stage === WATER) return "El río no se riega";
  if (facts.stage === ROCK) return "Es una roca";
  return null;
}

export interface TileAction {
  kind: "drop" | "clean_trash" | "none";
  label: string;
}

/**
 * La única acción de «Interactuar» en la casilla. El simulador rechaza la
 * gota en agua, roca, edificio o basura, y limpiar sin basura; sí la acepta
 * sobre una instalación o un fuego.
 */
export function interactionFor(
  state: TileState | null,
  coord: { x: number; y: number },
): TileAction {
  const facts = factsAt(state, coord);
  if (!facts) return { kind: "none", label: "Sin datos del mapa todavía" };
  const blocked = blockedReason(facts);
  if (blocked) return { kind: "none", label: blocked };
  if (facts.trash > 0) return { kind: "clean_trash", label: "Recoger basura" };
  if (facts.fireDrops !== null) {
    const drops = facts.fireDrops === 1 ? "1 gota recibida" : `${facts.fireDrops} gotas recibidas`;
    return { kind: "drop", label: `Echar agua al fuego (${drops})` };
  }
  return { kind: "drop", label: "Regar" };
}

/** Si la instalación cabe en la casilla (las hojas las comprueba el servidor). */
export function placementFor(
  state: TileState | null,
  coord: { x: number; y: number },
  tool: InstallationKind,
): { ok: boolean; label: string } {
  const facts = factsAt(state, coord);
  if (!facts) return { ok: false, label: "Sin datos del mapa todavía" };
  if (facts.installation) return { ok: false, label: "Ya hay una instalación aquí" };
  if (facts.building) return { ok: false, label: "No se puede construir sobre un edificio" };
  if (facts.stage === WATER) return { ok: false, label: "No se puede construir en el río" };
  if (facts.stage === ROCK) return { ok: false, label: "No se puede construir sobre una roca" };
  if (facts.trash > 0) return { ok: false, label: "Primero recoge la basura" };
  return { ok: true, label: `Colocar ${toolName(tool).toLowerCase()}` };
}

/**
 * Si el jugador puede pagar una gota: con el depósito vacío el simulador la
 * rechaza (`insufficient_water`), así que no se envía. En modo demo la gota
 * no gasta agua, y antes del primer comando el estado aún no trae al jugador:
 * en esos casos decide el servidor.
 */
export function hasWaterForDrop(
  state: Pick<StateFrame, "demo_mode"> & { player: { water: bigint } | null } | null,
): boolean {
  if (!state || state.demo_mode || !state.player) return true;
  return state.player.water > 0n;
}

export interface TileTooltip {
  title: string;
  coord: string;
  /** Humedad en texto (u64), o `null` donde no aplica (río, roca, edificio). */
  humidity: string | null;
  detail: string | null;
  action: string;
  ok: boolean;
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function tileTitle(facts: TileFacts): string {
  if (facts.building) return "Edificio";
  if (facts.stage === WATER) return "Río";
  if (facts.trash > 0) return `Basura (nivel ${facts.trash})`;
  const ground = stageName(facts.stage);
  if (facts.installation) return `${toolName(facts.installation.kind)} sobre ${ground}`;
  if (facts.fireDrops !== null) return `${capitalize(ground)} en llamas`;
  return capitalize(ground);
}

/** Lo que se ve al pasar el ratón: qué es la casilla y qué hará el clic. */
export function tileTooltip(
  state: TileState | null,
  coord: { x: number; y: number },
  tool: ToolId,
): TileTooltip | null {
  const facts = factsAt(state, coord);
  if (!facts) return null;
  const natural = facts.building || facts.stage === WATER || facts.stage === ROCK;
  let action: string;
  let ok: boolean;
  if (isInstallationTool(tool)) {
    const placement = placementFor(state, coord, tool);
    ok = placement.ok;
    action = ok ? `Clic: ${placement.label.toLowerCase()}` : placement.label;
  } else {
    const interaction = interactionFor(state, coord);
    ok = interaction.kind !== "none";
    action = ok ? `Clic: ${interaction.label.charAt(0).toLowerCase()}${interaction.label.slice(1)}` : interaction.label;
  }
  return {
    title: tileTitle(facts),
    coord: `(${coord.x}, ${coord.y})`,
    humidity: natural || facts.trash > 0 ? null : String(facts.humidity),
    detail: facts.installation ? `Le quedan ${facts.installation.durability} usos` : null,
    action,
    ok,
  };
}
