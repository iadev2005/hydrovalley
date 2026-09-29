import {
  type Account,
  type AccountDirectory,
  DirectoryUnavailableError,
  InvalidAccountError,
  UsernameTakenError,
} from "../../src/account-directory.js";

interface Stored extends Account {
  username: string;
}

/** Directorio en memoria con fallos configurables y contadores de llamadas. */
export class FakeAccountDirectory implements AccountDirectory {
  readonly accounts = new Map<string, Stored>();
  readonly calls = { register: 0, getAccount: 0, getUsernames: 0 };
  registerError: Error | undefined;
  getAccountError: Error | undefined;
  getUsernamesError: Error | undefined;
  lastRegisteredHash: string | undefined;
  private nextId = 1;

  seed(username: string, passwordHash: string, role: Account["role"] = "player"): Account {
    const account: Stored = {
      playerId: `00000000-0000-4000-8000-${String(this.nextId++).padStart(12, "0")}`,
      username,
      passwordHash,
      role,
    };
    this.accounts.set(username.toLowerCase(), account);
    return account;
  }

  register(username: string, passwordHash: string) {
    this.calls.register++;
    this.lastRegisteredHash = passwordHash;
    if (this.registerError) return Promise.reject(this.registerError);
    if (this.accounts.has(username.toLowerCase())) {
      return Promise.reject(new UsernameTakenError());
    }
    const account = this.seed(username, passwordHash);
    return Promise.resolve({ playerId: account.playerId, role: account.role });
  }

  getAccount(username: string) {
    this.calls.getAccount++;
    if (this.getAccountError) return Promise.reject(this.getAccountError);
    return Promise.resolve(this.accounts.get(username.toLowerCase()) ?? null);
  }

  getUsernames(playerIds: readonly string[]) {
    this.calls.getUsernames++;
    if (this.getUsernamesError) return Promise.reject(this.getUsernamesError);
    const result = new Map<string, string>();
    for (const account of this.accounts.values()) {
      if (playerIds.includes(account.playerId)) result.set(account.playerId, account.username);
    }
    return Promise.resolve(result as ReadonlyMap<string, string>);
  }
}

export const unavailable = () => new DirectoryUnavailableError("persistence unavailable");
export const invalid = () => new InvalidAccountError("bad account");
