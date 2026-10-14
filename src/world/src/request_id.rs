//! Identificador de petición de punta a punta (RNF-07).
//!
//! Entrante: el metadato gRPC `x-request-id` (la API lo envía en
//! `GetRanking`; el hub, como `tick-<n>`, en cada `Step`) se lee, se valida y
//! se pone como campo `request_id` del *span* de la llamada, así que todas las
//! líneas de registro de esa llamada lo llevan. Saliente: cada llamada del
//! simulador a la persistencia genera su propio id (`world-<uuid>`) y lo
//! envía como el mismo metadato, para correlacionar los dos registros.

use tonic::Request;
use tonic::metadata::{MetadataMap, MetadataValue};
use uuid::Uuid;

/// Nombre del metadato gRPC (en minúsculas, como exige HTTP/2).
pub const REQUEST_ID_KEY: &str = "x-request-id";

/// Valor que se registra cuando la llamada no trae un id utilizable.
pub const NO_REQUEST_ID: &str = "-";

const MAX_LEN: usize = 64;

/// Un id es seguro para escribirlo en un registro si tiene de 1 a 64
/// caracteres entre letras, dígitos y guion (UUID, 32 hexadecimales,
/// `tick-<n>` o `world-<uuid>`). Cualquier otra cosa (espacios, saltos de
/// línea) se descarta.
pub fn is_safe(id: &str) -> bool {
    (1..=MAX_LEN).contains(&id.len()) && id.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-')
}

/// El id de la llamada entrante, o `-` si falta o no es seguro.
pub fn from_metadata(metadata: &MetadataMap) -> String {
    metadata
        .get(REQUEST_ID_KEY)
        .and_then(|value| value.to_str().ok())
        .filter(|id| is_safe(id))
        .unwrap_or(NO_REQUEST_ID)
        .to_string()
}

/// Id nuevo para una llamada saliente del simulador.
pub fn outgoing_id() -> String {
    format!("world-{}", Uuid::new_v4())
}

/// Añade el id a una petición saliente.
pub fn tag<T>(request: &mut Request<T>, id: &str) {
    if let Ok(value) = MetadataValue::try_from(id) {
        request.metadata_mut().insert(REQUEST_ID_KEY, value);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn metadata(value: Option<&str>) -> MetadataMap {
        let mut map = MetadataMap::new();
        if let Some(value) = value {
            map.insert(REQUEST_ID_KEY, value.parse().unwrap());
        }
        map
    }

    #[test]
    fn keeps_safe_ids() {
        for id in [
            "3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b",
            "0123456789abcdef0123456789ABCDEF",
            "tick-42",
            &format!("world-{}", Uuid::new_v4()),
            &"a".repeat(64),
        ] {
            assert!(is_safe(id), "{id}");
            assert_eq!(from_metadata(&metadata(Some(id))), id);
        }
    }

    #[test]
    fn missing_or_unusable_ids_become_a_dash() {
        assert_eq!(from_metadata(&metadata(None)), NO_REQUEST_ID);
        for id in ["", "con espacio", "a/b", "<script>", &"a".repeat(65)] {
            assert!(!is_safe(id), "{id:?}");
        }
        // Un valor que ni siquiera es ASCII visible tampoco pasa.
        let mut map = MetadataMap::new();
        map.insert_bin("x-request-id-bin", MetadataValue::from_bytes(b"\xff\xfe"));
        assert_eq!(from_metadata(&map), NO_REQUEST_ID);
        assert_eq!(from_metadata(&metadata(Some("con espacio"))), NO_REQUEST_ID);
    }

    #[test]
    fn outgoing_ids_are_safe_and_distinct() {
        let (a, b) = (outgoing_id(), outgoing_id());
        assert!(a.starts_with("world-") && is_safe(&a));
        assert_ne!(a, b);
    }

    #[test]
    fn tag_sets_the_metadata() {
        let mut request = Request::new(());
        tag(&mut request, "tick-7");
        assert_eq!(from_metadata(request.metadata()), "tick-7");
    }
}
