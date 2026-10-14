"""Identificador de petición (RNF-07): lectura del metadato `x-request-id`,
registro de cada llamada y filtro de logging."""

import io
import logging

import grpc
import pytest
from grpc_health.v1 import health_pb2, health_pb2_grpc

from hydrovalley.persistence.v1 import account_pb2 as account_pb
from hydrovalley.persistence.v1 import account_pb2_grpc as account_pb_grpc
from persistence.adapters.memory import (
    InMemoryAccountRepository,
    InMemoryCatalogRepository,
    InMemorySnapshotRepository,
)
from persistence.request_id import (
    NO_REQUEST_ID,
    REQUEST_ID_KEY,
    RequestIdFilter,
    current_request_id,
    sanitize,
)
from persistence.server import create_server, set_serving

ID = "3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b"


@pytest.fixture
def running():
    server, port, health = create_server(
        InMemorySnapshotRepository(),
        "127.0.0.1:0",
        accounts=InMemoryAccountRepository(),
        catalog=InMemoryCatalogRepository(),
    )
    set_serving(health, True)
    server.start()
    channel = grpc.insecure_channel(f"127.0.0.1:{port}")
    yield channel
    channel.close()
    server.stop(grace=None)


@pytest.fixture
def tagged(caplog):
    """caplog con el filtro que añade `request_id` a cada registro."""
    caplog.handler.addFilter(RequestIdFilter())
    caplog.set_level(logging.DEBUG)
    return caplog


def get_account(channel, username="nadie", metadata=()):
    stub = account_pb_grpc.AccountServiceStub(channel)
    try:
        stub.GetAccount(account_pb.GetAccountRequest(username=username), metadata=metadata)
    except grpc.RpcError:
        pass


def rpc_records(caplog):
    return [r for r in caplog.records if r.name == "persistence.request"]


class TestSanitize:
    @pytest.mark.parametrize("value", [ID, "0123456789abcdef0123456789ABCDEF", "tick-42", "world-" + ID])
    def test_keeps_safe_ids(self, value):
        assert sanitize(value) == value

    @pytest.mark.parametrize("value", [None, "", "a" * 65, "con espacio", "x\r\ny", "<script>"])
    def test_unusable_ids_become_dash(self, value):
        assert sanitize(value) == NO_REQUEST_ID == "-"


class TestFilter:
    def test_filter_adds_dash_outside_a_call(self):
        record = logging.LogRecord("x", logging.INFO, "f", 1, "m", None, None)
        assert RequestIdFilter().filter(record) is True
        assert record.request_id == "-"
        assert current_request_id() == "-"


class TestInterceptor:
    def test_call_is_logged_with_the_incoming_id(self, running, tagged):
        get_account(running, metadata=((REQUEST_ID_KEY, ID),))
        records = rpc_records(tagged)
        assert len(records) == 1
        assert records[0].request_id == ID
        assert "AccountService/GetAccount" in records[0].getMessage()
        assert "NOT_FOUND" in records[0].getMessage() or "UNAVAILABLE" in records[0].getMessage()

    def test_missing_or_invalid_id_is_a_dash(self, running, tagged):
        get_account(running)
        get_account(running, metadata=((REQUEST_ID_KEY, "no valido!"),))
        assert [r.request_id for r in rpc_records(tagged)] == ["-", "-"]

    def test_id_does_not_leak_into_the_next_call(self, running, tagged):
        get_account(running, metadata=((REQUEST_ID_KEY, ID),))
        get_account(running)
        assert [r.request_id for r in rpc_records(tagged)] == [ID, "-"]

    def test_lines_logged_inside_the_servicer_carry_the_id(self, tagged):
        class Broken(InMemoryAccountRepository):
            def get_by_username(self, username):
                raise RuntimeError("falla simulada")

        server, port, _ = create_server(
            InMemorySnapshotRepository(),
            "127.0.0.1:0",
            accounts=Broken(),
            catalog=InMemoryCatalogRepository(),
        )
        server.start()
        channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        try:
            get_account(channel, username="ana", metadata=((REQUEST_ID_KEY, ID),))
        finally:
            channel.close()
            server.stop(grace=None)
        inner = [r for r in tagged.records if r.name == "persistence.account_servicer"]
        assert inner, "el servicer debía registrar el error inesperado"
        assert all(r.request_id == ID for r in inner)

    def test_health_checks_are_debug_not_info(self, running, tagged):
        tagged.set_level(logging.INFO)
        health_pb2_grpc.HealthStub(running).Check(health_pb2.HealthCheckRequest(service=""))
        assert rpc_records(tagged) == []
        tagged.set_level(logging.DEBUG)
        health_pb2_grpc.HealthStub(running).Check(health_pb2.HealthCheckRequest(service=""))
        assert len(rpc_records(tagged)) == 1


def test_formatted_line_has_the_id():
    stream = io.StringIO()
    handler = logging.StreamHandler(stream)
    handler.addFilter(RequestIdFilter())
    handler.setFormatter(logging.Formatter("%(levelname)s request_id=%(request_id)s %(message)s"))
    log = logging.getLogger("persistence.test_fmt")
    log.addHandler(handler)
    log.propagate = False
    log.warning("hola")
    assert stream.getvalue().strip() == "WARNING request_id=- hola"
