// CP-09: Dos jugadores envían a la vez y ven el mismo orden (RF-13, RF-14).
//
// Invariantes verificadas:
// 1. Reloj lógico estrictamente creciente (`seq` uint64 secuencial sin saltos).
// 2. Orden idéntico de mensajes recibido por ambos jugadores concurrentes.
// 3. Extracción de identidad (`username`) garantizada desde el token JWT.
// 4. Idempotencia en SendMessage mediante `message_id` (reenviar no genera duplicados ni avanza `seq`).
// 5. Sincronización post-reconexión: frame `sync` entrega mensajes perdidos y culmina con `synced`.
import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { ChatSession } from "./chat-session.mjs";
import {
  WS_BASE,
  eventually,
  loginAdmin,
  provisionPlayers,
  sleep,
  waitForHealthyStack,
} from "./support.mjs";

describe("chat ordering and synchronization (CP-09 / RF-13 / RF-14)", () => {
  let adminToken;
  let accounts;
  const openSessions = [];

  before(async () => {
    await waitForHealthyStack();
    adminToken = await loginAdmin();
    accounts = await provisionPlayers(adminToken, 2, "cp09");
  });

  after(() => {
    for (const session of openSessions) {
      try {
        session.close();
      } catch {}
    }
  });

  it(
    "ordena concurrentemente mensajes simultáneos de dos jugadores en el mismo orden exacto",
    { timeout: 60_000 },
    async () => {
      const s1 = new ChatSession(`${WS_BASE}/chat?token=${accounts[0].token}`);
      const s2 = new ChatSession(`${WS_BASE}/chat?token=${accounts[1].token}`);
      openSessions.push(s1, s2);

      await Promise.all([s1.opened, s2.opened]);

      const p1Ids = [];
      const p2Ids = [];
      const BURST_PER_PLAYER = 5;

      for (let i = 0; i < BURST_PER_PLAYER; i++) {
        p1Ids.push(randomUUID());
        p2Ids.push(randomUUID());
      }

      const allIds = new Set([...p1Ids, ...p2Ids]);

      // Enviar ráfagas simultáneas desde ambos jugadores
      await Promise.all([
        ...p1Ids.map((id, i) => {
          s1.sendMessage(`msg-p1-${i}-${id.slice(0, 4)}`, id);
        }),
        ...p2Ids.map((id, i) => {
          s2.sendMessage(`msg-p2-${i}-${id.slice(0, 4)}`, id);
        }),
      ]);

      // Esperar a que ambos jugadores reciban todos los 10 mensajes
      await eventually(
        "both players to receive all burst messages",
        20_000,
        () => {
          const s1Msgs = s1.frames.filter((f) => f.type === "message" && allIds.has(f.message_id));
          const s2Msgs = s2.frames.filter((f) => f.type === "message" && allIds.has(f.message_id));
          return s1Msgs.length === BURST_PER_PLAYER * 2 && s2Msgs.length === BURST_PER_PLAYER * 2;
        },
        500,
      );

      const s1Msgs = s1.frames.filter((f) => f.type === "message" && allIds.has(f.message_id));
      const s2Msgs = s2.frames.filter((f) => f.type === "message" && allIds.has(f.message_id));

      const s1Order = s1Msgs.map((f) => f.message_id);
      const s2Order = s2Msgs.map((f) => f.message_id);

      // CP-09: Orden idéntico en ambas pantallas
      assert.deepEqual(s1Order, s2Order, "Ambos jugadores deben observar exactamente el mismo orden de mensajes");

      // Invariante de secuencia monótona creciente
      const seqs = s1Msgs.map((f) => BigInt(f.seq));
      for (let i = 1; i < seqs.length; i++) {
        assert.equal(seqs[i], seqs[i - 1] + 1n, `Secuencia debe ser estrictamente consecutiva: ${seqs[i - 1]} -> ${seqs[i]}`);
      }

      // Validar mapeo de usuario desde el token JWT
      for (const msg of s1Msgs) {
        if (p1Ids.includes(msg.message_id)) {
          assert.equal(msg.username, accounts[0].username, "El remitente debe coincidir con la cuenta del token");
        } else {
          assert.equal(msg.username, accounts[1].username, "El remitente debe coincidir con la cuenta del token");
        }
      }

      // Invariante de Idempotencia: reenviar un message_id existente
      const existingId = p1Ids[0];
      const initialCount = s1.frames.filter((f) => f.type === "message").length;
      s1.sendMessage("intento-duplicado", existingId);

      await sleep(1500);

      const afterCount = s1.frames.filter((f) => f.type === "message").length;
      assert.equal(afterCount, initialCount, "El reenvío idempotente no debe generar un nuevo mensaje");

      // Sincronización post-reconexión: solicitar mensajes con after_seq
      const lastSeq = seqs[seqs.length - 1];
      const afterSeq = lastSeq - 3n;

      const s3 = new ChatSession(`${WS_BASE}/chat?token=${accounts[0].token}`);
      openSessions.push(s3);
      await s3.opened;

      s3.sync(Number(afterSeq));

      const syncedFrame = await s3.first("synced frame", (f) => f.type === "synced", 10_000);
      assert.ok(syncedFrame.server_epoch, "Frame synced debe incluir server_epoch");

      const s3Msgs = s3.frames.filter((f) => f.type === "message");
      assert.equal(s3Msgs.length, 3, "Deben entregarse exactamente los últimos 3 mensajes");
      assert.equal(BigInt(s3Msgs[0].seq), afterSeq + 1n);
      assert.equal(BigInt(s3Msgs[2].seq), lastSeq);
    },
  );
});
