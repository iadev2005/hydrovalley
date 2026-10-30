export interface LogConsoleProps {
  logs: string[];
  hidden?: boolean;
}

export function LogConsole({ logs, hidden = false }: LogConsoleProps) {
  return (
    <section
      className="section-shell log-section"
      aria-labelledby="log-title"
      hidden={hidden}
    >
      <h2 id="log-title">Registro</h2>
      <ol id="log" reversed>
        {logs.map((line, idx) => (
          <li key={`${idx}-${line.slice(0, 16)}`}>{line}</li>
        ))}
      </ol>
    </section>
  );
}
