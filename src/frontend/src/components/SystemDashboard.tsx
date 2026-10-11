import type { SystemView } from "../logic/system.ts";

export interface SystemDashboardProps {
  metrics: SystemView | null;
  error: string | null;
  lastUpdated: string;
}

export function SystemDashboard({
  metrics,
  error,
  lastUpdated,
}: SystemDashboardProps) {
  const hasHub = Boolean(metrics?.hub);
  const hub = metrics?.hub;

  return (
    <main id="system-page">
      <h1>Panel del sistema</h1>
      <p className="meta">
        Estado en vivo de los servicios. Se actualiza cada 2 s.{" "}
        <a href="/">Volver al juego</a>
      </p>
      <p className="meta" id="system-updated" role="status">
        {lastUpdated}
      </p>
      <p className="error" id="system-error" role="alert" hidden={!error}>
        {error ?? ""}
      </p>

      <section aria-labelledby="services-title">
        <h2 id="services-title">Servicios</h2>
        <ul id="services-list" className="services">
          {metrics?.services.map((service) => (
            <li
              key={service.label}
              className={service.up ? "service up" : "service down"}
            >
              <span className="dot" aria-hidden="true" />
              <span>
                {service.label}: {service.statusText}
              </span>
              {service.detail ? (
                <span className="detail"> ({service.detail})</span>
              ) : null}
            </li>
          ))}
        </ul>
      </section>

      <p className="meta" id="hub-missing" hidden={hasHub}>
        Sin datos del hub.
      </p>

      <section id="hub-section" aria-labelledby="hub-title" hidden={!hasHub}>
        <h2 id="hub-title">Hub</h2>
        <dl className="stats">
          <div>
            <dt>Jugadores conectados</dt>
            <dd data-field="sessions">{hub?.sessions ?? "—"}</dd>
          </div>
          <div>
            <dt>Goroutines</dt>
            <dd data-field="goroutines">{hub?.goroutines ?? "—"}</dd>
          </div>
          <div>
            <dt>Acciones por segundo (admitidas)</dt>
            <dd data-field="actionsAdmitted">{hub?.actionsAdmitted ?? "—"}</dd>
          </div>
          <div>
            <dt>Acciones por segundo (aplicadas)</dt>
            <dd data-field="actionsApplied">{hub?.actionsApplied ?? "—"}</dd>
          </div>
          <div>
            <dt>Latencia p50 (ms)</dt>
            <dd data-field="latencyP50">{hub?.latencyP50 ?? "—"}</dd>
          </div>
          <div>
            <dt>Latencia p95 (ms)</dt>
            <dd data-field="latencyP95">{hub?.latencyP95 ?? "—"}</dd>
          </div>
          <div>
            <dt>Tick</dt>
            <dd data-field="tick">{hub?.tick ?? "—"}</dd>
          </div>
          <div>
            <dt>Duración de Step, última (ms)</dt>
            <dd data-field="stepLast">{hub?.stepLast ?? "—"}</dd>
          </div>
          <div>
            <dt>Duración de Step, p95 (ms)</dt>
            <dd data-field="stepP95">{hub?.stepP95 ?? "—"}</dd>
          </div>
          <div>
            <dt>Fallos de Step</dt>
            <dd data-field="stepFailures">{hub?.stepFailures ?? "—"}</dd>
          </div>
          <div>
            <dt>Cola de comandos</dt>
            <dd data-field="queue">{hub?.queue ?? "—"}</dd>
          </div>
          <div>
            <dt>Valle en pausa</dt>
            <dd data-field="paused">{hub?.paused ?? "—"}</dd>
          </div>
          <div>
            <dt>Circuit Breaker</dt>
            <dd data-field="breaker">{hub?.breaker ?? "—"}</dd>
          </div>
        </dl>
      </section>

      <section
        id="valley-section"
        aria-labelledby="valley-stats-title"
        hidden={!hasHub}
      >
        <h2 id="valley-stats-title">Valle</h2>
        <dl className="stats">
          <div>
            <dt>Humedad total</dt>
            <dd data-field="humidityTotal">{hub?.humidityTotal ?? "—"}</dd>
          </div>
          <div>
            <dt>Humedad aportada</dt>
            <dd data-field="humidityContributed">
              {hub?.humidityContributed ?? "—"}
            </dd>
          </div>
          <div>
            <dt>Gotas aceptadas</dt>
            <dd data-field="acceptedDrops">{hub?.acceptedDrops ?? "—"}</dd>
          </div>
          <div>
            <dt>Incendios activos</dt>
            <dd data-field="activeFires">{hub?.activeFires ?? "—"}</dd>
          </div>
        </dl>
        <p className="meta">
          En modo demo, lo que sube la humedad aportada debe coincidir con lo que
          suben las gotas aceptadas. Las gotas aceptadas se cuentan desde el
          último arranque del hub; la humedad aportada, desde siempre.
        </p>
      </section>
    </main>
  );
}
