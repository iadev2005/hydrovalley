export interface LogConsoleProps {
  logs: string[];
}

export function LogConsole({ logs }: LogConsoleProps) {
  return (
    <section className="section-shell log-section" aria-labelledby="log-title">
      <h2 id="log-title">Registro</h2>
      <ol id="log" reversed>
        {logs.map((line, idx) => (
          <li key={`${idx}-${line.slice(0, 16)}`}>{line}</li>
        ))}
      </ol>
    </section>
  );
}
