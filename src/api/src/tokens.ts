// Token de acceso: JWT firmado con Ed25519 (alg EdDSA). Solo la API tiene la
// clave privada; el hub verifica con la pública (diagrama 09).
import type { KeyObject } from "node:crypto";
import { jwtVerify, SignJWT } from "jose";
import type { AccountRole } from "./account-directory.js";

/** Emisor (`iss`) de los tokens; el hub lo exige al verificar. */
export const TOKEN_ISSUER = "hydrovalley-api";
/** Audiencia (`aud`) de los tokens: el hub (`/play`); el hub la exige al verificar. */
export const TOKEN_AUDIENCE = "hydrovalley-play";

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
        .setIssuer(TOKEN_ISSUER)
        .setAudience(TOKEN_AUDIENCE)
        .setSubject(playerId)
        .setIssuedAt(iat)
        .setExpirationTime(exp)
        .sign(options.privateKey);
      return { token, expiresAt: new Date(exp * 1000) };
    },
  };
}

/** Datos del jugador que lleva un token válido. */
export interface VerifiedToken {
  playerId: string;
  username: string;
  role: AccountRole;
}

export interface TokenVerifier {
  /** `null` si el token no es válido: firma, `iss`, `aud`, `exp` o campos. */
  verify(token: string): Promise<VerifiedToken | null>;
}

export interface TokenVerifierOptions {
  /** Clave pública Ed25519; se deriva de la privada con `createPublicKey`. */
  publicKey: KeyObject;
  /** Reloj en milisegundos, inyectable para las pruebas. */
  now?: () => number;
}

/**
 * Verifica como el hub: solo EdDSA, con `iss` y `aud` exigidos y `exp`
 * comprobado. No distingue el motivo del rechazo: a la ruta le basta con el
 * 401.
 */
export function createTokenVerifier(options: TokenVerifierOptions): TokenVerifier {
  const now = options.now ?? Date.now;
  return {
    async verify(token) {
      try {
        const { payload } = await jwtVerify(token, options.publicKey, {
          algorithms: ["EdDSA"],
          issuer: TOKEN_ISSUER,
          audience: TOKEN_AUDIENCE,
          currentDate: new Date(now()),
          requiredClaims: ["exp"],
        });
        const { player_id: playerId, username, role } = payload;
        if (typeof playerId !== "string" || playerId === "") return null;
        if (typeof username !== "string") return null;
        if (role !== "player" && role !== "admin") return null;
        return { playerId, username, role };
      } catch {
        return null;
      }
    },
  };
}
