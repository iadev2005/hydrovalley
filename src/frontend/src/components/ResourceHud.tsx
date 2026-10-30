import type { PlayerPanelView } from "../logic/valley.ts";

export interface ResourceHudProps {
  panel: PlayerPanelView;
}

/** Agua y hojas del jugador, siempre a la vista en la barra superior. */
export function ResourceHud({ panel }: ResourceHudProps) {
  return (
    <div className="resource-hud" aria-label="Recursos del jugador">
      <div className="resource-pill resource-pill-water">
        <svg className="resource-icon" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
          <path d="M12 3s6 6.5 6 11a6 6 0 0 1-12 0c0-4.5 6-11 6-11z" />
        </svg>
        <span className="resource-label">Agua</span>
        <strong id="water">{panel.water}</strong>
        <progress id="water-bar" max={1} value={panel.waterRatio} aria-label="Nivel de agua"></progress>
      </div>
      <div className="resource-pill resource-pill-leaves">
        <svg className="resource-icon" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
          <path d="M5 19c0-8 6-14 15-14 0 9-6 15-14 15" />
          <path d="M5 19l7-7" />
        </svg>
        <span className="resource-label">Hojas</span>
        <strong id="leaves">{panel.leaves}</strong>
      </div>
    </div>
  );
}
