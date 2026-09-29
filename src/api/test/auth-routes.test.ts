import { Writable } from "node:stream";
import type { FastifyInstance } from "fastify";
import { jwtVerify } from "jose";
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import { createPasswordService, type PasswordService } from "../src/passwords.js";
import { createAttemptLimiter } from "../src/rate-limit.js";
import { createTokenService } from "../src/tokens.js";
import { FakeAccountDirectory, invalid, unavailable } from "./support/fakes.js";
import { makeKeyPair } from "./support/keys.js";

const keys = makeKeyPair();
const NOW = Date.UTC(2026, 9, 1, 12, 0, 0);
let passwords: PasswordService;

beforeAll(async () => {
  passwords = await createPasswordService();
});

describe("auth routes", () => {
  let app: FastifyInstance | undefined;
  let accounts: FakeAccountDirectory;
  let verifyCalls: string[];
  let logs: string[];

  async function setup(limit = 10) {
    accounts = new FakeAccountDirectory();
    verifyCalls = [];
    logs = [];
    const spy: PasswordService = {
      hash: (password) => passwords.hash(password),
      verify: (hash, password) => {
        verifyCalls.push(hash);
        return passwords.verify(hash, password);
      },
      dummyHash: passwords.dummyHash,
    };
    const stream = new Writable({
      write(chunk: Buffer, _enc, done) {
        logs.push(chunk.toString());
        done();
      },
    });
    app = await buildApp(
      { logger: { level: "debug", stream } },
      {
        accounts,
        passwords: spy,
        tokens: createTokenService({
          privateKey: keys.privateKey,
          ttlSeconds: 43200,
          now: () => NOW,
        }),
        loginLimiter: createAttemptLimiter({ limit, windowMs: 60_000, now: () => NOW }),
      },
    );
    return app;
  }

  const running = () => {
    if (!app) throw new Error("setup() was not called");
    return app;
  };
  const post = (url: string, payload: unknown) =>
    running().inject({ method: "POST", url, payload: payload as object });

  afterEach(async () => {
    await app?.close();
    app = undefined;
  });

  describe("POST /api/auth/register", () => {
    it("creates the account with an Argon2id hash and answers 201 with a token", async () => {
      await setup();
      const res = await post("/api/auth/register", { username: "ana", password: "s3cret-pass" });

      expect(res.statusCode).toBe(201);
      const body = res.json<Record<string, string>>();
      expect(Object.keys(body).sort()).toEqual(["expires_at", "player_id", "token", "username"]);
      expect(body.player_id).toMatch(/^[0-9a-f-]{36}$/);
      expect(body.username).toBe("ana");
      expect(body.expires_at).toBe("2026-10-02T00:00:00.000Z");
      const { payload } = await jwtVerify(body.token ?? "", keys.publicKey, {
        algorithms: ["EdDSA"],
        currentDate: new Date(NOW),
      });
      expect(payload).toMatchObject({
        sub: body.player_id,
        player_id: body.player_id,
        username: "ana",
        role: "player",
        exp: NOW / 1000 + 43200,
      });
      expect(accounts.calls.getAccount).toBe(0);
      expect(accounts.calls.getUsernames).toBe(0);
      expect(accounts.lastRegisteredHash).toMatch(/^\$argon2id\$/);
      expect(accounts.lastRegisteredHash).not.toContain("s3cret-pass");
      expect(res.body).not.toContain("argon2");
    });

    it("answers 409 when the username exists (case-insensitive)", async () => {
      await setup();
      accounts.seed("Ana", "$argon2id$x");
      const res = await post("/api/auth/register", { username: "ana", password: "s3cret-pass" });

      expect(res.statusCode).toBe(409);
      expect(res.json()).toEqual({ error: "El nombre de usuario ya existe" });
    });

    it.each([
      ["too short username", { username: "ab", password: "s3cret-pass" }],
      ["too long username", { username: "a".repeat(33), password: "s3cret-pass" }],
      ["forbidden characters", { username: "ana lopez", password: "s3cret-pass" }],
      ["non-ASCII username", { username: "añaña", password: "s3cret-pass" }],
      ["short password", { username: "ana", password: "1234567" }],
      ["long password", { username: "ana", password: "x".repeat(129) }],
      ["missing password", { username: "ana" }],
      ["numeric username", { username: 12345, password: "s3cret-pass" }],
    ])("answers 400 for %s without calling persistence", async (_name, payload) => {
      await setup();
      const res = await post("/api/auth/register", payload);

      expect(res.statusCode).toBe(400);
      expect(res.json<{ error: string }>().error).toEqual(expect.any(String));
      expect(accounts.calls.register).toBe(0);
    });

    it("accepts the boundaries: 3 and 32 characters, 8 and 128", async () => {
      await setup();
      expect(
        (await post("/api/auth/register", { username: "abc", password: "12345678" })).statusCode,
      ).toBe(201);
      expect(
        (await post("/api/auth/register", { username: "A.b-c_".repeat(5) + "de", password: "x".repeat(128) }))
          .statusCode,
      ).toBe(201);
    });

    it("maps INVALID_ARGUMENT from persistence to 400", async () => {
      await setup();
      accounts.registerError = invalid();
      const res = await post("/api/auth/register", { username: "ana", password: "s3cret-pass" });

      expect(res.statusCode).toBe(400);
    });

    it("answers 503 with a clear message and never retries Register", async () => {
      await setup();
      accounts.registerError = unavailable();
      const res = await post("/api/auth/register", { username: "ana", password: "s3cret-pass" });

      expect(res.statusCode).toBe(503);
      expect(res.json<{ error: string }>().error).toMatch(/no está disponible/);
      expect(accounts.calls.register).toBe(1);
    });

    it("hides unexpected errors behind a generic 500", async () => {
      await setup();
      accounts.registerError = new Error("boom with internal detail");
      const res = await post("/api/auth/register", { username: "ana", password: "s3cret-pass" });

      expect(res.statusCode).toBe(500);
      expect(res.body).not.toContain("internal detail");
    });
  });

  describe("POST /api/auth/login", () => {
    async function registered() {
      await setup();
      const hash = await passwords.hash("s3cret-pass");
      return accounts.seed("ana", hash, "admin");
    }

    it("answers 200 with a token that verifies with the public key", async () => {
      const account = await registered();
      const res = await post("/api/auth/login", { username: "ANA", password: "s3cret-pass" });

      expect(res.statusCode).toBe(200);
      const body = res.json<{ token: string; expires_at: string }>();
      expect(body.expires_at).toBe("2026-10-02T00:00:00.000Z");
      const { payload } = await jwtVerify(body.token, keys.publicKey, {
        algorithms: ["EdDSA"],
        currentDate: new Date(NOW),
      });
      expect(payload).toMatchObject({
        sub: account.playerId,
        player_id: account.playerId,
        username: "ana",
        role: "admin",
        exp: NOW / 1000 + 43200,
      });
      expect(Object.keys(body).sort()).toEqual(["expires_at", "token"]);
    });

    it("answers 401 for a wrong password and for an unknown user with the same body", async () => {
      await registered();
      const wrong = await post("/api/auth/login", { username: "ana", password: "not-the-one" });
      const unknown = await post("/api/auth/login", { username: "ghost", password: "not-the-one" });

      expect(wrong.statusCode).toBe(401);
      expect(unknown.statusCode).toBe(401);
      expect(unknown.body).toBe(wrong.body);
      expect(wrong.json()).toEqual({ error: "Usuario o contraseña incorrectos" });
    });

    it("verifies against the dummy hash when the user does not exist", async () => {
      await registered();
      await post("/api/auth/login", { username: "ghost", password: "whatever-pass" });

      expect(verifyCalls).toEqual([passwords.dummyHash]);
    });

    it("verifies against the stored hash when the user exists", async () => {
      const account = await registered();
      await post("/api/auth/login", { username: "ana", password: "s3cret-pass" });

      expect(verifyCalls).toEqual([account.passwordHash]);
    });

    it("answers 401 (not 400) for a username that could never be registered", async () => {
      await registered();
      const res = await post("/api/auth/login", { username: "no valid!", password: "whatever-pass" });

      expect(res.statusCode).toBe(401);
    });

    it("answers 400 when the body is malformed", async () => {
      await registered();

      expect((await post("/api/auth/login", { username: "ana" })).statusCode).toBe(400);
      expect((await post("/api/auth/login", { username: "ana", password: "" })).statusCode).toBe(400);
    });

    it("answers 503 when persistence is unavailable, and retries are the client's job", async () => {
      await registered();
      accounts.getAccountError = unavailable();
      const res = await post("/api/auth/login", { username: "ana", password: "s3cret-pass" });

      expect(res.statusCode).toBe(503);
      expect(res.json<{ error: string }>().error).toMatch(/no está disponible/);
    });

    it("answers 429 after the limit for the same username and IP", async () => {
      await setup(3);
      accounts.seed("ana", await passwords.hash("s3cret-pass"));
      for (let i = 0; i < 3; i++) {
        expect((await post("/api/auth/login", { username: "ana", password: "bad-password" })).statusCode).toBe(401);
      }
      const calls = accounts.calls.getAccount;
      const res = await post("/api/auth/login", { username: "Ana", password: "s3cret-pass" });

      expect(res.statusCode).toBe(429);
      expect(res.headers["retry-after"]).toBe("60");
      expect(res.json<{ error: string }>().error).toMatch(/Demasiados intentos/);
      expect(accounts.calls.getAccount).toBe(calls);
    });

    it("does not limit other usernames from the same IP", async () => {
      await setup(1);
      await post("/api/auth/login", { username: "ana", password: "bad-password" });

      const other = await post("/api/auth/login", { username: "bob", password: "bad-password" });
      expect(other.statusCode).toBe(401);
    });
  });

  it("never writes passwords or hashes to the logs", async () => {
    await setup();
    await post("/api/auth/register", { username: "ana", password: "very-secret-pw" });
    await post("/api/auth/login", { username: "ana", password: "very-secret-pw" });
    await post("/api/auth/login", { username: "ana", password: "other-secret-pw" });

    const all = logs.join("");
    expect(all).not.toContain("very-secret-pw");
    expect(all).not.toContain("other-secret-pw");
    expect(all).not.toContain("$argon2");
  });

  it("documents both routes in OpenAPI with their statuses and no hash field", async () => {
    await setup();
    const res = await running().inject({ method: "GET", url: "/api/docs/json" });
    const doc = res.json<{ paths: Record<string, { post: { responses: Record<string, unknown> } }> }>();

    expect(Object.keys(doc.paths["/api/auth/register"]?.post.responses ?? {}).sort()).toEqual(
      expect.arrayContaining(["201", "400", "409", "503"]),
    );
    expect(Object.keys(doc.paths["/api/auth/login"]?.post.responses ?? {}).sort()).toEqual(
      expect.arrayContaining(["200", "400", "401", "429", "503"]),
    );
    expect(res.body).not.toContain("password_hash");
  });

  it("does not register auth routes when no auth dependencies are given", async () => {
    app = await buildApp();
    const res = await app.inject({ method: "POST", url: "/api/auth/login", payload: {} });

    expect(res.statusCode).toBe(404);
  });
});
