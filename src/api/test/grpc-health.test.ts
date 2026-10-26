import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, describe, expect, it } from "vitest";
import { createGrpcHealthProbe, Health, ServingStatus } from "../src/grpc-health.js";
import { TestGrpcServer } from "./support/grpc-server.js";

const server = new TestGrpcServer();
afterEach(() => server.stop());

describe("gRPC health probe", () => {
  it("is up when the service is SERVING and asks for the given service name", async () => {
    const seen: string[] = [];
    const address = await server.start(Health, {
      check: (req) => {
        seen.push(req.service);
        return { status: ServingStatus.SERVING };
      },
    });

    const result = await createGrpcHealthProbe({
      address,
      service: "hydrovalley.world.v1.WorldService",
    })();

    expect(result).toEqual({ status: "up" });
    expect(seen).toEqual(["hydrovalley.world.v1.WorldService"]);
  });

  it("sends the empty service name for the overall status", async () => {
    const seen: string[] = [];
    const address = await server.start(Health, {
      check: (req) => {
        seen.push(req.service);
        return { status: ServingStatus.SERVING };
      },
    });

    await createGrpcHealthProbe({ address, service: "" })();

    expect(seen).toEqual([""]);
  });

  it("is down with not_serving on NOT_SERVING", async () => {
    const address = await server.start(Health, {
      check: () => ({ status: ServingStatus.NOT_SERVING }),
    });

    expect(await createGrpcHealthProbe({ address, service: "" })()).toEqual({
      status: "down",
      detail: "not_serving",
    });
  });

  it("is down with not_serving on SERVICE_UNKNOWN", async () => {
    const address = await server.start(Health, {
      check: () => ({ status: ServingStatus.SERVICE_UNKNOWN }),
    });

    expect(await createGrpcHealthProbe({ address, service: "" })()).toEqual({
      status: "down",
      detail: "not_serving",
    });
  });

  it("is down with a short detail on a gRPC error, without leaking the message", async () => {
    const address = await server.start(Health, {
      check: () => {
        throw new ConnectError("secret internal detail 10.0.0.9", Code.Internal);
      },
    });

    expect(await createGrpcHealthProbe({ address, service: "" })()).toEqual({
      status: "down",
      detail: "error",
    });
  });

  it("is down with timeout when the call exceeds the deadline", async () => {
    const address = await server.start(Health, {
      check: async () => {
        await new Promise((r) => setTimeout(r, 500));
        return { status: ServingStatus.SERVING };
      },
    });

    expect(await createGrpcHealthProbe({ address, service: "", timeoutMs: 50 })()).toEqual({
      status: "down",
      detail: "timeout",
    });
  });

  it("is down with unavailable when nobody listens", async () => {
    const address = await server.start(Health, {});
    await server.stop();

    expect(await createGrpcHealthProbe({ address, service: "", timeoutMs: 500 })()).toEqual({
      status: "down",
      detail: "unavailable",
    });
  });

  it("fails over to the backup address when the primary is down (B8 W2)", async () => {
    const deadServer = new TestGrpcServer();
    const deadAddr = await deadServer.start(Health, {});
    await deadServer.stop();

    const standbyAddr = await server.start(Health, {
      check: () => ({ status: ServingStatus.SERVING }),
    });

    const probe = createGrpcHealthProbe({
      address: `${deadAddr},${standbyAddr}`,
      service: "hydrovalley.world.v1.WorldService",
      timeoutMs: 300,
    });
    expect(await probe()).toEqual({ status: "up" });
  });
});
