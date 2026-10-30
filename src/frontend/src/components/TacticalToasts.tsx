import type { TacticalAlert } from "../hooks/useTacticalAlerts.ts";

export interface TacticalToastsProps {
  alerts: TacticalAlert[];
  onDismiss: (id: string) => void;
}

export function TacticalToasts({ alerts, onDismiss }: TacticalToastsProps) {
  if (alerts.length === 0) return null;

  return (
    <aside
      className="tactical-toasts-container"
      aria-label="Avisos tácticos del valle"
      role="region"
    >
      {alerts.map((alert) => (
        <div
          key={alert.id}
          className={`tactical-toast tactical-toast-${alert.variant}`}
          role="status"
          aria-live="polite"
        >
          <span className="toast-icon" aria-hidden="true">
            {alert.icon}
          </span>
          <div className="toast-body">
            <strong className="toast-title">{alert.title}</strong>
            <p className="toast-message">{alert.message}</p>
          </div>
          {alert.actionLabel && alert.onAction ? (
            <button
              type="button"
              className="toast-action-btn"
              onClick={alert.onAction}
            >
              {alert.actionLabel}
            </button>
          ) : null}
          <button
            type="button"
            className="toast-dismiss-btn"
            onClick={() => onDismiss(alert.id)}
            title="Cerrar aviso"
            aria-label="Cerrar aviso"
          >
            ✕
          </button>
        </div>
      ))}
    </aside>
  );
}
