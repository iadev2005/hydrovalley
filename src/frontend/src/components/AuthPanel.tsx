import { useState, type FormEvent } from "react";
import type { Credentials } from "../logic/auth.ts";

export interface AuthPanelProps {
  hidden?: boolean;
  onLogin: (credentials: Credentials) => Promise<boolean>;
  onRegister: (credentials: Credentials) => Promise<boolean>;
  isBusy: boolean;
  isRetryLocked: boolean;
  error: string;
  onClearError: () => void;
}

export function AuthPanel({
  hidden = false,
  onLogin,
  onRegister,
  isBusy,
  isRetryLocked,
  error,
  onClearError,
}: AuthPanelProps) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [localError, setLocalError] = useState("");

  const disabled = isBusy || isRetryLocked;
  const displayedError = localError || error;

  const validateAndSubmit = async (
    action: (credentials: Credentials) => Promise<boolean>,
  ) => {
    if (disabled) return;
    const trimmedUser = username.trim();
    if (trimmedUser === "" || password === "") {
      setLocalError("escribe el usuario y la contraseña");
      return;
    }
    setLocalError("");
    onClearError();
    const success = await action({ username: trimmedUser, password });
    if (success) {
      setPassword("");
    }
  };

  const handleLoginSubmit = (event: FormEvent) => {
    event.preventDefault();
    void validateAndSubmit(onLogin);
  };

  const handleRegisterClick = () => {
    void validateAndSubmit(onRegister);
  };

  return (
    <section
      id="access-panel"
      className="section-shell"
      aria-labelledby="access-title"
      hidden={hidden}
    >
      <h2 id="access-title">Acceso</h2>
      <p id="access-help">
        Inicia sesión para participar en el valle y administrar tus recursos.
      </p>
      <form
        id="auth-form"
        aria-describedby="access-help"
        autoComplete="on"
        onSubmit={handleLoginSubmit}
      >
        <label>
          Usuario{" "}
          <input
            id="username"
            name="username"
            type="text"
            autoComplete="username"
            required
            value={username}
            onChange={(e) => {
              setUsername(e.target.value);
              if (localError) setLocalError("");
            }}
          />
        </label>
        <label>
          Contraseña{" "}
          <input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => {
              setPassword(e.target.value);
              if (localError) setLocalError("");
            }}
          />
        </label>
        <button
          id="register"
          type="button"
          disabled={disabled}
          onClick={handleRegisterClick}
        >
          Registrarse
        </button>
        <button id="login" type="submit" disabled={disabled}>
          Entrar
        </button>
      </form>
      <p
        id="auth-error"
        className="error"
        role="alert"
        hidden={!displayedError}
      >
        {displayedError}
      </p>
    </section>
  );
}
