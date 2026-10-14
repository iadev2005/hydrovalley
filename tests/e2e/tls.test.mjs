// RNF-12: nginx termina TLS en el único puerto publicado.
//
// Contra el stack que levanta run.sh: HTTP plano en ese puerto redirige a
// HTTPS (308, que conserva el método de un POST, con la ruta y la consulta),
// el certificado verifica con el efímero de la prueba (NODE_EXTRA_CA_CERTS,
// nunca sin verificar), TLS 1.1 lo rechaza el servidor (se prueba con
// `openssl s_client`, porque Node ya se niega a hablarlo por su cuenta y la
// aserción no demostraría nada) y TLS 1.3 se negocia, y el healthcheck del
// contenedor (HTTP en el bucle local) lo deja `healthy`. Aparte, un nginx
// suelto sin certificado montado ejercita el certificado autogenerado. Que /play funciona por wss: lo prueban
// smoke.test.mjs y los casos de fallos, que abren todas sus sesiones con
// WS_BASE derivado de HV_BASE_URL (https → wss).
import { after, describe, it } from "node:test";
import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
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

// Ejecuta openssl con una entrada estándar vacía y devuelve su salida; no
// falla si openssl termina con error (el rechazo es justo lo que se mide).
function openssl(args, input = "") {
  return new Promise((resolve, reject) => {
    const child = spawn("openssl", args, { stdio: ["pipe", "pipe", "pipe"] });
    let out = "";
    child.stdout.on("data", (d) => (out += d));
    child.stderr.on("data", (d) => (out += d));
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, out }));
    child.stdin.end(input);
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
    // 308 y no 301: un POST a http:// sigue siendo POST tras la redirección.
    assert.equal(res.status, 308);
    assert.equal(res.headers.get("location"), `https://${BASE_URL.host}/api/health?x=1`);
  });

  it("negotiates TLS 1.3 (or 1.2 when capped)", async () => {
    const modern = await handshake({});
    assert.equal(modern.authorized, true);
    assert.equal(modern.protocol, "TLSv1.3");
    assert.equal((await handshake({ maxVersion: "TLSv1.2" })).protocol, "TLSv1.2");
  });

  it("rejects TLS 1.1 on the server side (openssl, not Node's own refusal)", async () => {
    const target = `${BASE_URL.hostname}:${BASE_URL.port}`;
    // Control: el mismo cliente con TLS 1.2 conecta, así que el fallo de TLS 1.1
    // no es de red ni del cliente.
    const ok = await openssl(["s_client", "-tls1_2", "-connect", target, "-CAfile", CA_FILE], "");
    assert.match(ok.out, /Protocol\s*:\s*TLSv1\.2/);
    // SECLEVEL=0 permite que el cliente ofrezca TLS 1.1; lo que lo rechaza es
    // la alerta del servidor (protocol version), no una negativa local.
    const old = await openssl(
      ["s_client", "-tls1_1", "-cipher", "DEFAULT:@SECLEVEL=0", "-connect", target, "-CAfile", CA_FILE],
      "",
    );
    assert.match(old.out, /tlsv1 alert protocol version/i);
    assert.match(old.out, /no peer certificate available/i);
  });

  it("does not send HSTS (the lab certificate is self-signed)", async () => {
    const res = await fetch(`${BASE_URL.origin}/`);
    assert.equal(res.headers.get("strict-transport-security"), null);
  });

  it("reports the proxy container as healthy through the loopback http listener", async () => {
    const { stdout } = await run("docker", ["compose", "ps", "--format", "{{.Health}}", "nginx"]);
    assert.equal(stdout.trim(), "healthy");
  });});

// Certificado autogenerado (docker-entrypoint.d/40-tls-cert.sh): el stack de
// run.sh monta uno propio, así que este camino no se ejercita ahí. Aquí se
// arranca la misma imagen sola, sin certificado montado, con TLS_EXTRA_SANS.
describe("certificado autofirmado de nginx (sin certificado montado)", () => {
  const name = `hv-e2e-nginx-selfsigned-${process.pid}`;
  const extraSans = "lab.example.test,10.68.17.9";

  after(async () => {
    await run("docker", ["rm", "-f", name]).catch(() => {});
  });

  it("becomes healthy and serves a certificate with the fixed and extra SANs", async () => {
    const { stdout: id } = await run("docker", ["compose", "ps", "-q", "nginx"]);
    const { stdout: image } = await run("docker", ["inspect", "--format", "{{.Image}}", id.trim()]);
    await run("docker", [
      "run", "-d", "--name", name, "-p", "127.0.0.1::8080",
      "-e", `TLS_EXTRA_SANS=${extraSans}`, image.trim(),
    ]);

    let health = "";
    for (let i = 0; i < 30 && health !== "healthy"; i++) {
      ({ stdout: health } = await run("docker", ["inspect", "--format", "{{.State.Health.Status}}", name]));
      health = health.trim();
      if (health !== "healthy") await new Promise((r) => setTimeout(r, 500));
    }
    assert.equal(health, "healthy");

    const { stdout: mapping } = await run("docker", ["port", name, "8080/tcp"]);
    const port = mapping.trim().split("\n")[0].split(":").pop();
    const { out: handshakeOut } = await openssl(
      ["s_client", "-connect", `127.0.0.1:${port}`, "-servername", "localhost"],
      "",
    );
    const pem = handshakeOut.match(/-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----/)?.[0];
    assert.ok(pem, "el servidor no presentó certificado");
    const { out: sans } = await openssl(["x509", "-noout", "-ext", "subjectAltName"], pem);
    for (const expected of ["DNS:localhost", "DNS:nginx", "IP Address:127.0.0.1", "DNS:lab.example.test", "IP Address:10.68.17.9"]) {
      assert.ok(sans.includes(expected), `falta ${expected} en el SAN: ${sans}`);
    }
  });
});
