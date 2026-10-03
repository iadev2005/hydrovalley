import { generateKeyPairSync } from "node:crypto";
import { decodeProtectedHeader, jwtVerify, SignJWT } from "jose";
import { describe, expect, it } from "vitest";
import {
  createTokenService,
  createTokenVerifier,
  TOKEN_AUDIENCE,
  TOKEN_ISSUER,
} from "../src/tokens.js";

const { privateKey, publicKey } = generateKeyPairSync("ed25519");
const PLAYER_ID = "0b6f2d3c-6f5e-4c53-9d0e-3f6f5f4d9a11";

describe("token service", () => {
  it("signs an EdDSA JWT that the public key verifies, with the agreed claims", async () => {
    const now = Date.UTC(2026, 9, 1, 12, 0, 0);
    const tokens = createTokenService({
      privateKey,
      ttlSeconds: 43200,
      now: () => now,
    });

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
      iss: "hydrovalley-api",
      aud: "hydrovalley-play",
      sub: PLAYER_ID,
      player_id: PLAYER_ID,
      username: "ana",
      role: "player",
      iat: now / 1000,
      exp: now / 1000 + 43200,
    });
    expect(expiresAt.toISOString()).toBe("2026-10-02T00:00:00.000Z");
  });

  it("exposes the issuer and audience as named constants", () => {
    expect(TOKEN_ISSUER).toBe("hydrovalley-api");
    expect(TOKEN_AUDIENCE).toBe("hydrovalley-play");
  });

  it("rejects the token after it expires", async () => {
    const now = Date.UTC(2026, 9, 1, 12, 0, 0);
    const tokens = createTokenService({
      privateKey,
      ttlSeconds: 60,
      now: () => now,
    });
    const { token } = await tokens.issue({
      playerId: PLAYER_ID,
      username: "ana",
      role: "admin",
    });

    await expect(
      jwtVerify(token, publicKey, { currentDate: new Date(now + 61_000) }),
    ).rejects.toThrow(/exp/);
  });

  it("puts the key id in the header only when configured", async () => {
    const plain = createTokenService({ privateKey, ttlSeconds: 60 });
    const withKid = createTokenService({
      privateKey,
      ttlSeconds: 60,
      keyId: "k1",
    });
    const claims = {
      playerId: PLAYER_ID,
      username: "ana",
      role: "player",
    } as const;

    expect(decodeProtectedHeader((await plain.issue(claims)).token).kid).toBeUndefined();
    expect(decodeProtectedHeader((await withKid.issue(claims)).token).kid).toBe("k1");
  });

  it("rejects a key that is not Ed25519", () => {
    const { privateKey: rsa } = generateKeyPairSync("rsa", {
      modulusLength: 2048,
    });

    expect(() => createTokenService({ privateKey: rsa, ttlSeconds: 60 })).toThrow(/Ed25519/);
  });
});

describe("token verifier", () => {
  const NOW = Date.UTC(2026, 9, 1, 12, 0, 0);
  const signer = (ttlSeconds = 43200, key = privateKey) =>
    createTokenService({ privateKey: key, ttlSeconds, now: () => NOW });
  const verifier = (key = publicKey, now = NOW) =>
    createTokenVerifier({ publicKey: key, now: () => now });

  it("accepts a token issued by the signer and returns its claims", async () => {
    const { token } = await signer().issue({
      playerId: PLAYER_ID,
      username: "ana",
      role: "admin",
    });

    expect(await verifier().verify(token)).toEqual({
      playerId: PLAYER_ID,
      username: "ana",
      role: "admin",
    });
  });

  it("rejects an expired token", async () => {
    const { token } = await signer(60).issue({
      playerId: PLAYER_ID,
      username: "ana",
      role: "player",
    });

    expect(await verifier(publicKey, NOW + 61_000).verify(token)).toBeNull();
  });

  it("rejects a token signed with another key", async () => {
    const other = generateKeyPairSync("ed25519");
    const { token } = await signer(60, other.privateKey).issue({
      playerId: PLAYER_ID,
      username: "ana",
      role: "player",
    });

    expect(await verifier().verify(token)).toBeNull();
  });

  it("rejects garbage and an empty string", async () => {
    expect(await verifier().verify("not-a-jwt")).toBeNull();
    expect(await verifier().verify("")).toBeNull();
  });

  it("rejects a token with the wrong issuer or audience", async () => {
    const sign = (claims: { iss: string; aud: string }) =>
      new SignJWT({ player_id: PLAYER_ID, username: "ana", role: "player" })
        .setProtectedHeader({ alg: "EdDSA" })
        .setIssuer(claims.iss)
        .setAudience(claims.aud)
        .setIssuedAt(NOW / 1000)
        .setExpirationTime(NOW / 1000 + 60)
        .sign(privateKey);

    expect(await verifier().verify(await sign({ iss: "evil", aud: TOKEN_AUDIENCE }))).toBeNull();
    expect(await verifier().verify(await sign({ iss: TOKEN_ISSUER, aud: "other" }))).toBeNull();
    expect(
      await verifier().verify(await sign({ iss: TOKEN_ISSUER, aud: TOKEN_AUDIENCE })),
    ).not.toBeNull();
  });

  it("rejects a token without player_id or with an unknown role", async () => {
    const sign = (claims: Record<string, unknown>) =>
      new SignJWT(claims)
        .setProtectedHeader({ alg: "EdDSA" })
        .setIssuer(TOKEN_ISSUER)
        .setAudience(TOKEN_AUDIENCE)
        .setIssuedAt(NOW / 1000)
        .setExpirationTime(NOW / 1000 + 60)
        .sign(privateKey);

    expect(await verifier().verify(await sign({ username: "ana", role: "player" }))).toBeNull();
    expect(
      await verifier().verify(await sign({ player_id: PLAYER_ID, username: "ana", role: "root" })),
    ).toBeNull();
  });
});
