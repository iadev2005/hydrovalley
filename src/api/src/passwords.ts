// Argon2id con los parámetros mínimos recomendados por OWASP (Password
// Storage Cheat Sheet): memoria 19 MiB (19456 KiB), 2 iteraciones y 1 hilo.
// El hash sale en formato PHC ($argon2id$v=19$m=19456,t=2,p=1$...), que es
// lo único que acepta la persistencia (RNF-12).
import { randomBytes } from "node:crypto";
import { type Algorithm, hash as argonHash, verify as argonVerify } from "@node-rs/argon2";

export interface PasswordService {
  hash(password: string): Promise<string>;
  /** `false` si la contraseña no coincide o si el hash guardado no es válido. */
  verify(hash: string, password: string): Promise<boolean>;
  /**
   * Hash con los mismos parámetros, calculado una vez al arrancar. El acceso
   * de un usuario inexistente se verifica contra él para que el tiempo de
   * respuesta no delate si la cuenta existe.
   */
  readonly dummyHash: string;
}

// `Algorithm` es un `const enum` ambiental: con isolatedModules no se puede
// usar por nombre, así que se pasa su valor (Argon2id = 2).
const ARGON2ID = 2 as Algorithm.Argon2id;
const PARAMS = { algorithm: ARGON2ID, memoryCost: 19456, timeCost: 2, parallelism: 1 } as const;

export async function createPasswordService(): Promise<PasswordService> {
  const dummyHash = await argonHash(randomBytes(32).toString("hex"), PARAMS);
  return {
    dummyHash,
    hash: (password) => argonHash(password, PARAMS),
    async verify(hash, password) {
      try {
        return await argonVerify(hash, password);
      } catch {
        return false;
      }
    },
  };
}
