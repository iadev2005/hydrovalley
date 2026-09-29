// Puerto hacia el servicio de cuentas. Las rutas dependen de esta interfaz,
// no de gRPC: en las pruebas se sustituye por un doble en memoria.

export type AccountRole = "player" | "admin";

export interface Account {
  /** UUID en texto: es el `player_id` del token y del mundo. */
  playerId: string;
  /** Hash Argon2id en formato PHC. Nunca sale de la API. */
  passwordHash: string;
  role: AccountRole;
}

export interface AccountDirectory {
  /** Crea una cuenta de jugador. No es idempotente: no se reintenta. */
  register(
    username: string,
    passwordHash: string,
  ): Promise<{ playerId: string; role: AccountRole }>;
  /** Busca por nombre de usuario; `null` si no existe. */
  getAccount(username: string): Promise<Account | null>;
  /** Traduce `player_id` a nombre de usuario; los desconocidos no aparecen. */
  getUsernames(playerIds: readonly string[]): Promise<ReadonlyMap<string, string>>;
}

/** ALREADY_EXISTS: el nombre de usuario ya está registrado. */
export class UsernameTakenError extends Error {
  constructor() {
    super("username already exists");
    this.name = "UsernameTakenError";
  }
}

/** INVALID_ARGUMENT: la persistencia rechazó los datos de la cuenta. */
export class InvalidAccountError extends Error {
  constructor(message = "account data rejected by persistence") {
    super(message);
    this.name = "InvalidAccountError";
  }
}

/** UNAVAILABLE o plazo agotado: la persistencia no respondió a tiempo. */
export class DirectoryUnavailableError extends Error {
  constructor(message = "account directory unavailable") {
    super(message);
    this.name = "DirectoryUnavailableError";
  }
}
