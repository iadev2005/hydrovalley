import { Writable } from "node:stream";
import type { FastifyInstance } from "fastify";
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import { ADMIN_LIMIT_PER_ADMIN, BOT_HASH_CONCURRENCY } from "../src/limits.js";
import { createPasswordService, type PasswordService } from "../src/passwords.js";
import { createAttemptLimiter } from "../src/rate-limit.js";
import { UsernameTakenError } from "../src/account-directory.js";
import { createTokenService, createTokenVerifier } from "../src/tokens.js";
import { FakeAccountDirectory, unavailable } from "./support/fakes.js";
import { makeKeyPair } from "./support/keys.js";

const keys = makeKeyPair();
const NOW = Date.UTC(2026, 9, 1, 12, 0, 0);
const ADMIN_ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const PLAYER_ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const NAME = /^bot-[0-9a-f]{8}$/;

const tokens = createTokenService({
  privateKey: keys.privateKey,
  ttlSeconds: 43200,
  now: () => NOW,
});
const verifier = createTokenVerifier({ publicKey: keys.publicKey, now: () => NOW });

let realPasswords: PasswordService;
beforeAll(async () => {
  realPasswords = await createPasswordService();
});

describe("POST /api/admin/bot-accounts", () => {
  let app: FastifyInstance | undefined;
  let accounts: FakeAccountDirectory;
  let logs: string[];
  let hashed: string[];
  let active: number;
  let maxActive: number;
  let adminToken: string;
  let playerToken: string;

  async function setup(options: { limit?: number; hashDelayMs?: number; real?: boolean } = {}) {
    accounts = new FakeAccountDirectory();
    logs = [];
    hashed = [];
    active = 0;
    maxActive = 0;
    const passwords: PasswordService = options.real
      ? realPasswords
      : {
          dummyHash: "dummy",
          verify: () => Promise.resolve(false),
          async hash(password) {
            hashed.push(password);
            active++;
            maxActive = Math.max(maxActive, active);
            await new Promise((r) => setTimeout(r, options.hashDelayMs ?? 0));
            active--;
            return `$argon2id$fake$${password}`;
          },
        };
    const stream = new Writable({
      write(chunk: Buffer, _enc, done) {
        logs.push(chunk.toString());
        done();
      },
    });
    const limiter = createAttemptLimiter({
      limit: options.limit ?? 1000,
      windowMs: 60_000,
      now: () => NOW,
    });
    app = await buildApp(
      { logger: { level: "debug", stream } },
      undefined,
      undefined,
      { accounts, passwords, tokens, verifier, adminLimiter: limiter },
    );
    adminToken = (await tokens.issue({ playerId: ADMIN_ID, username: "root", role: "admin" })).token;
    playerToken = (await tokens.issue({ playerId: PLAYER_ID, username: "ana", role: "player" }))
      .token;
    return app;
  }

  const running = () => {
    if (!app) throw new Error("setup() was not called");
    return app;
  };
  const post = (body: unknown, token: string | null = adminToken) =>
    running().inject({
      method: "POST",
      url: "/api/admin/bot-accounts",
      payload: body as object,
      ...(token === null ? {} : { headers: { authorization: `Bearer ${token}` } }),
    });

  afterEach(async () => {
    await app?.close();
    app = undefined;
  });

  it("creates the requested accounts and returns verifiable player tokens", async () => {
    await setup();
    const res = await post({ count: 3 });
    expect(res.statusCode).toBe(201);
    const body = res.json<{
      accounts: { player_id: string; username: string; token: string; expires_at: string }[];
    }>();
    expect(body.accounts).toHaveLength(3);
    expect(accounts.calls.register).toBe(3);
    expect(new Set(body.accounts.map((a) => a.username)).size).toBe(3);
    for (const a of body.accounts) {
      expect(a.username).toMatch(NAME);
      expect(a.expires_at).toBe(new Date(NOW + 43200_000).toISOString());
      const verified = await verifier.verify(a.token);
      expect(verified).toEqual({ playerId: a.player_id, username: a.username, role: "player" });
    }
  });

  it("hashes a distinct random password per account with Argon2id and never exposes it", async () => {
    await setup({ real: true });
    const res = await post({ count: 2 });
    expect(res.statusCode).toBe(201);
    const stored = [...accounts.accounts.values()].map((a) => a.passwordHash);
    expect(stored).toHaveLength(2);
    for (const h of stored) expect(h).toMatch(/^\$argon2id\$v=19\$m=19456,t=2,p=1\$/);
    expect(stored[0]).not.toBe(stored[1]);
    expect(Object.keys(res.json<{ accounts: object[] }>().accounts[0] ?? {}).sort()).toEqual([
      "expires_at",
      "player_id",
      "token",
      "username",
    ]);
  });

  it("does not log passwords", async () => {
    await setup();
    await post({ count: 2 });
    await app?.close();
    expect(hashed).toHaveLength(2);
    const all = logs.join("");
    for (const pw of hashed) expect(all).not.toContain(pw);
    expect(all).not.toContain(adminToken);
  });

  it("uses the requested prefix", async () => {
    await setup();
    const res = await post({ count: 1, prefix: "load.test_1" });
    expect(res.statusCode).toBe(201);
    expect(res.json<{ accounts: { username: string }[] }>().accounts[0]?.username).toMatch(
      /^load\.test_1-[0-9a-f]{8}$/,
    );
  });

  it("answers 401 without a token or with an invalid one", async () => {
    await setup();
    expect((await post({ count: 1 }, null)).statusCode).toBe(401);
    const bad = await post({ count: 1 }, "not-a-token");
    expect(bad.statusCode).toBe(401);
    expect(bad.headers["www-authenticate"]).toMatch(/^Bearer/);
    expect(accounts.calls.register).toBe(0);
  });

  it("answers 403 for a player token", async () => {
    await setup();
    const res = await post({ count: 1 }, playerToken);
    expect(res.statusCode).toBe(403);
    expect(accounts.calls.register).toBe(0);
  });

  it.each([
    [{}],
    [{ count: 0 }],
    [{ count: 101 }],
    [{ count: 1.5 }],
    [{ count: "3" }],
    [{ count: 1, prefix: "" }],
    [{ count: 1, prefix: "a".repeat(24) }],
    [{ count: 1, prefix: "bad prefix" }],
    [{ count: 1, prefix: "ñu" }],
  ])("rejects an invalid body %j with 400", async (body) => {
    await setup();
    const res = await post(body);
    expect(res.statusCode).toBe(400);
    expect(accounts.calls.register).toBe(0);
  });

  it("accepts the limits: count 100 and a 23-character prefix", async () => {
    await setup();
    const res = await post({ count: 100, prefix: "a".repeat(23) });
    expect(res.statusCode).toBe(201);
    expect(res.json<{ accounts: object[] }>().accounts).toHaveLength(100);
  });

  it("generates another name when the name is taken, without retrying Register on the same name", async () => {
    await setup();
    const real = accounts.register.bind(accounts);
    const seen: string[] = [];
    let first = true;
    accounts.register = (username, hash) => {
      seen.push(username);
      if (first) {
        first = false;
        accounts.calls.register++;
        return Promise.reject(new UsernameTakenError());
      }
      return real(username, hash);
    };
    const res = await post({ count: 1 });
    expect(res.statusCode).toBe(201);
    expect(seen).toHaveLength(2);
    expect(seen[0]).not.toBe(seen[1]);
    expect(res.json<{ accounts: { username: string }[] }>().accounts[0]?.username).toBe(seen[1]);
  });

  it("gives up with 503 after bounded attempts when every name is taken", async () => {
    await setup();
    accounts.registerError = new UsernameTakenError();
    const res = await post({ count: 1 });
    expect(res.statusCode).toBe(503);
    expect(res.json()).toMatchObject({ created: 0 });
    expect(accounts.calls.register).toBeLessThanOrEqual(10);
    expect(accounts.calls.register).toBeGreaterThan(1);
  });

  it("answers 503 with the created count, no tokens and no retry when persistence fails midway", async () => {
    await setup();
    const real = accounts.register.bind(accounts);
    let calls = 0;
    accounts.register = (username, hash) => {
      calls++;
      if (calls === 3) {
        accounts.calls.register++;
        return Promise.reject(unavailable());
      }
      return real(username, hash);
    };
    const res = await post({ count: 10 });
    expect(res.statusCode).toBe(503);
    const body = res.json<{ created: number }>();
    expect(body.created).toBe(accounts.accounts.size);
    expect(body.created).toBeGreaterThanOrEqual(2);
    expect(body).not.toHaveProperty("accounts");
    expect(JSON.stringify(body)).not.toMatch(/eyJ/);
    // Fail fast: no se siguen creando cuentas tras el fallo (solo las que ya iban en curso).
    expect(calls).toBeLessThanOrEqual(2 + BOT_HASH_CONCURRENCY);
  });

  it("answers 503 with created 0 when persistence is down from the start", async () => {
    await setup();
    accounts.registerError = unavailable();
    const res = await post({ count: 5 });
    expect(res.statusCode).toBe(503);
    expect(res.json()).toMatchObject({ created: 0 });
    expect(accounts.calls.register).toBeLessThanOrEqual(BOT_HASH_CONCURRENCY);
  });

  it("hashes with bounded concurrency", async () => {
    await setup({ hashDelayMs: 5 });
    const res = await post({ count: 20 });
    expect(res.statusCode).toBe(201);
    expect(maxActive).toBeGreaterThan(1);
    expect(maxActive).toBeLessThanOrEqual(BOT_HASH_CONCURRENCY);
  });

  it("limits requests per admin with 429 and Retry-After, after authenticating", async () => {
    await setup({ limit: 2 });
    expect((await post({ count: 1 })).statusCode).toBe(201);
    expect((await post({ count: 1 })).statusCode).toBe(201);
    const limited = await post({ count: 1 });
    expect(limited.statusCode).toBe(429);
    expect(Number(limited.headers["retry-after"])).toBeGreaterThan(0);
    // Un token inválido no consume el cupo del administrador.
    expect((await post({ count: 1 }, "nope")).statusCode).toBe(401);
  });

  it("documents the route in OpenAPI with the admin limit", async () => {
    await setup();
    const doc = (await running().inject({ method: "GET", url: "/api/docs/json" })).json<{
      paths: Record<string, { post?: { description?: string; security?: unknown } }>;
    }>();
    const op = doc.paths["/api/admin/bot-accounts"]?.post;
    expect(op?.security).toEqual([{ bearerAuth: [] }]);
    expect(op?.description).toContain(String(ADMIN_LIMIT_PER_ADMIN.limit));
  });
});
