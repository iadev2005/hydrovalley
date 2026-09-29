import { generateKeyPairSync } from "node:crypto";
import { describe, expect, it } from "vitest";
import { loadConfig, loadListenConfig } from "../src/config.js";
import { makeKeyPair } from "./support/keys.js";

const { pem } = makeKeyPair();
const REQUIRED = { PERSISTENCE_ADDR: "persistence:50051", TOKEN_PRIVATE_KEY: pem };

describe("loadConfig", () => {
  it("uses 0.0.0.0:3000 when API_LISTEN is not set", () => {
    expect(loadConfig(REQUIRED)).toMatchObject({ host: "0.0.0.0", port: 3000, logLevel: "info" });
  });

  it("reads host and port from API_LISTEN", () => {
    expect(loadConfig({ ...REQUIRED, API_LISTEN: "127.0.0.1:4000" })).toMatchObject({
      host: "127.0.0.1",
      port: 4000,
      logLevel: "info",
    });
  });

  it("accepts a bare :port and listens on every interface", () => {
    expect(loadConfig({ ...REQUIRED, API_LISTEN: ":3001" })).toMatchObject({
      host: "0.0.0.0",
      port: 3001,
      logLevel: "info",
    });
  });

  it("accepts a bracketed IPv6 host", () => {
    expect(loadConfig({ ...REQUIRED, API_LISTEN: "[::]:3000" })).toMatchObject({
      host: "::",
      port: 3000,
      logLevel: "info",
    });
  });

  it.each(["3000", "localhost", "host:", "host:abc", "host:0", "host:65536"])(
    "rejects the invalid value %s",
    (value) => {
      expect(() => loadConfig({ ...REQUIRED, API_LISTEN: value })).toThrow(/API_LISTEN/);
    },
  );
});

describe("loadConfig LOG_LEVEL", () => {
  it.each(["fatal", "error", "warn", "info", "debug", "trace", "silent"])(
    "accepts the pino level %s",
    (level) => {
      expect(loadConfig({ ...REQUIRED, LOG_LEVEL: level }).logLevel).toBe(level);
    },
  );

  it("defaults to info when LOG_LEVEL is not set", () => {
    expect(loadConfig(REQUIRED).logLevel).toBe("info");
  });

  it.each(["verbose", "INFO", "", "warning"])("rejects the invalid level %j", (level) => {
    expect(() => loadConfig({ ...REQUIRED, LOG_LEVEL: level })).toThrow(
      /^LOG_LEVEL must be one of fatal, error, warn, info, debug, trace, silent; got ".*"$/,
    );
  });
});

describe("loadListenConfig", () => {
  it("reads only the listening settings, without the auth variables", () => {
    expect(loadListenConfig({ API_LISTEN: "127.0.0.1:4000" })).toEqual({
      host: "127.0.0.1",
      port: 4000,
      logLevel: "info",
    });
  });
});

describe("loadConfig PERSISTENCE_ADDR", () => {
  it("is required and has no default host", () => {
    expect(() => loadConfig({ TOKEN_PRIVATE_KEY: pem })).toThrow(/PERSISTENCE_ADDR/);
  });

  it("reads host:port as is", () => {
    expect(loadConfig(REQUIRED).persistenceAddr).toBe("persistence:50051");
  });

  it.each(["persistence", "host:", ":50051", "host:0", "host:70000", "http://host:50051", "a b:1"])(
    "rejects the invalid value %j",
    (value) => {
      expect(() => loadConfig({ ...REQUIRED, PERSISTENCE_ADDR: value })).toThrow(/PERSISTENCE_ADDR/);
    },
  );
});

describe("loadConfig TOKEN_PRIVATE_KEY", () => {
  it("is required and the error names the variable", () => {
    expect(() => loadConfig({ PERSISTENCE_ADDR: "p:1" })).toThrow(/TOKEN_PRIVATE_KEY/);
  });

  it("accepts a PKCS#8 PEM", () => {
    expect(loadConfig(REQUIRED).tokenPrivateKey.asymmetricKeyType).toBe("ed25519");
  });

  it("accepts the PEM with escaped newlines, as some .env loaders leave it", () => {
    const escaped = pem.trim().replaceAll("\n", "\\n");
    expect(
      loadConfig({ ...REQUIRED, TOKEN_PRIVATE_KEY: escaped }).tokenPrivateKey.asymmetricKeyType,
    ).toBe("ed25519");
  });

  it("accepts the base64 of the PEM", () => {
    const b64 = Buffer.from(pem).toString("base64");
    expect(
      loadConfig({ ...REQUIRED, TOKEN_PRIVATE_KEY: b64 }).tokenPrivateKey.asymmetricKeyType,
    ).toBe("ed25519");
  });

  it("rejects garbage without echoing the value", () => {
    const attempt = () => loadConfig({ ...REQUIRED, TOKEN_PRIVATE_KEY: "super-secret-garbage" });
    expect(attempt).toThrow(/TOKEN_PRIVATE_KEY/);
    expect(attempt).not.toThrow(/super-secret-garbage/);
  });

  it("rejects a key that is not Ed25519", () => {
    const rsa = generateKeyPairSync("rsa", { modulusLength: 2048 })
      .privateKey.export({ type: "pkcs8", format: "pem" })
      .toString();
    expect(() => loadConfig({ ...REQUIRED, TOKEN_PRIVATE_KEY: rsa })).toThrow(/Ed25519/);
  });

  it("rejects a public key", () => {
    const pub = generateKeyPairSync("ed25519")
      .publicKey.export({ type: "spki", format: "pem" })
      .toString();
    expect(() => loadConfig({ ...REQUIRED, TOKEN_PRIVATE_KEY: pub })).toThrow(/TOKEN_PRIVATE_KEY/);
  });
});

describe("loadConfig TOKEN_TTL_SECONDS and extras", () => {
  it("defaults to 12 hours", () => {
    expect(loadConfig(REQUIRED).tokenTtlSeconds).toBe(43200);
  });

  it("reads a custom value", () => {
    expect(loadConfig({ ...REQUIRED, TOKEN_TTL_SECONDS: "600" }).tokenTtlSeconds).toBe(600);
  });

  it.each(["0", "-5", "abc", "1.5", ""])("rejects %j", (value) => {
    expect(() => loadConfig({ ...REQUIRED, TOKEN_TTL_SECONDS: value })).toThrow(/TOKEN_TTL_SECONDS/);
  });

  it("reads the optional key id", () => {
    expect(loadConfig(REQUIRED).tokenKeyId).toBeUndefined();
    expect(loadConfig({ ...REQUIRED, TOKEN_KEY_ID: "k1" }).tokenKeyId).toBe("k1");
  });

  it("trusts the proxy only when TRUST_PROXY=true", () => {
    expect(loadConfig(REQUIRED).trustProxy).toBe(false);
    expect(loadConfig({ ...REQUIRED, TRUST_PROXY: "true" }).trustProxy).toBe(true);
    expect(() => loadConfig({ ...REQUIRED, TRUST_PROXY: "yes" })).toThrow(/TRUST_PROXY/);
  });
});
