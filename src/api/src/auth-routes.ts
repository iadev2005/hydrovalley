// Rutas de registro y acceso (RF-01, RF-02; diagrama 09). Las contraseñas y
// los hashes no se registran ni se devuelven jamás: la serialización de
// respuesta solo deja pasar los campos declarados en cada esquema.
import type { FastifyPluginAsync } from "fastify";
import {
  type AccountDirectory,
  DirectoryUnavailableError,
  InvalidAccountError,
  UsernameTakenError,
} from "./account-directory.js";
import type { PasswordService } from "./passwords.js";
import type { AttemptLimiter } from "./rate-limit.js";
import type { TokenService } from "./tokens.js";

export interface AuthDeps {
  accounts: AccountDirectory;
  passwords: PasswordService;
  tokens: TokenService;
  loginLimiter: AttemptLimiter;
}

const USERNAME_PATTERN = "^[A-Za-z0-9_.-]{3,32}$";

const MESSAGES = {
  username:
    "El nombre de usuario debe tener de 3 a 32 caracteres: letras, números, «_», «.» o «-»",
  password: "La contraseña debe tener de 8 a 128 caracteres",
  invalidBody: "La solicitud no es válida",
  taken: "El nombre de usuario ya existe",
  badCredentials: "Usuario o contraseña incorrectos",
  unavailable:
    "El servicio de cuentas no está disponible en este momento. Inténtalo de nuevo en unos minutos.",
  tooMany: "Demasiados intentos. Espera un momento antes de volver a intentarlo.",
  internal: "Error interno",
} as const;

const errorSchema = {
  type: "object",
  properties: { error: { type: "string" } },
  required: ["error"],
  additionalProperties: false,
} as const;

const registerSchema = {
  summary: "Registrar una cuenta de jugador",
  description:
    "Crea la cuenta con rol `player` y devuelve un token firmado, como el acceso: no hace falta entrar después. La contraseña se convierte en un hash Argon2id dentro de la API y nunca se guarda ni se devuelve en claro.",
  tags: ["auth"],
  body: {
    type: "object",
    properties: {
      username: {
        type: "string",
        pattern: USERNAME_PATTERN,
        description: "De 3 a 32 caracteres `A-Z a-z 0-9 _ . -`; único sin distinguir mayúsculas.",
      },
      password: { type: "string", minLength: 8, maxLength: 128 },
    },
    required: ["username", "password"],
  },
  response: {
    201: {
      description: "Cuenta creada",
      type: "object",
      properties: {
        player_id: { type: "string", format: "uuid" },
        username: { type: "string" },
        token: { type: "string" },
        expires_at: { type: "string", format: "date-time", description: "ISO 8601 en UTC" },
      },
      required: ["player_id", "username", "token", "expires_at"],
      additionalProperties: false,
    },
    400: { description: "Datos no válidos", ...errorSchema },
    409: { description: "El nombre de usuario ya existe", ...errorSchema },
    500: { description: "Error interno", ...errorSchema },
    503: { description: "La persistencia no está disponible", ...errorSchema },
  },
} as const;

const loginSchema = {
  summary: "Entrar y obtener un token firmado",
  description:
    "Devuelve un JWT firmado con Ed25519 (`alg: EdDSA`) con los campos `sub`, `player_id`, `username`, `role`, `iat` y `exp`. El mismo 401 sirve para usuario inexistente y contraseña incorrecta. Límite: 10 intentos por minuto por usuario e IP y por instancia (429).",
  tags: ["auth"],
  body: {
    type: "object",
    properties: {
      username: { type: "string", minLength: 1, maxLength: 64 },
      password: { type: "string", minLength: 1, maxLength: 128 },
    },
    required: ["username", "password"],
  },
  response: {
    200: {
      description: "Credenciales correctas",
      type: "object",
      properties: {
        token: { type: "string" },
        expires_at: { type: "string", format: "date-time", description: "ISO 8601 en UTC" },
      },
      required: ["token", "expires_at"],
      additionalProperties: false,
    },
    400: { description: "Datos no válidos", ...errorSchema },
    401: { description: "Usuario o contraseña incorrectos", ...errorSchema },
    429: { description: "Demasiados intentos", ...errorSchema },
    500: { description: "Error interno", ...errorSchema },
    503: { description: "La persistencia no está disponible", ...errorSchema },
  },
} as const;

interface Credentials {
  username: string;
  password: string;
}

/** Mensaje en español para un fallo de validación del cuerpo. */
function validationMessage(err: { validation?: readonly unknown[] }): string {
  const first = err.validation?.[0] as
    | { instancePath?: string; params?: { missingProperty?: string } }
    | undefined;
  const field = first?.params?.missingProperty ?? first?.instancePath?.replace(/^\//, "");
  if (field === "username") return MESSAGES.username;
  if (field === "password") return MESSAGES.password;
  return MESSAGES.invalidBody;
}

export const authRoutes: FastifyPluginAsync<AuthDeps> = (app, deps) => {
  app.setErrorHandler((err: Error & { validation?: readonly unknown[]; statusCode?: number }, req, reply) => {
    if (err.validation) {
      return reply.code(400).send({ error: validationMessage(err) });
    }
    if (err instanceof UsernameTakenError) return reply.code(409).send({ error: MESSAGES.taken });
    if (err instanceof InvalidAccountError) {
      return reply.code(400).send({ error: MESSAGES.invalidBody });
    }
    if (err instanceof DirectoryUnavailableError) {
      req.log.warn({ err: err.message }, "account directory unavailable");
      return reply.code(503).send({ error: MESSAGES.unavailable });
    }
    const status = err.statusCode;
    if (status !== undefined && status >= 400 && status < 500) {
      return reply.code(status).send({ error: MESSAGES.invalidBody });
    }
    req.log.error({ err }, "unexpected error");
    return reply.code(500).send({ error: MESSAGES.internal });
  });

  app.post<{ Body: Credentials }>("/api/auth/register", { schema: registerSchema }, async (req, reply) => {
    const { username, password } = req.body;
    const passwordHash = await deps.passwords.hash(password);
    const account = await deps.accounts.register(username, passwordHash);
    // Sin GetAccount: el rol lo devuelve Register y el nombre es el recién
    // registrado.
    const { token, expiresAt } = await deps.tokens.issue({
      playerId: account.playerId,
      username,
      role: account.role,
    });
    return reply.code(201).send({
      player_id: account.playerId,
      username,
      token,
      expires_at: expiresAt.toISOString(),
    });
  });

  app.post<{ Body: Credentials }>("/api/auth/login", { schema: loginSchema }, async (req, reply) => {
    const { username, password } = req.body;

    const attempt = deps.loginLimiter.hit(`${username.toLowerCase()}|${req.ip}`);
    if (!attempt.allowed) {
      return reply
        .code(429)
        .header("Retry-After", String(attempt.retryAfterSeconds))
        .send({ error: MESSAGES.tooMany });
    }

    const account = await deps.accounts.getAccount(username);
    // Si la cuenta no existe se verifica contra el hash de relleno: cuesta lo
    // mismo y la respuesta no revela si el usuario existe.
    const matches = await deps.passwords.verify(
      account?.passwordHash ?? deps.passwords.dummyHash,
      password,
    );
    if (!account || !matches) {
      return reply.code(401).send({ error: MESSAGES.badCredentials });
    }

    // GetAccount no devuelve el nombre con su grafía de registro y el token
    // lo lleva (el chat lo usa): se pide a GetUsernames, y si no aparece se
    // usa el que escribió el jugador.
    const names = await deps.accounts.getUsernames([account.playerId]);
    const { token, expiresAt } = await deps.tokens.issue({
      playerId: account.playerId,
      username: names.get(account.playerId) ?? username,
      role: account.role,
    });
    return reply.send({ token, expires_at: expiresAt.toISOString() });
  });

  return Promise.resolve();
};
