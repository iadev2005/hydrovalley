import { describeOfflineSummary } from "../logic/offlineSummary.ts";
import type { OfflineSummary } from "../logic/play.ts";

export interface OfflineSummaryPanelProps {
  summary: OfflineSummary | null;
  offlineDurationMs?: number | null;
  onDismiss: () => void;
}

function formatDuration(durationMs: number): string {
  const totalSeconds = Math.floor(durationMs / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) return `${hours} h ${minutes} min ${seconds} s`;
  if (minutes > 0) return `${minutes} min ${seconds} s`;
  return `${seconds} s`;
}

/**
 * Resumen al volver (RF-25): lo que hicieron los aspersores del jugador
 * mientras no estaba. Se puede cerrar; sin nada que contar no se dibuja.
 */
export function OfflineSummaryPanel({
  summary,
  offlineDurationMs = null,
  onDismiss,
}: OfflineSummaryPanelProps) {
  const text = describeOfflineSummary(summary);
  if (!text) return null;
  return (
    <div className="offline-summary-backdrop">
      <section
        id="offline-summary"
        className="offline-summary"
        role="dialog"
        aria-modal="true"
        aria-labelledby="offline-summary-title"
        aria-describedby="offline-summary-description"
      >
        <div role="status" aria-live="polite">
          <h2 id="offline-summary-title">Resumen al volver</h2>
          <p id="offline-summary-description" className="offline-summary-period">
            Resultados obtenidos durante la desconexión
          </p>
          {offlineDurationMs !== null && offlineDurationMs >= 0 ? (
            <p className="offline-summary-duration">
              Tiempo desconectado: <strong>{formatDuration(offlineDurationMs)}</strong>
            </p>
          ) : null}
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
        </div>
        <button type="button" id="btn-dismiss-offline-summary" onClick={onDismiss}>
          Cerrar
        </button>
      </section>
    </div>
  );
}
