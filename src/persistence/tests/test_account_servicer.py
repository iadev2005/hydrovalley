"""Pruebas del servidor gRPC de AccountService con el repositorio en memoria.

Igual que test_servicer: servidor gRPC real en un puerto libre de 127.0.0.1
y stub generado, para comprobar los códigos de estado tal como los verá la API.
"""

import uuid

import grpc
import pytest
from grpc_health.v1 import health_pb2, health_pb2_grpc

from hydrovalley.persistence.v1 import account_pb2 as pb
from hydrovalley.persistence.v1 import account_pb2_grpc as pb_grpc
from persistence.adapters.memory import (
    InMemoryAccountRepository,
    InMemoryCatalogRepository,
    InMemorySnapshotRepository,
)
from persistence.account_servicer import MAX_USERNAMES_PER_REQUEST
from persistence.domain import InvalidAccountError, RepositoryUnavailableError
from persistence.server import ACCOUNT_SERVICE_NAME, create_server, set_serving

HASH = "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$aGFzaGhhc2hoYXNo"


@pytest.fixture
def accounts():
    return InMemoryAccountRepository()


@pytest.fixture
def running(accounts):
    server, port, health = create_server(
        InMemorySnapshotRepository(), "127.0.0.1:0", accounts=accounts,
        catalog=InMemoryCatalogRepository(),
    )
    server.start()
    channel = grpc.insecure_channel(f"127.0.0.1:{port}")
    yield pb_grpc.AccountServiceStub(channel), channel, health
    channel.close()
    server.stop(grace=None)


@pytest.fixture
def stub(running):
    return running[0]


def register(stub, username="ana", password_hash=HASH):
    return stub.Register(
        pb.RegisterRequest(username=username, password_hash=password_hash), timeout=2
    )


def get_account(stub, username="ana"):
    return stub.GetAccount(pb.GetAccountRequest(username=username), timeout=2)


def get_usernames(stub, ids):
    return stub.GetUsernames(pb.GetUsernamesRequest(player_ids=ids), timeout=2)


def status_of(call) -> grpc.StatusCode:
    with pytest.raises(grpc.RpcError) as error:
        call()
    return error.value.code()


# --- Register ---------------------------------------------------------------


def test_register_creates_a_player_with_a_uuid(stub):
    response = register(stub)
    assert str(uuid.UUID(response.account_id)) == response.account_id
    assert response.role == pb.ROLE_PLAYER


def test_register_gives_each_account_its_own_id(stub):
    assert register(stub, "ana").account_id != register(stub, "beto").account_id


def test_duplicate_username_is_already_exists(stub):
    register(stub, "ana")
    assert status_of(lambda: register(stub, "ana")) == grpc.StatusCode.ALREADY_EXISTS


def test_usernames_are_unique_ignoring_case(stub):
    register(stub, "Ana")
    assert status_of(lambda: register(stub, "aNA")) == grpc.StatusCode.ALREADY_EXISTS


@pytest.mark.parametrize("username", ["", "ab", "x" * 33, "con espacio", "ñandú", "a\x00b"])
def test_invalid_username_is_invalid_argument(stub, username):
    code = status_of(lambda: register(stub, username))
    assert code == grpc.StatusCode.INVALID_ARGUMENT


@pytest.mark.parametrize(
    "password_hash", ["", "hunter2", "$argon2i$v=19$x$y", "$argon2id$", "$argon2id$a b"]
)
def test_invalid_password_hash_is_invalid_argument(stub, password_hash):
    code = status_of(lambda: register(stub, "ana", password_hash))
    assert code == grpc.StatusCode.INVALID_ARGUMENT


def test_rejected_register_stores_nothing(stub, accounts):
    status_of(lambda: register(stub, "ab"))
    assert accounts.get_by_username("ab") is None


# --- GetAccount ---------------------------------------------------------------


def test_get_account_returns_id_hash_and_role(stub):
    created = register(stub, "Ana")
    found = get_account(stub, "Ana")
    assert found.account_id == created.account_id
    assert found.password_hash == HASH
    assert found.role == pb.ROLE_PLAYER


def test_get_account_ignores_case(stub):
    created = register(stub, "Ana")
    assert get_account(stub, "ANA").account_id == created.account_id


def test_get_account_unknown_is_not_found(stub):
    assert status_of(lambda: get_account(stub, "nadie")) == grpc.StatusCode.NOT_FOUND


@pytest.mark.parametrize("username", ["", "x" * 500, "con espacio", "a\x00b"])
def test_get_account_with_an_impossible_username_is_not_found(stub, username):
    # Un nombre que nunca pudo registrarse simplemente no existe: el acceso
    # de la API trata igual todos los fallos de búsqueda.
    assert status_of(lambda: get_account(stub, username)) == grpc.StatusCode.NOT_FOUND


def test_get_account_returns_admin_role(stub, accounts):
    accounts.add_admin("root", HASH)
    assert get_account(stub, "root").role == pb.ROLE_ADMIN


# --- GetUsernames -------------------------------------------------------------


def test_get_usernames_maps_existing_ids_and_omits_unknown(stub):
    ana = register(stub, "Ana").account_id
    beto = register(stub, "beto").account_id
    unknown = str(uuid.uuid4())
    response = get_usernames(stub, [ana, unknown, beto])
    assert dict(response.usernames) == {ana: "Ana", beto: "beto"}


def test_get_usernames_with_no_ids_is_empty(stub):
    assert dict(get_usernames(stub, []).usernames) == {}


def test_get_usernames_accepts_duplicates_and_uppercase_ids(stub):
    ana = register(stub, "ana").account_id
    response = get_usernames(stub, [ana, ana.upper()])
    assert dict(response.usernames) == {ana: "ana"}


def test_get_usernames_malformed_id_is_invalid_argument(stub):
    code = status_of(lambda: get_usernames(stub, ["no-es-uuid"]))
    assert code == grpc.StatusCode.INVALID_ARGUMENT


def test_get_usernames_at_the_limit_is_ok(stub):
    ids = [str(uuid.uuid4()) for _ in range(MAX_USERNAMES_PER_REQUEST)]
    assert dict(get_usernames(stub, ids).usernames) == {}


def test_get_usernames_over_the_limit_is_invalid_argument(stub):
    ids = [str(uuid.uuid4()) for _ in range(MAX_USERNAMES_PER_REQUEST + 1)]
    assert status_of(lambda: get_usernames(stub, ids)) == grpc.StatusCode.INVALID_ARGUMENT


# --- Errores del repositorio ---------------------------------------------------


def test_unavailable_repository_is_unavailable(stub, accounts):
    accounts.available = False
    assert status_of(lambda: register(stub)) == grpc.StatusCode.UNAVAILABLE
    assert status_of(lambda: get_account(stub)) == grpc.StatusCode.UNAVAILABLE
    assert status_of(lambda: get_usernames(stub, [])) == grpc.StatusCode.UNAVAILABLE


def test_data_the_schema_rejects_is_invalid_argument(stub, accounts):
    accounts.error = InvalidAccountError("dato inválido")
    assert status_of(lambda: register(stub)) == grpc.StatusCode.INVALID_ARGUMENT


def test_unexpected_errors_are_internal_never_unknown(stub, accounts):
    accounts.error = RuntimeError("boom")
    assert status_of(lambda: register(stub)) == grpc.StatusCode.INTERNAL
    assert status_of(lambda: get_account(stub)) == grpc.StatusCode.INTERNAL
    assert status_of(lambda: get_usernames(stub, [])) == grpc.StatusCode.INTERNAL


LEAK = "SECRET-SQL-TEXT password_hash=$argon2id$leak"


def detail_of(call) -> str:
    with pytest.raises(grpc.RpcError) as error:
        call()
    return error.value.details()


@pytest.mark.parametrize(
    ("failure", "expected"),
    [
        (RuntimeError(LEAK), "error interno"),
        (InvalidAccountError(LEAK), "los datos de la cuenta no son válidos"),
        (RepositoryUnavailableError(LEAK), "base de datos no disponible"),
    ],
)
def test_error_details_are_fixed_and_never_echo_the_exception(stub, accounts, failure, expected):
    accounts.error = failure
    calls = [
        lambda: register(stub),
        lambda: get_account(stub),
        lambda: get_usernames(stub, []),
    ]
    for call in calls:
        detail = detail_of(call)
        assert detail == expected
        assert "SECRET" not in detail


def test_already_exists_detail_does_not_echo_the_username(stub):
    register(stub, "ana")
    assert detail_of(lambda: register(stub, "ana")) == "el nombre de usuario ya existe"


# --- Salud ------------------------------------------------------------------------


def test_health_covers_account_service(running):
    _, channel, health = running
    health_stub = health_pb2_grpc.HealthStub(channel)

    def check() -> int:
        request = health_pb2.HealthCheckRequest(service=ACCOUNT_SERVICE_NAME)
        return health_stub.Check(request, timeout=2).status

    assert ACCOUNT_SERVICE_NAME == "hydrovalley.persistence.v1.AccountService"
    assert check() == health_pb2.HealthCheckResponse.NOT_SERVING
    set_serving(health, True)
    assert check() == health_pb2.HealthCheckResponse.SERVING
