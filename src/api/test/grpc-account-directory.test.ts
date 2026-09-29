import { createServer, type Http2Server, type ServerHttp2Session } from "node:http2";
import type { AddressInfo } from "node:net";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { afterEach, describe, expect, it } from "vitest";
import {
  DirectoryUnavailableError,
  InvalidAccountError,
  UsernameTakenError,
} from "../src/account-directory.js";
import { createGrpcAccountDirectory } from "../src/grpc-account-directory.js";
import { AccountService, Role } from "../src/gen/hydrovalley/persistence/v1/account_pb.js";

type Handlers = Partial<ServiceImpl<typeof AccountService>>;

describe("gRPC account directory", () => {
  let server: Http2Server | undefined;
  const sessions = new Set<ServerHttp2Session>();
  const sleeps: number[] = [];

  // close() espera a que terminen las sesiones HTTP/2 abiertas y el cliente las
  // mantiene vivas: se destruyen primero para que la limpieza no se cuelgue.
  async function stop() {
    const s = server;
    server = undefined;
    if (s) {
      for (const session of sessions) session.destroy();
      sessions.clear();
      await new Promise<void>((resolve) => {
        s.close(() => {
          resolve();
        });
      });
    }
  }

  afterEach(async () => {
    sleeps.length = 0;
    clockOffset = 0;
    await stop();
  });

  async function start(handlers: Handlers) {
    const created = createServer(
      connectNodeAdapter({
        grpc: true,
        routes: (router) => {
          router.service(AccountService, handlers);
        },
      }),
    );
    created.on("session", (session) => {
      sessions.add(session);
      session.on("close", () => sessions.delete(session));
    });
    server = created;
    await new Promise<void>((resolve) => created.listen(0, "127.0.0.1", resolve));
    return `127.0.0.1:${String((created.address() as AddressInfo).port)}`;
  }

  let clockOffset = 0;
  const directory = (
    address: string,
    timeoutMs = 2000,
    extra: Partial<Parameters<typeof createGrpcAccountDirectory>[0]> = {},
  ) =>
    createGrpcAccountDirectory({
      address,
      timeoutMs,
      random: () => 0,
      now: () => Date.now() + clockOffset,
      sleep: (ms) => {
        sleeps.push(ms);
        clockOffset += ms;
        return Promise.resolve();
      },
      ...extra,
    });

  it("registers an account and maps the response", async () => {
    const seen: { username: string; hash: string }[] = [];
    const address = await start({
      register: (req) => {
        seen.push({ username: req.username, hash: req.passwordHash });
        return {
          accountId: "11111111-1111-4111-8111-111111111111",
          role: Role.PLAYER,
        };
      },
    });

    const result = await directory(address).register("ana", "$argon2id$hash");

    expect(result).toEqual({
      playerId: "11111111-1111-4111-8111-111111111111",
      role: "player",
    });
    expect(seen).toEqual([{ username: "ana", hash: "$argon2id$hash" }]);
  });

  it("maps ALREADY_EXISTS and INVALID_ARGUMENT to domain errors", async () => {
    const address = await start({
      register: (req) => {
        throw new ConnectError(
          "x",
          req.username === "dup" ? Code.AlreadyExists : Code.InvalidArgument,
        );
      },
    });
    const dir = directory(address);

    await expect(dir.register("dup", "h")).rejects.toBeInstanceOf(UsernameTakenError);
    await expect(dir.register("bad", "h")).rejects.toBeInstanceOf(InvalidAccountError);
  });

  it("never retries Register, even on UNAVAILABLE", async () => {
    let calls = 0;
    const address = await start({
      register: () => {
        calls++;
        throw new ConnectError("down", Code.Unavailable);
      },
    });

    await expect(directory(address).register("ana", "h")).rejects.toBeInstanceOf(
      DirectoryUnavailableError,
    );
    expect(calls).toBe(1);
    expect(sleeps).toEqual([]);
  });

  it("returns the account from GetAccount, mapping the admin role", async () => {
    const address = await start({
      getAccount: () => ({
        accountId: "id-1",
        passwordHash: "$argon2id$h",
        role: Role.ADMIN,
      }),
    });

    expect(await directory(address).getAccount("root")).toEqual({
      playerId: "id-1",
      passwordHash: "$argon2id$h",
      role: "admin",
    });
  });

  it("returns null when GetAccount answers NOT_FOUND, without retrying", async () => {
    let calls = 0;
    const address = await start({
      getAccount: () => {
        calls++;
        throw new ConnectError("none", Code.NotFound);
      },
    });

    expect(await directory(address).getAccount("ghost")).toBeNull();
    expect(calls).toBe(1);
  });

  it("retries GetAccount up to 3 times with 100/200/400 ms backoff, then succeeds", async () => {
    let calls = 0;
    const address = await start({
      getAccount: () => {
        calls++;
        if (calls < 4) throw new ConnectError("down", Code.Unavailable);
        return {
          accountId: "id-1",
          passwordHash: "$argon2id$h",
          role: Role.PLAYER,
        };
      },
    });

    const account = await directory(address).getAccount("ana");

    expect(account?.playerId).toBe("id-1");
    expect(calls).toBe(4);
    expect(sleeps).toEqual([100, 200, 400]);
  });

  it("gives up after 3 retries and reports unavailability", async () => {
    let calls = 0;
    const address = await start({
      getAccount: () => {
        calls++;
        throw new ConnectError("down", Code.Unavailable);
      },
    });

    await expect(directory(address).getAccount("ana")).rejects.toBeInstanceOf(
      DirectoryUnavailableError,
    );
    expect(calls).toBe(4);
  });

  it("does not retry non-transient errors from GetAccount", async () => {
    let calls = 0;
    const address = await start({
      getAccount: () => {
        calls++;
        throw new ConnectError("bug", Code.Internal);
      },
    });

    await expect(directory(address).getAccount("ana")).rejects.not.toBeInstanceOf(
      DirectoryUnavailableError,
    );
    expect(calls).toBe(1);
  });

  it("treats a missed deadline as unavailable", async () => {
    const address = await start({
      register: async () => {
        await new Promise((resolve) => setTimeout(resolve, 300));
        return { accountId: "id", role: Role.PLAYER };
      },
    });

    await expect(directory(address, 50).register("ana", "h")).rejects.toBeInstanceOf(
      DirectoryUnavailableError,
    );
  });

  it("treats a refused connection as unavailable", async () => {
    const address = await start({});
    await stop();

    await expect(directory(address).getAccount("ana")).rejects.toBeInstanceOf(
      DirectoryUnavailableError,
    );
  });

  it("maps GetUsernames into a Map", async () => {
    const address = await start({
      getUsernames: (req) => ({
        usernames: Object.fromEntries(req.playerIds.map((id) => [id, `user-${id}`])),
      }),
    });

    const result = await directory(address).getUsernames(["a", "b"]);

    expect([...result]).toEqual([
      ["a", "user-a"],
      ["b", "user-b"],
    ]);
  });

  it("adds up to 25% of jitter to each backoff", async () => {
    const address = await start({
      getAccount: () => {
        throw new ConnectError("down", Code.Unavailable);
      },
    });

    await expect(
      directory(address, 2000, {
        random: () => 1,
        budgetMs: 10_000,
      }).getAccount("ana"),
    ).rejects.toBeInstanceOf(DirectoryUnavailableError);
    expect(sleeps).toEqual([125, 250, 500]);
  });

  it("stops retrying when the next backoff would exceed the overall budget", async () => {
    let calls = 0;
    const address = await start({
      getAccount: () => {
        calls++;
        throw new ConnectError("down", Code.Unavailable);
      },
    });

    await expect(
      directory(address, 2000, { budgetMs: 250 }).getAccount("ana"),
    ).rejects.toBeInstanceOf(DirectoryUnavailableError);
    // 100 ms de espera caben; los 200 ms siguientes (100 + 200 > 250) no.
    expect(calls).toBe(2);
    expect(sleeps).toEqual([100]);
  });

  it("caps each attempt's deadline by the remaining budget", async () => {
    let calls = 0;
    const address = await start({
      getAccount: async () => {
        calls++;
        await new Promise((resolve) => setTimeout(resolve, 1500));
        return {
          accountId: "id",
          passwordHash: "$argon2id$h",
          role: Role.PLAYER,
        };
      },
    });

    const started = Date.now();
    await expect(
      directory(address, 2000, { budgetMs: 200 }).getAccount("ana"),
    ).rejects.toBeInstanceOf(DirectoryUnavailableError);

    expect(Date.now() - started).toBeLessThan(1000);
    expect(calls).toBe(1);
  });
});
