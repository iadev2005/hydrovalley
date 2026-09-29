"""Servidor gRPC de AccountService (Register, GetAccount y GetUsernames).

Valida la petición, traduce entre los mensajes del contrato y el dominio, y
entre las excepciones del puerto y los códigos estándar de gRPC. Solo ve
hashes: la contraseña en claro nunca llega a este servicio (RNF-12).
"""

import logging
import uuid

import grpc

from hydrovalley.persistence.v1 import account_pb2 as pb
from hydrovalley.persistence.v1 import account_pb2_grpc as pb_grpc
from persistence.domain import (
    AccountAlreadyExistsError,
    InvalidAccountError,
    RepositoryUnavailableError,
    Role,
    is_valid_username,
    password_hash_problem,
    username_problem,
)
from persistence.ports import AccountRepository

log = logging.getLogger(__name__)

# Tope de identificadores por GetUsernames: el ranking pide unas decenas.
MAX_USERNAMES_PER_REQUEST = 1000

_ROLE_TO_PROTO = {Role.PLAYER: pb.ROLE_PLAYER, Role.ADMIN: pb.ROLE_ADMIN}


def _status_for(error: Exception) -> grpc.StatusCode:
    """Traduce una excepción del puerto a su código de gRPC. Lo inesperado
    es INTERNAL, nunca UNKNOWN."""
    if isinstance(error, RepositoryUnavailableError):
        return grpc.StatusCode.UNAVAILABLE
    if isinstance(error, AccountAlreadyExistsError):
        return grpc.StatusCode.ALREADY_EXISTS
    if isinstance(error, InvalidAccountError):
        return grpc.StatusCode.INVALID_ARGUMENT
    return grpc.StatusCode.INTERNAL


class AccountServicer(pb_grpc.AccountServiceServicer):
    def __init__(self, repository: AccountRepository) -> None:
        self._repository = repository

    def Register(self, request: pb.RegisterRequest, context: grpc.ServicerContext):
        problem = username_problem(request.username) or password_hash_problem(
            request.password_hash
        )
        if problem is not None:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, problem)
        # context.abort lanza su propia excepción: se llama fuera del try
        # para que el except genérico no la capture.
        try:
            account = self._repository.register(request.username, request.password_hash)
        except Exception as error:
            code, detail = self._log_failure("Register", error), str(error)
        else:
            return pb.RegisterResponse(
                account_id=str(account.id), role=_ROLE_TO_PROTO[account.role]
            )
        context.abort(code, detail)

    def GetAccount(self, request: pb.GetAccountRequest, context: grpc.ServicerContext):
        # Un nombre que no cumple las reglas nunca pudo registrarse: no existe.
        # Así el acceso de la API trata igual todas las búsquedas fallidas.
        if not is_valid_username(request.username):
            context.abort(grpc.StatusCode.NOT_FOUND, "la cuenta no existe")
        try:
            account = self._repository.get_by_username(request.username)
        except Exception as error:
            code, detail = self._log_failure("GetAccount", error), str(error)
        else:
            if account is None:
                context.abort(grpc.StatusCode.NOT_FOUND, "la cuenta no existe")
            return pb.GetAccountResponse(
                account_id=str(account.id),
                password_hash=account.password_hash,
                role=_ROLE_TO_PROTO[account.role],
            )
        context.abort(code, detail)

    def GetUsernames(self, request: pb.GetUsernamesRequest, context: grpc.ServicerContext):
        if len(request.player_ids) > MAX_USERNAMES_PER_REQUEST:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                f"player_ids admite como máximo {MAX_USERNAMES_PER_REQUEST} identificadores",
            )
        try:
            ids = [uuid.UUID(value) for value in request.player_ids]
        except ValueError:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "player_ids contiene un UUID inválido")
        try:
            names = self._repository.get_usernames(ids)
        except Exception as error:
            code, detail = self._log_failure("GetUsernames", error), str(error)
        else:
            # Las claves salen en la forma canónica del UUID (minúsculas).
            return pb.GetUsernamesResponse(
                usernames={str(player_id): name for player_id, name in names.items()}
            )
        context.abort(code, detail)

    @staticmethod
    def _log_failure(method: str, error: Exception) -> grpc.StatusCode:
        code = _status_for(error)
        if code is grpc.StatusCode.INTERNAL:
            log.error("%s: error inesperado", method, exc_info=error)
        elif code is not grpc.StatusCode.UNAVAILABLE:
            # Sin el detalle de la excepción: puede citar datos de la cuenta.
            log.warning("%s rechazado (%s)", method, code.name)
        return code
