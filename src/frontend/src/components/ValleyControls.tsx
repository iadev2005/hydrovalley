import type { ReactNode } from "react";
import type { ActionMessage } from "../hooks/useGameSocket.ts";
import type { StateFrame } from "../logic/play.ts";
import { playerPanel, selectionView } from "../logic/valley.ts";

export interface ValleyControlsProps {
  gameState: StateFrame | null;
  selected: { x: number; y: number } | null;
  isOpen: boolean;
  pendingCount: number;
  actionMessage: ActionMessage | null;
  onDrop: () => void;
  onPlaceInstallation: (installation: string) => void;
  onBuyUpgrade: (upgrade: string) => void;
  children?: ReactNode;
}

export function ValleyControls({
  gameState,
  selected,
  isOpen,
  pendingCount,
  actionMessage,
  onDrop,
  onPlaceInstallation,
  onBuyUpgrade,
  children,
}: ValleyControlsProps) {
  const panel = playerPanel(gameState?.player ?? null);
  const view = selectionView(gameState, selected);
  const canActOnTile = isOpen && selected !== null;

  let messageClass = "meta";
  if (actionMessage) {
    messageClass = actionMessage.ok ? "meta ok" : "meta error";
  }

  return (
    <div className="panel">
      <p className="meta">
        Casilla: <strong id="selected">{view.label}</strong>
      </p>
      <p className="meta" id="tile-info">
        {view.info}
      </p>
      <p className="meta">
        Agua: <strong id="water">{panel.water}</strong>{" "}
        <progress id="water-bar" max={1} value={panel.waterRatio}></progress>
      </p>
      <p className="meta">
        Hojas: <strong id="leaves">{panel.leaves}</strong>
      </p>
      <div className="actions">
        <button
          id="drop"
          type="button"
          disabled={!canActOnTile}
          onClick={onDrop}
        >
          Soltar gota
        </button>
        <button
          id="place-sprinkler"
          type="button"
          disabled={!canActOnTile}
          onClick={() => onPlaceInstallation("sprinkler")}
        >
          Colocar aspersor (
          <span id="price-sprinkler">{panel.prices.sprinkler}</span> hojas)
        </button>
        <button
          id="place-drip"
          type="button"
          disabled={!canActOnTile}
          onClick={() => onPlaceInstallation("drip")}
        >
          Colocar riego por goteo (
          <span id="price-drip">{panel.prices.drip}</span> hojas)
        </button>
        <button
          id="place-treatment_plant"
          type="button"
          disabled={!canActOnTile}
          onClick={() => onPlaceInstallation("treatment_plant")}
        >
          Colocar planta de tratamiento (
          <span id="price-treatment_plant">{panel.prices.treatment_plant}</span>{" "}
          hojas)
        </button>
        <button
          id="buy-bigger_drop"
          type="button"
          disabled={!isOpen || panel.owned.bigger_drop}
          onClick={() => onBuyUpgrade("bigger_drop")}
        >
          Comprar gota grande
        </button>
        <button
          id="buy-bigger_tank"
          type="button"
          disabled={!isOpen || panel.owned.bigger_tank}
          onClick={() => onBuyUpgrade("bigger_tank")}
        >
          Comprar depósito mayor
        </button>
      </div>
      <p className={messageClass} id="action-message" role="status">
        {actionMessage?.text ?? ""}
      </p>
      <p className="meta">
        Acciones pendientes: <span id="pending">{pendingCount}</span>
      </p>
      {children}
    </div>
  );
}
