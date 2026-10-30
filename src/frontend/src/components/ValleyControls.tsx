import { memo, type ReactNode } from "react";
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
  onFocusFire?: () => void;
  children?: ReactNode;
}

export const ValleyControls = memo(function ValleyControls({
  gameState,
  selected,
  isOpen,
  pendingCount,
  actionMessage,
  onDrop,
  onPlaceInstallation,
  onBuyUpgrade,
  onFocusFire,
  children,
}: ValleyControlsProps) {
  const panel = playerPanel(gameState?.player ?? null);
  const view = selectionView(gameState, selected);
  const canActOnTile = isOpen && selected !== null;
  const activeFiresCount = gameState?.fires?.length ?? 0;

  // Comprobación de asequibilidad de recursos para feedback de videojuego RTS
  const player = gameState?.player;
  const currentLeaves = player?.leaves ?? 0n;
  const hasWater = (player?.water ?? 0n) > 0n;

  const canAffordSprinkler = player ? currentLeaves >= (player.next_sprinkler_price ?? 0n) : false;
  const canAffordDrip = player ? currentLeaves >= (player.next_drip_price ?? 0n) : false;
  const canAffordTreatmentPlant = player
    ? currentLeaves >= (player.next_treatment_plant_price ?? 0n)
    : false;

  let messageClass = "meta tactical-message";
  if (actionMessage) {
    messageClass = actionMessage.ok ? "meta tactical-message ok" : "meta tactical-message error";
  }

  return (
    <div className="panel command-deck-panel">
      {/* HUD de recursos con barras tácticas */}
      <div className="resource-hud" aria-label="Recursos del jugador">
        <div className="resource-pill resource-pill-water">
          <span className="resource-icon" aria-hidden="true">💧</span>
          <div className="resource-details">
            <span className="resource-label">Agua</span>
            <strong id="water">{panel.water}</strong>
          </div>
          <progress
            id="water-bar"
            max={1}
            value={panel.waterRatio}
            aria-label="Nivel de agua"
            title={`Depósito: ${Math.round(panel.waterRatio * 100)}%`}
          />
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
        {/* Barra de estado e Inspector de Casilla */}
        <div className="panel-status-row">
          <div className="tile-summary tactical-inspector-card" aria-label="Información de la casilla seleccionada">
            <div className="tile-summary-heading">
              <span className="tile-summary-label">
                <span className="inspector-radar-dot" aria-hidden="true"></span>
                Casilla seleccionada
              </span>
              <strong id="selected">{view.label}</strong>
            </div>
            <p className="tile-summary-info" id="tile-info">
              {view.info}
            </p>
          </div>

          {activeFiresCount > 0 ? (
            <button
              id="btn-focus-fire"
              type="button"
              className="btn-focus-fire"
              onClick={onFocusFire}
              title="Centrar cámara en el incendio más cercano (Tecla F)"
            >
              🔥 Incendios activos: {activeFiresCount}
              <kbd className="hotkey-hint" aria-hidden="true">[F]</kbd>
            </button>
          ) : null}

          {actionMessage ? (
            <p className={messageClass} id="action-message" role="status">
              <span className="message-icon" aria-hidden="true">
                {actionMessage.ok ? "✓" : "⚠️"}
              </span>
              <span>{actionMessage.text}</span>
            </p>
          ) : (
            <p className={messageClass} id="action-message" role="status" style={{ display: "none" }} />
          )}

          <p className="meta pending-status" title="Acciones en tránsito hacia el servidor">
            <span className="pulse-network-icon" aria-hidden="true">●</span>
            Acciones pendientes: <span id="pending">{pendingCount}</span>
          </p>
        </div>

        {/* Mazo de Acciones y Comandos RTS */}
        <div className="actions tactical-command-grid">
          {/* Acción principal: Soltar Gota */}
          <div className="action-group action-group-primary">
            <span className="action-group-label">Acción táctica principal</span>
            <button
              id="drop"
              type="button"
              className={`btn-game-command btn-command-water ${!hasWater && canActOnTile ? "command-low-resource" : ""}`}
              aria-label="Soltar gota"
              disabled={!canActOnTile}
              onClick={onDrop}
              title={
                !canActOnTile
                  ? "Selecciona una casilla para soltar agua"
                  : !hasWater
                    ? "Depósito vacío (espera recarga o adquiere mejoras)"
                    : "Lanza una gota de agua sobre la casilla [Espacio]"
              }
            >
              <span className="btn-icon" aria-hidden="true">💧</span>
              <span className="btn-title">Soltar gota</span>
              <kbd className="hotkey-hint" aria-hidden="true">[Espacio]</kbd>
            </button>
          </div>

          {/* Construcción e Instalaciones 3R */}
          <div className="action-group action-group-installations">
            <span className="action-group-label">Instalaciones 3R del Valle</span>
            <div className="action-group-buttons">
              <button
                id="place-sprinkler"
                type="button"
                className={`btn-game-command btn-command-3r ${!canAffordSprinkler ? "command-insufficient" : ""}`}
                disabled={!canActOnTile}
                onClick={() => onPlaceInstallation("sprinkler")}
                title={
                  !canAffordSprinkler
                    ? `Requiere ${panel.prices.sprinkler} hojas (insuficientes)`
                    : "Instalar aspersor automatizado [1]"
                }
              >
                <span className="btn-icon" aria-hidden="true">💦</span>
                <span className="btn-title">
                  Aspersor (<span id="price-sprinkler" className={canAffordSprinkler ? "price-affordable" : "price-shortage"}>{panel.prices.sprinkler}</span> hojas)
                </span>
                <kbd className="hotkey-hint" aria-hidden="true">[1]</kbd>
              </button>

              <button
                id="place-drip"
                type="button"
                className={`btn-game-command btn-command-3r ${!canAffordDrip ? "command-insufficient" : ""}`}
                disabled={!canActOnTile}
                onClick={() => onPlaceInstallation("drip")}
                title={
                  !canAffordDrip
                    ? `Requiere ${panel.prices.drip} hojas (insuficientes)`
                    : "Instalar sistema de riego por goteo [2]"
                }
              >
                <span className="btn-icon" aria-hidden="true">🌱</span>
                <span className="btn-title">
                  Riego por goteo (<span id="price-drip" className={canAffordDrip ? "price-affordable" : "price-shortage"}>{panel.prices.drip}</span> hojas)
                </span>
                <kbd className="hotkey-hint" aria-hidden="true">[2]</kbd>
              </button>

              <button
                id="place-treatment_plant"
                type="button"
                className={`btn-game-command btn-command-3r ${!canAffordTreatmentPlant ? "command-insufficient" : ""}`}
                disabled={!canActOnTile}
                onClick={() => onPlaceInstallation("treatment_plant")}
                title={
                  !canAffordTreatmentPlant
                    ? `Requiere ${panel.prices.treatment_plant} hojas (insuficientes)`
                    : "Instalar planta de tratamiento de aguas residuales [3]"
                }
              >
                <span className="btn-icon" aria-hidden="true">🏭</span>
                <span className="btn-title">
                  Planta de tratamiento (
                  <span id="price-treatment_plant" className={canAffordTreatmentPlant ? "price-affordable" : "price-shortage"}>{panel.prices.treatment_plant}</span>{" "}
                  hojas)
                </span>
                <kbd className="hotkey-hint" aria-hidden="true">[3]</kbd>
              </button>
            </div>
          </div>

          {/* Investigación y Mejoras del Jugador */}
          <div className="action-group action-group-upgrades">
            <span className="action-group-label">Mejoras de Infraestructura</span>
            <div className="action-group-buttons action-group-upgrades-grid">
              <button
                id="buy-bigger_drop"
                type="button"
                className={`btn-game-command btn-command-upgrade ${panel.owned.bigger_drop ? "command-owned" : ""}`}
                disabled={!isOpen || panel.owned.bigger_drop}
                onClick={() => onBuyUpgrade("bigger_drop")}
                title={panel.owned.bigger_drop ? "Mejora ya adquirida" : "Aumenta el radio y efectividad de cada gota"}
              >
                <span className="btn-icon" aria-hidden="true">{panel.owned.bigger_drop ? "✓" : "🌊"}</span>
                <span className="btn-title">Gota grande</span>
                {panel.owned.bigger_drop ? <span className="badge-owned">Comprada</span> : null}
              </button>

              <button
                id="buy-bigger_tank"
                type="button"
                className={`btn-game-command btn-command-upgrade ${panel.owned.bigger_tank ? "command-owned" : ""}`}
                disabled={!isOpen || panel.owned.bigger_tank}
                onClick={() => onBuyUpgrade("bigger_tank")}
                title={panel.owned.bigger_tank ? "Mejora ya adquirida" : "Amplía la capacidad máxima de tu depósito de agua"}
              >
                <span className="btn-icon" aria-hidden="true">{panel.owned.bigger_tank ? "✓" : "🛢️"}</span>
                <span className="btn-title">Depósito mayor</span>
                {panel.owned.bigger_tank ? <span className="badge-owned">Comprada</span> : null}
              </button>
            </div>
          </div>
        </div>

        {children}
      </div>
    </div>
  );
});
