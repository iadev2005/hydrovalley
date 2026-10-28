import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const envPath = path.resolve(__dirname, "../deploy/.env");

if (fs.existsSync(envPath)) {
  const existing = fs.readFileSync(envPath, "utf8");
  if (existing.includes("TOKEN_PRIVATE_KEY=") && existing.includes("TOKEN_PUBLIC_KEY=")) {
    console.log(`El archivo deploy/.env ya existe y cuenta con claves configuradas en: ${envPath}`);
    process.exit(0);
  }
  // Generar par de claves Ed25519 y añadirlas
  const { privateKey, publicKey } = crypto.generateKeyPairSync("ed25519");
  const privB64 = Buffer.from(privateKey.export({ type: "pkcs8", format: "pem" })).toString("base64");
  const pubB64 = Buffer.from(publicKey.export({ type: "spki", format: "pem" })).toString("base64");
  const updated = existing.trimEnd() + `\n\n# Claves Ed25519 para tokens JWT/auth\nTOKEN_PRIVATE_KEY=${privB64}\nTOKEN_PUBLIC_KEY=${pubB64}\n`;
  fs.writeFileSync(envPath, updated, "utf8");
  console.log(`✓ Claves Ed25519 agregadas exitosamente a: ${envPath}`);
  process.exit(0);
}

// Generar par de claves Ed25519
const { privateKey, publicKey } = crypto.generateKeyPairSync("ed25519");
const privPem = privateKey.export({ type: "pkcs8", format: "pem" });
const pubPem = publicKey.export({ type: "spki", format: "pem" });

const privB64 = Buffer.from(privPem).toString("base64");
const pubB64 = Buffer.from(pubPem).toString("base64");

const envContent = `# Configuración local generada para Docker Compose (deploy/compose.yml)
POSTGRES_USER=hydrovalley
POSTGRES_PASSWORD=hydrovalley_dev_password
POSTGRES_DB=hydrovalley

HV_HTTP_PORT=8080
LOG_LEVEL=info

# Claves Ed25519 para tokens JWT/auth
TOKEN_PRIVATE_KEY=${privB64}
TOKEN_PUBLIC_KEY=${pubB64}
`;

fs.writeFileSync(envPath, envContent, "utf8");
console.log(`✓ Archivo deploy/.env creado exitosamente con nuevas claves Ed25519 en: ${envPath}`);
