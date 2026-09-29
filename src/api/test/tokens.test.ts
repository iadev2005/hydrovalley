import { generateKeyPairSync } from "node:crypto";
import { decodeProtectedHeader, jwtVerify } from "jose";
import { describe, expect, it } from "vitest";
import { createTokenService } from "../src/tokens.js";

const { privateKey, publicKey } = generateKeyPairSync("ed25519");
const PLAYER_ID = "0b6f2d3c-6f5e-4c53-9d0e-3f6f5f4d9a11";

describe("token service", () => {
  it("signs an EdDSA JWT that the public key verifies, with the agreed claims", async () => {
    const now = Date.UTC(2026, 9, 1, 12, 0, 0);
    const tokens = createTokenService({ privateKey, ttlSeconds: 43200, now: () => now });

    const { token, expiresAt } = await tokens.issue({
      playerId: PLAYER_ID,
      username: "ana",
      role: "player",
    });

    const { payload, protectedHeader } = await jwtVerify(token, publicKey, {
      algorithms: ["EdDSA"],
      currentDate: new Date(now),
    });
    expect(protectedHeader.alg).toBe("EdDSA");
    expect(payload).toEqual({
      sub: PLAYER_ID,
      player_id: PLAYER_ID,
      username: "ana",
      role: "player",
      iat: now / 1000,
      exp: now / 1000 + 43200,
    });
    expect(expiresAt.toISOString()).toBe("2026-10-02T00:00:00.000Z");
  });

  it("rejects the token after it expires", async () => {
    const now = Date.UTC(2026, 9, 1, 12, 0, 0);
    const tokens = createTokenService({ privateKey, ttlSeconds: 60, now: () => now });
    const { token } = await tokens.issue({ playerId: PLAYER_ID, username: "ana", role: "admin" });

    await expect(
      jwtVerify(token, publicKey, { currentDate: new Date(now + 61_000) }),
    ).rejects.toThrow(/exp/);
  });

  it("puts the key id in the header only when configured", async () => {
    const plain = createTokenService({ privateKey, ttlSeconds: 60 });
    const withKid = createTokenService({ privateKey, ttlSeconds: 60, keyId: "k1" });
    const claims = { playerId: PLAYER_ID, username: "ana", role: "player" } as const;

    expect(decodeProtectedHeader((await plain.issue(claims)).token).kid).toBeUndefined();
    expect(decodeProtectedHeader((await withKid.issue(claims)).token).kid).toBe("k1");
  });

  it("rejects a key that is not Ed25519", () => {
    const { privateKey: rsa } = generateKeyPairSync("rsa", { modulusLength: 2048 });

    expect(() => createTokenService({ privateKey: rsa, ttlSeconds: 60 })).toThrow(/Ed25519/);
  });
});
