// Cliente gRPC de AccountService (HTTP/2 sin cifrar dentro de la red del
// laboratorio). Política de CLAUDE.md: plazo de 2 s por llamada; GetAccount
// y GetUsernames, idempotentes, se reintentan hasta 3 veces con esperas de
// 100, 200 y 400 ms (más un poco de azar) dentro de un presupuesto total de
// 3 s; Register nunca se reintenta.
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import {
  type AccountDirectory,
  type AccountRole,
  DirectoryUnavailableError,
  InvalidAccountError,
  UsernameTakenError,
} from "./account-directory.js";
import { AccountService, Role } from "./gen/hydrovalley/persistence/v1/account_pb.js";
import { createGrpcCaller, type GrpcCallOptions, TRANSIENT_CODES } from "./grpc-retry.js";

export type GrpcAccountDirectoryOptions = GrpcCallOptions;

function roleOf(role: Role): AccountRole {
  return role === Role.ADMIN ? "admin" : "player";
}

export function createGrpcAccountDirectory(options: GrpcAccountDirectoryOptions): AccountDirectory {
  const { idempotent, timeoutMs } = createGrpcCaller(options);
  const client = createClient(
    AccountService,
    createGrpcTransport({ baseUrl: `http://${options.address}` }),
  );

  /** Traduce los errores de gRPC que la API distingue; el resto se propaga. */
  function translate(err: unknown): Error {
    const error = ConnectError.from(err);
    if (TRANSIENT_CODES.has(error.code)) {
      return new DirectoryUnavailableError(`persistence call failed: ${Code[error.code]}`);
    }
    if (error.code === Code.AlreadyExists) return new UsernameTakenError();
    if (error.code === Code.InvalidArgument) return new InvalidAccountError();
    return error;
  }

  return {
    async register(username, passwordHash) {
      try {
        const res = await client.register({ username, passwordHash }, { timeoutMs });
        return { playerId: res.accountId, role: roleOf(res.role) };
      } catch (err) {
        throw translate(err);
      }
    },

    async getAccount(username) {
      try {
        const res = await idempotent((timeout) =>
          client.getAccount({ username }, { timeoutMs: timeout }),
        );
        return {
          playerId: res.accountId,
          passwordHash: res.passwordHash,
          role: roleOf(res.role),
        };
      } catch (err) {
        if (ConnectError.from(err).code === Code.NotFound) return null;
        throw translate(err);
      }
    },

    async getUsernames(playerIds) {
      try {
        const res = await idempotent((timeout) =>
          client.getUsernames({ playerIds: [...playerIds] }, { timeoutMs: timeout }),
        );
        return new Map(Object.entries(res.usernames));
      } catch (err) {
        throw translate(err);
      }
    },
  };
}
