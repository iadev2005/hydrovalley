import {
  type Account,
  type AccountDirectory,
  DirectoryUnavailableError,
  InvalidAccountError,
  UsernameTakenError,
} from "../../src/account-directory.js";
import type { RankingSource, Ranking } from "../../src/ranking-source.js";
import type { Report, ReportQuery, ReportSource, ReportTotals } from "../../src/report-source.js";

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

/** Ranking en memoria: guarda el `limit` de cada llamada y puede fallar. */
export class FakeRankingSource implements RankingSource {
  readonly limits: number[] = [];
  ranking: Ranking = { tick: 0n, entries: [] };
  error: Error | undefined;

  getRanking(limit: number) {
    this.limits.push(limit);
    if (this.error) return Promise.reject(this.error);
    return Promise.resolve(this.ranking);
  }
}

const ZERO_TOTALS: ReportTotals = {
  waterReduced: 0n,
  waterReused: 0n,
  waterRecycled: 0n,
  waterRefilled: 0n,
  waterSpent: 0n,
  leavesEarned: 0n,
  leavesSpent: 0n,
};

/** Reportes en memoria: guarda las consultas y puede fallar. */
export class FakeReportSource implements ReportSource {
  readonly queries: ReportQuery[] = [];
  report: Report = {
    playerId: "",
    epoch: 1,
    fromTick: 0n,
    toTick: 0n,
    totals: ZERO_TOTALS,
  };
  error: Error | undefined;

  getReport(query: ReportQuery) {
    this.queries.push(query);
    if (this.error) return Promise.reject(this.error);
    return Promise.resolve(this.report);
  }
}
