// Token de acceso: JWT firmado con Ed25519 (alg EdDSA). Solo la API tiene la
// clave privada; el hub verifica con la pública (diagrama 09).
import type { KeyObject } from "node:crypto";
import { SignJWT } from "jose";
import type { AccountRole } from "./account-directory.js";

export interface TokenClaims {
  playerId: string;
  username: string;
  role: AccountRole;
}

export interface TokenService {
  issue(claims: TokenClaims): Promise<{ token: string; expiresAt: Date }>;
}

export interface TokenServiceOptions {
  privateKey: KeyObject;
  ttlSeconds: number;
  /** Identificador de clave para la cabecera (`kid`); opcional. */
  keyId?: string | undefined;
  /** Reloj en milisegundos, inyectable para las pruebas. */
  now?: () => number;
}

export function createTokenService(options: TokenServiceOptions): TokenService {
  if (options.privateKey.asymmetricKeyType !== "ed25519") {
    throw new Error("the token signing key must be an Ed25519 private key");
  }
  const now = options.now ?? Date.now;
  return {
    async issue({ playerId, username, role }) {
      const iat = Math.floor(now() / 1000);
      const exp = iat + options.ttlSeconds;
      const header = {
        alg: "EdDSA",
        typ: "JWT",
        ...(options.keyId === undefined ? {} : { kid: options.keyId }),
      };
      const token = await new SignJWT({ player_id: playerId, username, role })
        .setProtectedHeader(header)
        .setSubject(playerId)
        .setIssuedAt(iat)
        .setExpirationTime(exp)
        .sign(options.privateKey);
      return { token, expiresAt: new Date(exp * 1000) };
    },
  };
}
