import { generateKeyPairSync } from "node:crypto";

/** Par Ed25519 de pruebas: PEM PKCS#8 de la privada y objeto de la pública. */
export function makeKeyPair() {
  const { privateKey, publicKey } = generateKeyPairSync("ed25519");
  const pem = privateKey.export({ type: "pkcs8", format: "pem" }).toString();
  return { privateKey, publicKey, pem };
}
