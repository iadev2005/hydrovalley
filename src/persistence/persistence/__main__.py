"""Punto de entrada: python -m persistence (con gen/python en PYTHONPATH)."""

import logging
import os
import signal
import sys
import threading

from persistence.adapters.postgres import PostgresSnapshotRepository
from persistence.config import ConfigError, Settings
from persistence.server import create_server, set_serving

log = logging.getLogger("persistence")


def main() -> int:
    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )
    try:
        settings = Settings.from_env(os.environ)
    except ConfigError as error:
        log.error("%s", error)
        return 2

    # Un mismo adaptador de PostgreSQL sirve los cuatro puertos (instantáneas,
    # cuentas, catálogo y reportes) y siembra el catálogo al conectar.
    store = PostgresSnapshotRepository(
        settings.database_url,
        admin_seed=settings.admin_seed,
        catalog=settings.catalog,
    )
    server, port, health = create_server(
        store, settings.listen, accounts=store, catalog=store, reports=store
    )
    store.on_ready_change = lambda ready: set_serving(health, ready)

    # El servidor gRPC atiende desde el principio; la base de datos se
    # conecta en segundo plano y, mientras tanto, se responde UNAVAILABLE.
    server.start()
    store.start()
    log.info(
        "persistencia escuchando en %s (puerto %d); catálogo %s",
        settings.listen,
        port,
        settings.catalog.version,
    )

    stop = threading.Event()
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, lambda *_: stop.set())
    stop.wait()

    log.info("deteniendo la persistencia")
    set_serving(health, False)
    server.stop(grace=5).wait()
    store.stop()
    return 0


if __name__ == "__main__":
    sys.exit(main())
