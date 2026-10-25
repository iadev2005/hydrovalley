// Cliente de `grpc.health.v1.Health/Check` sin código generado: el contrato
// es el estándar de gRPC y no vive en `proto/`, así que se describe a mano
// (solo `Check`; nunca `Watch`, regla 4: sin streams). Lo usa el panel del
// sistema para saber si el simulador y la persistencia atienden.
import { create, createFileRegistry, type Message } from "@bufbuild/protobuf";
import type { GenMessage, GenService } from "@bufbuild/protobuf/codegenv2";
import {
  FieldDescriptorProto_Label,
  FieldDescriptorProto_Type,
  FileDescriptorProtoSchema,
} from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createGrpcTransportFor } from "./request-id.js";

/** Valores de `HealthCheckResponse.ServingStatus`. */
export const ServingStatus = {
  UNKNOWN: 0,
  SERVING: 1,
  NOT_SERVING: 2,
  SERVICE_UNKNOWN: 3,
} as const;

const file = create(FileDescriptorProtoSchema, {
  name: "grpc/health/v1/health.proto",
  package: "grpc.health.v1",
  syntax: "proto3",
  messageType: [
    {
      name: "HealthCheckRequest",
      field: [
        {
          name: "service",
          number: 1,
          label: FieldDescriptorProto_Label.OPTIONAL,
          type: FieldDescriptorProto_Type.STRING,
          jsonName: "service",
        },
      ],
    },
    {
      name: "HealthCheckResponse",
      field: [
        {
          name: "status",
          number: 1,
          label: FieldDescriptorProto_Label.OPTIONAL,
          type: FieldDescriptorProto_Type.ENUM,
          typeName: ".grpc.health.v1.HealthCheckResponse.ServingStatus",
          jsonName: "status",
        },
      ],
      enumType: [
        {
          name: "ServingStatus",
          value: [
            { name: "UNKNOWN", number: 0 },
            { name: "SERVING", number: 1 },
            { name: "NOT_SERVING", number: 2 },
            { name: "SERVICE_UNKNOWN", number: 3 },
          ],
        },
      ],
    },
  ],
  service: [
    {
      name: "Health",
      method: [
        {
          name: "Check",
          inputType: ".grpc.health.v1.HealthCheckRequest",
          outputType: ".grpc.health.v1.HealthCheckResponse",
        },
      ],
    },
  ],
});

interface HealthCheckRequest extends Message<"grpc.health.v1.HealthCheckRequest"> {
  service: string;
}
interface HealthCheckResponse extends Message<"grpc.health.v1.HealthCheckResponse"> {
  status: number;
}
type HealthService = GenService<{
  check: {
    methodKind: "unary";
    input: GenMessage<HealthCheckRequest>;
    output: GenMessage<HealthCheckResponse>;
  };
}>;

const registry = createFileRegistry(file, () => undefined);
const described = registry.getService("grpc.health.v1.Health");
if (!described) throw new Error("grpc.health.v1.Health is not defined");

/** Servicio `grpc.health.v1.Health` (solo `Check`). */
export const Health = described as unknown as HealthService;

/** Resultado de una sonda: arriba, o abajo con un motivo corto y seguro. */
export type ProbeResult =
  { readonly status: "up" } | { readonly status: "down"; readonly detail: string };

export interface GrpcHealthOptions {
  /** `host:puerto` del servicio. */
  address: string;
  /** Nombre que se consulta; `""` es el estado general del servidor. */
  service: string;
  /** Plazo de la llamada en milisegundos (1 s por defecto); sin reintentos. */
  timeoutMs?: number;
}

const DEFAULT_TIMEOUT_MS = 1000;

/**
 * Crea una sonda que hace un `Check` con plazo y sin reintento. Nunca
 * rechaza: cualquier fallo sale como `down` con un motivo (`timeout`,
 * `unavailable`, `not_serving` o `error`) que no incluye mensajes ni
 * direcciones.
 */
export function createGrpcHealthProbe(options: GrpcHealthOptions): () => Promise<ProbeResult> {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const addresses = options.address
    .split(",")
    .map((a) => a.trim())
    .filter((a) => a !== "");
  const clients = (addresses.length > 0 ? addresses : [options.address]).map((addr) =>
    createClient(Health, createGrpcTransportFor(addr)),
  );
  let active = 0;

  return async () => {
    let lastDown: ProbeResult = { status: "down", detail: "unavailable" };
    for (let i = 0; i < clients.length; i++) {
      const idx = (active + i) % clients.length;
      const client = clients[idx];
      if (!client) continue;
      try {
        const res = await client.check({ service: options.service }, { timeoutMs });
        if (res.status === ServingStatus.SERVING) {
          active = idx;
          return { status: "up" };
        }
        lastDown = { status: "down", detail: "not_serving" };
      } catch (err) {
        lastDown = { status: "down", detail: failureDetail(err) };
      }
    }
    return lastDown;
  };
}

function failureDetail(err: unknown): string {
  const code = ConnectError.from(err).code;
  if (code === Code.DeadlineExceeded) return "timeout";
  if (code === Code.Unavailable) return "unavailable";
  return "error";
}
