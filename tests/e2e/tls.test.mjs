// RNF-12: nginx termina TLS en el único puerto publicado.
//
// Contra el stack que levanta run.sh: HTTP plano en ese puerto redirige a
// HTTPS (301, con la ruta y la consulta), el certificado verifica con el
// efímero de la prueba (NODE_EXTRA_CA_CERTS, nunca sin verificar), TLS 1.1 se
// rechaza y TLS 1.3 se negocia, y el healthcheck del contenedor (HTTP en el
// bucle local) lo deja `healthy`. Que /play funciona por wss: lo prueban
// smoke.test.mjs y los casos de fallos, que abren todas sus sesiones con
// WS_BASE derivado de HV_BASE_URL (https → wss).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { readFileSync } from "node:fs";
import tls from "node:tls";
import { promisify } from "node:util";

const run = promisify(execFile);
const BASE_URL = new URL(process.env.HV_BASE_URL ?? "https://localhost:8080");
const CA_FILE = process.env.NODE_EXTRA_CA_CERTS;

function handshake(options) {
  return new Promise((resolve, reject) => {
    const socket = tls.connect(
      { host: BASE_URL.hostname, port: Number(BASE_URL.port), ca: readFileSync(CA_FILE), ...options },
      () => {
        const result = { protocol: socket.getProtocol(), authorized: socket.authorized };
        socket.end();
        resolve(result);
      },
    );
    socket.on("error", reject);
  });
}

describe("TLS en nginx (RNF-12)", () => {
  it("is configured for https with a certificate the test trusts", () => {
    assert.equal(BASE_URL.protocol, "https:", "HV_BASE_URL debe ser https");
    assert.ok(CA_FILE, "falta NODE_EXTRA_CA_CERTS: tests/e2e/run.sh lo define");
  });

  it("serves the API over https with a verified certificate", async () => {
    const res = await fetch(`${BASE_URL.origin}/api/health`);
    assert.equal(res.status, 200);
  });

  it("redirects plain http on the same port to https, keeping path and query", async () => {
    const res = await fetch(`http://${BASE_URL.host}/api/health?x=1`, { redirect: "manual" });
    assert.equal(res.status, 301);
    assert.equal(res.headers.get("location"), `https://${BASE_URL.host}/api/health?x=1`);
  });

  it("negotiates TLS 1.3 (or 1.2 when capped) and rejects TLS 1.1", async () => {
    const modern = await handshake({});
    assert.equal(modern.authorized, true);
    assert.equal(modern.protocol, "TLSv1.3");
    assert.equal((await handshake({ maxVersion: "TLSv1.2" })).protocol, "TLSv1.2");
    await assert.rejects(handshake({ maxVersion: "TLSv1.1", minVersion: "TLSv1" }));
  });

  it("does not send HSTS (the lab certificate is self-signed)", async () => {
    const res = await fetch(`${BASE_URL.origin}/`);
    assert.equal(res.headers.get("strict-transport-security"), null);
  });

  it("reports the proxy container as healthy through the loopback http listener", async () => {
    const { stdout } = await run("docker", ["compose", "ps", "--format", "{{.Health}}", "nginx"]);
    assert.equal(stdout.trim(), "healthy");
  });
});
