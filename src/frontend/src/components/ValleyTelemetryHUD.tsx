import type { StateFrame } from "../logic/play.ts";

export interface ValleyTelemetryHUDProps {
  gameState: StateFrame | null;
  onFocusFire?: () => void;
}

export function ValleyTelemetryHUD({ gameState, onFocusFire }: ValleyTelemetryHUDProps) {
  if (!gameState) {
    return (
      <div className="valley-telemetry-hud hud-placeholder" aria-label="Telemetría táctica">
        <span className="hud-placeholder-text">📡 Esperando telemetría del valle...</span>
      </div>
    );
  }

  const firesCount = gameState.fires.length;
  const installationsCount = gameState.installations.length;

  // Cálculo de cobertura vegetal viva (etapas 1 a 4)
  const totalTiles = gameState.stages.length > 0 ? gameState.stages.length : 1024;
  const greenTiles = gameState.stages.filter((stage) => stage >= 1).length;
  const greenPercent = Math.min(100, Math.round((greenTiles / totalTiles) * 100));

  // Datos de recursos del jugador
  const water = gameState.player?.water ?? 0n;
  const tankCapacity = gameState.player?.tank_capacity ?? 1n;
  const waterPercent = Math.min(
    100,
    Number((water * 100n) / (tankCapacity > 0n ? tankCapacity : 1n)),
  );
  const leaves = gameState.player?.leaves ?? 0n;

  return (
    <div
      className="valley-telemetry-hud"
      aria-label="Telemetría táctica del valle"
      role="region"
    >
      {/* Indicador de fuego */}
      {firesCount > 0 ? (
        <button
          type="button"
          className="hud-metric hud-metric-danger hud-metric-clickable"
          onClick={onFocusFire}
          title="Haz clic para enfocar incendios activos en la cámara (tecla F)"
        >
          <span className="hud-icon" aria-hidden="true">🔥</span>
          <div className="hud-metric-info">
            <span className="hud-value">{firesCount}</span>
            <span className="hud-label">Fuegos activos <kbd className="hud-kbd">F</kbd></span>
          </div>
        </button>
      ) : (
        <div className="hud-metric hud-metric-safe" title="El valle está libre de incendios">
          <span className="hud-icon" aria-hidden="true">🛡️</span>
          <div className="hud-metric-info">
            <span className="hud-value">0</span>
            <span className="hud-label">Bajo control</span>
          </div>
        </div>
      )}

      {/* Indicador de agua */}
      <div className="hud-metric hud-metric-water" title="Reserva de agua en el depósito">
        <span className="hud-icon" aria-hidden="true">💧</span>
        <div className="hud-metric-info">
          <span className="hud-value">
            {String(water)} / {String(tankCapacity)}
          </span>
          <div className="hud-mini-progress" aria-hidden="true">
            <div
              className="hud-mini-progress-fill hud-water-fill"
              style={{ width: `${waterPercent}%` }}
            />
          </div>
        </div>
      </div>

      {/* Cobertura vegetal */}
      <div
        className="hud-metric hud-metric-nature"
        title="Porcentaje de superficie con vegetación activa"
      >
        <span className="hud-icon" aria-hidden="true">🌿</span>
        <div className="hud-metric-info">
          <span className="hud-value">{greenPercent}%</span>
          <div className="hud-mini-progress" aria-hidden="true">
            <div
              className="hud-mini-progress-fill hud-nature-fill"
              style={{ width: `${greenPercent}%` }}
            />
          </div>
        </div>
      </div>

      {/* Hojas ecológicas */}
      <div className="hud-metric hud-metric-leaves" title="Hojas acumuladas por regeneración ecológica">
        <span className="hud-icon" aria-hidden="true">🍃</span>
        <div className="hud-metric-info">
          <span className="hud-value">{String(leaves)}</span>
          <span className="hud-label">Hojas</span>
        </div>
      </div>

      {/* Tecnologías 3R desplegadas */}
      <div
        className="hud-metric hud-metric-tech"
        title="Instalaciones 3R activas en la cuadrícula del valle"
      >
        <span className="hud-icon" aria-hidden="true">🏗️</span>
        <div className="hud-metric-info">
          <span className="hud-value">{installationsCount}</span>
          <span className="hud-label">3R Desplegadas</span>
        </div>
      </div>

      {/* Tick de simulación del mundo */}
      <div className="hud-metric hud-metric-tick" title="Ciclo de simulación del valle">
        <span className="hud-icon" aria-hidden="true">⏱️</span>
        <div className="hud-metric-info">
          <span className="hud-value">t-{String(gameState.tick)}</span>
        </div>
      </div>

      {/* Modo Pausa */}
      {gameState.paused ? (
        <div className="hud-metric hud-metric-paused" role="status" title="La partida se encuentra pausada">
          <span className="hud-icon" aria-hidden="true">⏸️</span>
          <span className="hud-value">Pausa</span>
        </div>
      ) : null}

      {/* Modo Demo */}
      {gameState.demo_mode ? (
        <div className="hud-metric hud-metric-demo" role="status" title="Modo demostración automatizado activo">
          <span className="hud-icon" aria-hidden="true">🎮</span>
          <span className="hud-value">Demo</span>
        </div>
      ) : null}
    </div>
  );
}
