import { describe, expect, it } from "vitest";
import { loadConfig } from "../src/config.js";

describe("loadConfig", () => {
  it("uses 0.0.0.0:3000 when API_LISTEN is not set", () => {
    expect(loadConfig({})).toEqual({ host: "0.0.0.0", port: 3000 });
  });

  it("reads host and port from API_LISTEN", () => {
    expect(loadConfig({ API_LISTEN: "127.0.0.1:4000" })).toEqual({
      host: "127.0.0.1",
      port: 4000,
    });
  });

  it("accepts a bare :port and listens on every interface", () => {
    expect(loadConfig({ API_LISTEN: ":3001" })).toEqual({
      host: "0.0.0.0",
      port: 3001,
    });
  });

  it("accepts a bracketed IPv6 host", () => {
    expect(loadConfig({ API_LISTEN: "[::]:3000" })).toEqual({
      host: "::",
      port: 3000,
    });
  });

  it.each(["3000", "localhost", "host:", "host:abc", "host:0", "host:65536"])(
    "rejects the invalid value %s",
    (value) => {
      expect(() => loadConfig({ API_LISTEN: value })).toThrow(/API_LISTEN/);
    },
  );
});
