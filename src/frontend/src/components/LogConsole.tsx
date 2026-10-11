export interface LogConsoleProps {
  logs: string[];
}

export function LogConsole({ logs }: LogConsoleProps) {
  return (
    <>
      <h2>Registro</h2>
      <ol id="log" reversed>
        {logs.map((line, idx) => (
          <li key={`${idx}-${line.slice(0, 16)}`}>{line}</li>
        ))}
      </ol>
    </>
  );
}
