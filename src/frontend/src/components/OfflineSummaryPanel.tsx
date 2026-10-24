import { describeOfflineSummary } from "../logic/offlineSummary.ts";
import type { OfflineSummary } from "../logic/play.ts";

export interface OfflineSummaryPanelProps {
  summary: OfflineSummary | null;
  onDismiss: () => void;
}

/**
 * Resumen al volver (RF-25): lo que hicieron los aspersores del jugador
 * mientras no estaba. Se puede cerrar; sin nada que contar no se dibuja.
 */
export function OfflineSummaryPanel({ summary, onDismiss }: OfflineSummaryPanelProps) {
  const text = describeOfflineSummary(summary);
  if (!text) return null;
  return (
    <section id="offline-summary" className="offline-summary" role="status" aria-labelledby="offline-summary-title">
      <h2 id="offline-summary-title">Resumen al volver</h2>
      <p>{text.headline}</p>
      {text.details.length > 0 ? (
        <dl>
          {text.details.map((row) => (
            <div key={row.label}>
              <dt>{row.label}</dt>
              <dd>{row.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      <button type="button" id="btn-dismiss-offline-summary" onClick={onDismiss}>
        Cerrar
      </button>
    </section>
  );
}
