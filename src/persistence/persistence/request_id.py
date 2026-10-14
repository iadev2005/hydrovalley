"""Identificador de petición de punta a punta (RNF-07).

La API (y el simulador, en sus propias llamadas) envían el id como metadato
gRPC `x-request-id`. Un interceptor del servidor lo lee, lo deja en una
variable de contexto durante la llamada y registra una línea por llamada; un
filtro de logging lo añade a todas las líneas de esa llamada como
`request_id` (`-` si no viene o no es válido). Nunca se registra el contenido
de las peticiones.
"""

import contextvars
import logging
import re
import time

import grpc

REQUEST_ID_KEY = "x-request-id"
NO_REQUEST_ID = "-"

# UUID, 32 hexadecimales (el id de nginx), «tick-<n>» (el Step del hub) o
# «world-<uuid>» (las llamadas del simulador): letras, dígitos y guion, 64
# como máximo. Cualquier otra cosa (saltos de línea, espacios) se descarta.
_SAFE = re.compile(r"[A-Za-z0-9-]{1,64}")

_request_id: contextvars.ContextVar[str] = contextvars.ContextVar(
    "request_id", default=NO_REQUEST_ID
)

log = logging.getLogger("persistence.request")
_HEALTH_PREFIX = "/grpc.health.v1.Health/"


def sanitize(value: object) -> str:
    """El id si es seguro para escribirlo en un registro; si no, `-`."""
    if isinstance(value, str) and _SAFE.fullmatch(value):
        return value
    return NO_REQUEST_ID


def current_request_id() -> str:
    return _request_id.get()


class RequestIdFilter(logging.Filter):
    """Añade `request_id` a cada registro (`%(request_id)s` en el formato)."""

    def filter(self, record: logging.LogRecord) -> bool:
        record.request_id = _request_id.get()
        return True


def configure_logging(level: str) -> None:
    """Configura el registro raíz con el id de petición en cada línea."""
    logging.basicConfig(
        level=level,
        format="%(asctime)s %(levelname)s %(name)s request_id=%(request_id)s: %(message)s",
    )
    for handler in logging.getLogger().handlers:
        handler.addFilter(RequestIdFilter())


def _metadata_id(context: grpc.ServicerContext) -> str:
    for key, value in context.invocation_metadata() or ():
        if key == REQUEST_ID_KEY:
            return sanitize(value)
    return NO_REQUEST_ID


class RequestIdInterceptor(grpc.ServerInterceptor):
    """Lee `x-request-id`, lo deja en el contexto y registra cada llamada.

    Las llamadas de salud van a nivel DEBUG (las consultan el healthcheck de
    Docker y el panel cada pocos segundos); el resto, a INFO.
    """

    def intercept_service(self, continuation, handler_call_details):
        handler = continuation(handler_call_details)
        if handler is None or handler.unary_unary is None:
            return handler
        method = handler_call_details.method
        level = logging.DEBUG if method.startswith(_HEALTH_PREFIX) else logging.INFO
        behavior = handler.unary_unary

        def wrapped(request, context):
            token = _request_id.set(_metadata_id(context))
            started = time.monotonic()
            status = "OK"
            try:
                return behavior(request, context)
            except BaseException:
                code = getattr(context, "code", lambda: None)()
                status = code.name if code is not None else "ERROR"
                raise
            finally:
                if log.isEnabledFor(level):
                    log.log(level, "%s %s %.1f ms", method, status, (time.monotonic() - started) * 1000)
                _request_id.reset(token)

        return handler._replace(unary_unary=wrapped)
