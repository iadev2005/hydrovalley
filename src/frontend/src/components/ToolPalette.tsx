import { useState } from "react";
import type { PlayerPanelOwned, PlayerPanelPrices } from "../logic/valley.ts";
import { TOOLS, UPGRADES, isInstallationTool, type ToolId } from "../logic/tools.ts";

/** Icono de cada instalación: el mismo sprite que dibuja el valle. */
const TOOL_SPRITE: Record<string, string> = {
  sprinkler: "/assets/installations/sprinkler.png",
  drip: "/assets/installations/drip.png",
  treatment_plant: "/assets/installations/treatment_plant.png",
  trash_bin: "/assets/installations/trash_bin.png",
};

export interface ToolPaletteProps {
  tool: ToolId;
  prices: PlayerPanelPrices;
  /** Hojas del jugador, o `null` antes de su primer comando. */
  leaves: bigint | null;
  owned: PlayerPanelOwned;
  isOpen: boolean;
  onSelectTool: (tool: ToolId) => void;
  onBuyUpgrade: (upgrade: string) => void;
}

function canAfford(price: string, leaves: bigint | null): boolean {
  if (leaves === null || !/^\d+$/.test(price)) return true;
  return leaves >= BigInt(price);
}

export function ToolPalette({
  tool,
  prices,
  leaves,
  owned,
  isOpen,
  onSelectTool,
  onBuyUpgrade,
}: ToolPaletteProps) {
  const [hovered, setHovered] = useState<ToolId | null>(null);
  const infoTool = TOOLS.find((t) => t.id === (hovered ?? tool)) ?? TOOLS[0];

  return (
    <aside className="tool-palette" aria-label="Herramientas">
      <h2 className="palette-heading">Herramientas</h2>
      <div className="tool-list">
        {TOOLS.map((t) => {
          const price = isInstallationTool(t.id) ? prices[t.id] : null;
          const classes = ["tool"];
          if (tool === t.id) classes.push("tool-selected");
          if (price !== null && !canAfford(price, leaves)) classes.push("tool-poor");
          return (
            <button
              key={t.id}
              id={`tool-${t.id}`}
              type="button"
              className={classes.join(" ")}
              aria-pressed={tool === t.id}
              onClick={() => onSelectTool(t.id)}
              onMouseEnter={() => setHovered(t.id)}
              onMouseLeave={() => setHovered(null)}
              onFocus={() => setHovered(t.id)}
              onBlur={() => setHovered(null)}
            >
              <span className="tool-icon" aria-hidden="true">
                {TOOL_SPRITE[t.id] ? (
                  <img
                    src={TOOL_SPRITE[t.id]}
                    alt=""
                    className={t.id === "drip" ? "tool-sprite tool-sprite-flat" : "tool-sprite"}
                  />
                ) : (
                  <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M12 3s6 6.5 6 11a6 6 0 0 1-12 0c0-4.5 6-11 6-11z" />
                  </svg>
                )}
              </span>
              <span className="tool-text">
                <strong>{t.name}</strong>
                <span>{t.short}</span>
              </span>
              {price !== null ? (
                <span className="tool-price" title="Hojas">
                  <span id={`price-${t.id}`}>{price}</span>
                  <span className="visually-hidden"> hojas</span>
                </span>
              ) : null}
            </button>
          );
        })}
      </div>

      <div className="tool-info" aria-live="polite">
        <span className="tool-info-kicker">{hovered ? "Para qué sirve" : "Herramienta activa"}</span>
        <strong>{infoTool?.name}</strong>
        <p>{infoTool?.description}</p>
        {infoTool && isInstallationTool(infoTool.id) ? (
          <p className="tool-info-note">
            Cuesta {prices[infoTool.id]} hojas; el precio sube con cada compra.
          </p>
        ) : null}
      </div>

      <h2 className="palette-heading">Mejoras</h2>
      <div className="upgrade-list">
        {UPGRADES.map((u) => (
          <div key={u.id} className="upgrade">
            <span className="tool-text">
              <strong>{u.name}</strong>
              <span>{u.description}</span>
            </span>
            <button
              id={`buy-${u.id}`}
              type="button"
              className="btn-buy"
              disabled={!isOpen || owned[u.id]}
              onClick={() => onBuyUpgrade(u.id)}
            >
              {owned[u.id] ? "Comprada" : "Comprar"}
            </button>
          </div>
        ))}
      </div>
    </aside>
  );
}
