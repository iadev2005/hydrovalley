import type { ReactNode } from "react";

export interface AccountMenuProps {
  username: string;
  onLogout: () => void;
  /** Lo que no se usa al jugar: administración y registro de eventos. */
  children?: ReactNode;
}

/** Menú desplegable de la cuenta; deja la pantalla de juego despejada. */
export function AccountMenu({ username, onLogout, children }: AccountMenuProps) {
  return (
    <details className="account-menu">
      <summary aria-label={`Menú de la cuenta de ${username}`}>
        <span className="account-avatar" aria-hidden="true">
          {username.slice(0, 2).toUpperCase()}
        </span>
        <span id="session-user" className="account-name">
          {username}
        </span>
      </summary>
      <div className="account-menu-body">
        <p className="meta">
          Conectado como <strong>{username}</strong>
        </p>
        <button id="logout" type="button" onClick={onLogout}>
          Salir
        </button>
        <a href="/sistema.html">Panel del sistema</a>
        {children}
      </div>
    </details>
  );
}
