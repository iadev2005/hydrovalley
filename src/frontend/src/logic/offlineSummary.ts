// Texto del resumen al volver (RF-25): «Mientras no estabas, tus aspersores
// hicieron crecer 38 casillas y apagaron un incendio». Sin DOM, para probarlo
// con vitest.

import { formatU64 } from "./reports.ts";
import type { OfflineSummary } from "./play.ts";

export interface OfflineSummaryText {
  /** La frase del documento, completa y con punto final. */
  headline: string;
  /** Las cifras que no caben en la frase; las que valen cero no salen. */
  details: { label: string; value: string }[];
}

const count = (n: bigint) => formatU64(n.toString());

function plural(n: bigint, one: string, many: string): string {
  return `${count(n)} ${n === 1n ? one : many}`;
}

/**
 * Qué mostrar al jugador, o `null` si no hay nada que contar (sin resumen o
 * con todo en cero). Los contadores son u64: se formatean como texto, sin
 * pasar por `Number` (regla 7).
 */
export function describeOfflineSummary(summary: OfflineSummary | null): OfflineSummaryText | null {
  if (!summary) return null;
  const { leaves_earned, water_reused, fires_extinguished, tiles_grown } = summary;
  if (leaves_earned === 0n && water_reused === 0n && fires_extinguished === 0n && tiles_grown === 0n) {
    return null;
  }

  const done: string[] = [];
  if (tiles_grown > 0n) {
    done.push(`hicieron crecer ${plural(tiles_grown, "casilla", "casillas")}`);
  }
  if (fires_extinguished > 0n) {
    done.push(
      fires_extinguished === 1n
        ? "apagaron un incendio"
        : `apagaron ${count(fires_extinguished)} incendios`,
    );
  }
  const what = done.length > 0 ? done.join(" y ") : "regaron el valle";

  const details: OfflineSummaryText["details"] = [];
  if (leaves_earned > 0n) details.push({ label: "Hojas ganadas", value: count(leaves_earned) });
  // Misma unidad y mismo nombre que el reporte de las 3R (reports.ts).
  if (water_reused > 0n) {
    details.push({ label: "Agua reutilizada", value: `${count(water_reused)} pts de humedad` });
  }
  return { headline: `Mientras no estabas, tus aspersores ${what}.`, details };
}
