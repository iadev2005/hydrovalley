// Rutas de registro y acceso (RF-01, RF-02; diagrama 09). Las contraseñas y
// los hashes no se registran ni se devuelven jamás: la serialización de
// respuesta solo deja pasar los campos declarados en cada esquema.
import type { FastifyPluginAsync, FastifyReply } from "fastify";
import {
  type AccountDirectory,
  DirectoryUnavailableError,
  InvalidAccountError,
  UsernameTakenError,
} from "./account-directory.js";
import {
  describeWindow,
  LOGIN_LIMIT_PER_IP,
  LOGIN_LIMIT_PER_USER_IP,
  REGISTER_LIMIT_PER_IP,
} from "./limits.js";
import type { PasswordService } from "./passwords.js";
import type { AttemptLimiter } from "./rate-limit.js";
import type { TokenService } from "./tokens.js";

export interface AuthDeps {
  accounts: AccountDirectory;
  passwords: PasswordService;
  tokens: TokenService;
  /** Intentos de acceso por usuario e IP. */
  loginLimiter: AttemptLimiter;
  /** Intentos de acceso por IP, entre todos los usuarios. */
  loginIpLimiter: AttemptLimiter;
  /** Registros por IP. */
  registerIpLimiter: AttemptLimiter;
}

const USERNAME_PATTERN = "^[A-Za-z0-9_.-]{3,32}$";

const MESSAGES = {
  username: "El nombre de usuario debe tener de 3 a 32 caracteres: letras, números, «_», «.» o «-»",
  password: "La contraseña debe tener de 8 a 128 caracteres",
  invalidBody: "La solicitud no es válida",
  loginRequired: "El usuario y la contraseña son obligatorios",
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
  description: `Crea la cuenta con rol \`player\` y devuelve un token firmado, como el acceso: no hace falta entrar después. La contraseña se convierte en un hash Argon2id dentro de la API y nunca se guarda ni se devuelve en claro. Límite por instancia (429): ${String(REGISTER_LIMIT_PER_IP.limit)} registros ${describeWindow(REGISTER_LIMIT_PER_IP.windowMs)} por IP.`,
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
  description: `Devuelve un JWT firmado con Ed25519 (\`alg: EdDSA\`) con los campos \`sub\`, \`player_id\`, \`username\`, \`role\`, \`iat\` y \`exp\`. El mismo 401 sirve para usuario inexistente y contraseña incorrecta. Límites por instancia (429): ${String(LOGIN_LIMIT_PER_USER_IP.limit)} intentos ${describeWindow(LOGIN_LIMIT_PER_USER_IP.windowMs)} por usuario e IP y ${String(LOGIN_LIMIT_PER_IP.limit)} ${describeWindow(LOGIN_LIMIT_PER_IP.windowMs)} por IP entre todos los usuarios.`,
  tags: ["auth"],
  body: {
    type: "object",
    properties: {
      // Más laxo que en el registro a propósito: un nombre que nunca pudo
      // registrarse (símbolos, muy corto) llega hasta GetAccount y recibe el
      // mismo 401 que uno inexistente, en vez de un 400 que delataría las
      // reglas de nombres. El tope de 64 solo acota el tamaño de la clave del
      // limitador.
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
function validationMessage(err: { validation?: readonly unknown[] }, isLogin: boolean): string {
  if (isLogin) return MESSAGES.loginRequired;
  const first = err.validation?.[0] as
    { instancePath?: string; params?: { missingProperty?: string } } | undefined;
  const field = first?.params?.missingProperty ?? first?.instancePath?.replace(/^\//, "");
  if (field === "username") return MESSAGES.username;
  if (field === "password") return MESSAGES.password;
  return MESSAGES.invalidBody;
}

function tooMany(reply: FastifyReply, retryAfterSeconds: number) {
  return reply
    .code(429)
    .header("Retry-After", String(retryAfterSeconds))
    .send({ error: MESSAGES.tooMany });
}

export const authRoutes: FastifyPluginAsync<AuthDeps> = (app, deps) => {
  app.setErrorHandler(
    (err: Error & { validation?: readonly unknown[]; statusCode?: number }, req, reply) => {
      if (err.validation) {
        return reply
          .code(400)
          .send({ error: validationMessage(err, req.routeOptions.url === "/api/auth/login") });
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
    },
  );

  app.post<{ Body: Credentials }>(
    "/api/auth/register",
    { schema: registerSchema },
    async (req, reply) => {
      const { username, password } = req.body;
      const attempt = deps.registerIpLimiter.hit(req.ip);
      if (!attempt.allowed) return tooMany(reply, attempt.retryAfterSeconds);

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
    },
  );

  app.post<{ Body: Credentials }>(
    "/api/auth/login",
    { schema: loginSchema },
    async (req, reply) => {
      const { username, password } = req.body;

      // Primero el límite por IP: si niega, no se toca el de usuario e IP, para
    // que una sola IP no llene el limitador con claves nuevas y bloquee a los
    // demás (el limitador falla cerrado cuando se llena).
    const byIp = deps.loginIpLimiter.hit(req.ip);
    if (!byIp.allowed) return tooMany(reply, byIp.retryAfterSeconds);
    const byUser = deps.loginLimiter.hit(`${username.toLowerCase()}|${req.ip}`);
    if (!byUser.allowed) return tooMany(reply, byUser.retryAfterSeconds);

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
      // Si esa consulta falla, la contraseña ya era correcta: no se rechaza el
      // acceso por un detalle cosmético.
      let canonical: string | undefined;
      try {
        canonical = (await deps.accounts.getUsernames([account.playerId])).get(account.playerId);
      } catch (err) {
        req.log.warn({ err: String(err) }, "username lookup failed; using the typed username");
      }
      const { token, expiresAt } = await deps.tokens.issue({
        playerId: account.playerId,
        username: canonical ?? username,
        role: account.role,
      });
      return reply.send({ token, expires_at: expiresAt.toISOString() });
    },
  );

  return Promise.resolve();
};
