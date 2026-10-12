export interface SessionBarProps {
  hidden?: boolean;
  username: string;
  onLogout: () => void;
}

export function SessionBar({
  hidden = false,
  username,
  onLogout,
}: SessionBarProps) {
  return (
    <section id="session-panel" hidden={hidden}>
      <p className="meta">
        Conectado como <strong id="session-user">{username}</strong>{" "}
        <button id="logout" type="button" onClick={onLogout}>
          Salir
        </button>
      </p>
    </section>
  );
}
