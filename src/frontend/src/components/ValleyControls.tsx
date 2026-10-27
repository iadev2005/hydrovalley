import type { ReactNode } from "react";
import type { ActionMessage } from "../hooks/useGameSocket.ts";
import type { StateFrame } from "../logic/play.ts";
import { playerPanel, selectionView } from "../logic/valley.ts";

/** Durabilidad base de las instalaciones (catalog.json); solo escala la barra. */
const DURABILITY_BAR_MAX = 30;

export interface ValleyControlsProps {
  gameState: StateFrame | null;
  selected: { x: number; y: number } | null;
  isOpen: boolean;
  pendingCount: number;
  actionMessage: ActionMessage | null;
  onDrop: () => void;
  onPlaceInstallation: (installation: string) => void;
  onCleanTrash?: () => void;
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
  onCleanTrash,
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

  const selectedInstallation =
    selected && gameState
      ? gameState.installations.find(
          (i) => i.x === selected.x && i.y === selected.y,
        )
      : null;

  const selectedTrash =
    selected && gameState && gameState.width > 0
      ? (gameState.trash[selected.y * gameState.width + selected.x] ?? 0)
      : 0;
  const selectedHasTrash = selectedTrash > 0;

  return (
    <div className="panel">
      <div className="resource-hud" aria-label="Recursos del jugador">
        <div className="resource-pill resource-pill-water">
          <span className="resource-icon" aria-hidden="true">💧</span>
          <div className="resource-details">
            <span className="resource-label">Agua</span>
            <strong id="water">{panel.water}</strong>
          </div>
          <progress id="water-bar" max={1} value={panel.waterRatio} aria-label="Nivel de agua"></progress>
        </div>
        <div className="resource-pill resource-pill-leaves">
          <span className="resource-icon" aria-hidden="true">🍃</span>
          <div className="resource-details">
            <span className="resource-label">Hojas</span>
            <strong id="leaves">{panel.leaves}</strong>
          </div>
        </div>
      </div>

      <div className="panel-bottom-dock">
        <div className="panel-status-row">
          <div className="tile-summary" aria-label="Información de la casilla seleccionada">
            <div className="tile-summary-heading">
              <span className="tile-summary-label">Casilla seleccionada</span>
              <strong id="selected">{view.label}</strong>
            </div>
            <p className="tile-summary-info" id="tile-info">
              {view.info}
            </p>
            {selectedInstallation && (
              <p className="tile-summary-info" id="installation-durability">
                Durabilidad ({selectedInstallation.installation}):{" "}
                <strong id="durability-value">{selectedInstallation.remaining_durability}</strong>{" "}
                usos restantes{" "}
                <progress
                  id="durability-bar"
                  max={Math.max(DURABILITY_BAR_MAX, selectedInstallation.remaining_durability)}
                  value={selectedInstallation.remaining_durability}
                />
              </p>
            )}
          </div>
          <p className={messageClass} id="action-message" role="status">
            {actionMessage?.text ?? ""}
          </p>
          <p className="meta pending-status">
            Acciones pendientes: <span id="pending">{pendingCount}</span>
          </p>
        </div>

        <div className="actions">
          <div className="action-group action-group-primary">
            <span className="action-group-label">Acción principal</span>
            <button
              id="drop"
              type="button"
              disabled={!canActOnTile}
              onClick={onDrop}
            >
              Soltar gota
            </button>
            {selectedHasTrash && onCleanTrash && (
              <button
                id="clean-trash"
                type="button"
                disabled={!canActOnTile}
                onClick={onCleanTrash}
              >
                Limpiar basura
              </button>
            )}
          </div>
          <div className="action-group action-group-installations">
            <span className="action-group-label">Comprar instalaciones 3R</span>
            <div className="action-group-buttons">
              <button
                id="place-sprinkler"
                type="button"
                disabled={!canActOnTile}
                onClick={() => onPlaceInstallation("sprinkler")}
              >
                Aspersor (
                <span id="price-sprinkler">{panel.prices.sprinkler}</span> hojas)
              </button>
              <button
                id="place-drip"
                type="button"
                disabled={!canActOnTile}
                onClick={() => onPlaceInstallation("drip")}
              >
                Riego por goteo (
                <span id="price-drip">{panel.prices.drip}</span> hojas)
              </button>
              <button
                id="place-treatment_plant"
                type="button"
                disabled={!canActOnTile}
                onClick={() => onPlaceInstallation("treatment_plant")}
              >
                Planta de tratamiento (
                <span id="price-treatment_plant">{panel.prices.treatment_plant}</span>{" "}
                hojas)
              </button>
              <button
                id="place-trash_bin"
                type="button"
                disabled={!canActOnTile}
                onClick={() => onPlaceInstallation("trash_bin")}
              >
                Cesto de reciclaje (
                <span id="price-trash_bin">{panel.prices.trash_bin}</span> hojas)
              </button>
            </div>
          </div>
          <div className="action-group action-group-upgrades">
            <span className="action-group-label">Comprar mejoras</span>
            <div className="action-group-buttons">
              <button
                id="buy-bigger_drop"
                type="button"
                disabled={!isOpen || panel.owned.bigger_drop}
                onClick={() => onBuyUpgrade("bigger_drop")}
              >
                Gota grande
              </button>
              <button
                id="buy-bigger_tank"
                type="button"
                disabled={!isOpen || panel.owned.bigger_tank}
                onClick={() => onBuyUpgrade("bigger_tank")}
              >
                Depósito mayor
              </button>
            </div>
          </div>
        </div>
        {children}
      </div>
    </div>
  );
}
