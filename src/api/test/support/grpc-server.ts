import { createServer, type Http2Server, type ServerHttp2Session } from "node:http2";
import type { AddressInfo } from "node:net";
import type { DescService } from "@bufbuild/protobuf";
import type { ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";

/**
 * Servidor gRPC en proceso para las pruebas de los adaptadores. `stop()`
 * destruye primero las sesiones HTTP/2: `close()` espera a que terminen y el
 * cliente las mantiene vivas, así que sin eso la limpieza se cuelga en Linux.
 */
export class TestGrpcServer {
  private server: Http2Server | undefined;
  private readonly sessions = new Set<ServerHttp2Session>();

  async start<S extends DescService>(
    service: S,
    handlers: Partial<ServiceImpl<S>>,
  ): Promise<string> {
    const created = createServer(
      connectNodeAdapter({
        grpc: true,
        routes: (router) => {
          router.service(service, handlers);
        },
      }),
    );
    created.on("session", (session) => {
      this.sessions.add(session);
      session.on("close", () => this.sessions.delete(session));
    });
    this.server = created;
    await new Promise<void>((resolve) => created.listen(0, "127.0.0.1", resolve));
    return `127.0.0.1:${String((created.address() as AddressInfo).port)}`;
  }

  async stop(): Promise<void> {
    const s = this.server;
    this.server = undefined;
    if (!s) return;
    for (const session of this.sessions) session.destroy();
    this.sessions.clear();
    await new Promise<void>((resolve) => {
      s.close(() => {
        resolve();
      });
    });
  }
}
