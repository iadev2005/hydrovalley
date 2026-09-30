"""Servidor gRPC de CatalogService (GetCatalog).

Lee las filas de `catalog_entries`, las revalida, calcula `catalog_version`
y las traduce a los mensajes de `catalog.proto` con la tabla
`persistence.catalog.SECTIONS`. No sabe nada de PostgreSQL: depende solo del
puerto CatalogRepository.
"""

import logging

import grpc

from hydrovalley.persistence.v1 import catalog_pb2 as pb
from hydrovalley.persistence.v1 import catalog_pb2_grpc as pb_grpc
from persistence.catalog import SECTIONS, Catalog, CatalogError, catalog_from_entries
from persistence.domain import RepositoryUnavailableError
from persistence.ports import CatalogRepository

log = logging.getLogger(__name__)

# Elemento del catálogo -> (mensaje, campo de pb.Catalog) de catalog.proto.
_MESSAGES = {
    "drop": (pb.DropRules, "drop"),
    "tank": (pb.TankRules, "tank"),
    "growth": (pb.GrowthRules, "growth"),
    "evaporation": (pb.EvaporationRules, "evaporation"),
    "fire": (pb.FireRules, "fire"),
    "sprinkler": (pb.SprinklerRules, "sprinkler"),
    "drip": (pb.DripRules, "drip"),
    "treatment_plant": (pb.TreatmentPlantRules, "treatment_plant"),
    "bigger_drop": (pb.BiggerDropRules, "bigger_drop"),
    "bigger_tank": (pb.BiggerTankRules, "bigger_tank"),
}


def to_message(catalog: Catalog) -> pb.Catalog:
    """Traduce el catálogo validado al mensaje del contrato. Una cifra sin
    definir (`null`) viaja como 0: el contrato no admite nulos."""
    message = pb.Catalog(catalog_version=catalog.version)
    for entry in catalog.entries:
        section = SECTIONS[entry.key]
        if section.scalar is not None:
            setattr(message, section.scalar.proto, entry.value)
            continue
        rules_type, attribute = _MESSAGES[entry.key]
        values = {
            field.proto: entry.value[field.name] or 0 for field in section.fields
        }
        getattr(message, attribute).CopyFrom(rules_type(**values))
    return message


def _status_for(error: Exception) -> grpc.StatusCode:
    """Traduce una excepción a su código de gRPC. Lo inesperado es INTERNAL,
    nunca UNKNOWN. Un catálogo guardado que no valida también es INTERNAL:
    la siembra ya lo validó, así que no es culpa de quien llama."""
    if isinstance(error, RepositoryUnavailableError):
        return grpc.StatusCode.UNAVAILABLE
    return grpc.StatusCode.INTERNAL


# Detalle fijo por código: nunca se envía el texto de la excepción, que puede
# citar SQL o cifras del catálogo.
_DETAIL = {
    grpc.StatusCode.UNAVAILABLE: "base de datos no disponible",
    grpc.StatusCode.INTERNAL: "error interno",
}


class CatalogServicer(pb_grpc.CatalogServiceServicer):
    def __init__(self, repository: CatalogRepository) -> None:
        self._repository = repository

    def GetCatalog(self, request: pb.GetCatalogRequest, context: grpc.ServicerContext):
        # context.abort lanza su propia excepción: se llama fuera del try
        # para que el except genérico no la capture.
        try:
            catalog = catalog_from_entries(self._repository.load_entries())
            response = pb.GetCatalogResponse(catalog=to_message(catalog))
        except Exception as error:
            code = _status_for(error)
            if isinstance(error, CatalogError):
                log.error("GetCatalog: el catálogo guardado no es válido: %s", error)
            elif code is grpc.StatusCode.INTERNAL:
                log.error("GetCatalog: error inesperado", exc_info=error)
        else:
            return response
        context.abort(code, _DETAIL[code])
