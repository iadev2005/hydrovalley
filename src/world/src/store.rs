//! Puerto hacia la persistencia de instantáneas (Capa 5) y su adaptador
//! gRPC contra `PersistenceService`.

use std::future::Future;
use std::time::Duration;

use tonic::transport::{Channel, Endpoint};
use tonic::{Code, Request, Status};

use crate::proto::persistence::v1 as persistence_pb;
use crate::proto::persistence::v1::persistence_service_client::PersistenceServiceClient;

/// Fila de instantánea tal como viaja a la persistencia.
pub type Snapshot = persistence_pb::Snapshot;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StoreError {
    /// La persistencia no responde (UNAVAILABLE o DEADLINE_EXCEEDED tras
    /// los reintentos).
    Unavailable(String),
    /// Ya hay otra instantánea distinta para ese tick. Por diseño no debe
    /// ocurrir nunca.
    AlreadyExists(String),
    /// Cualquier otro error.
    Failed(String),
}

impl std::fmt::Display for StoreError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Unavailable(message) => write!(f, "persistencia no disponible: {message}"),
            Self::AlreadyExists(message) => write!(f, "instantánea en conflicto: {message}"),
            Self::Failed(message) => write!(f, "error de la persistencia: {message}"),
        }
    }
}

impl std::error::Error for StoreError {}

/// Dónde se guardan y de dónde se cargan las instantáneas.
pub trait SnapshotStore: Send + Sync + 'static {
    /// La instantánea de mayor tick, o `None` si no hay ninguna.
    fn load(&self) -> impl Future<Output = Result<Option<Snapshot>, StoreError>> + Send;
    fn save(&self, snapshot: Snapshot) -> impl Future<Output = Result<(), StoreError>> + Send;
}

/// Plazo y reintentos de cada llamada (regla 4 de portabilidad): 2 s y, al
/// ser idempotentes, como mucho 3 reintentos tras 100, 200 y 400 ms.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CallPolicy {
    pub deadline: Duration,
    pub retry_delays: Vec<Duration>,
}

impl Default for CallPolicy {
    fn default() -> Self {
        Self {
            deadline: Duration::from_secs(2),
            retry_delays: [100, 200, 400].map(Duration::from_millis).to_vec(),
        }
    }
}

/// Códigos que merecen otro intento de una llamada idempotente.
pub fn is_retryable(code: Code) -> bool {
    matches!(code, Code::Unavailable | Code::DeadlineExceeded)
}

/// Adaptador gRPC de [`SnapshotStore`].
#[derive(Debug, Clone)]
pub struct GrpcSnapshotStore {
    client: PersistenceServiceClient<Channel>,
    policy: CallPolicy,
}

impl GrpcSnapshotStore {
    /// No conecta todavía: el canal se abre con la primera llamada y se
    /// reconecta solo (regla 5 de portabilidad).
    pub fn connect_lazy(
        address: &str,
        policy: CallPolicy,
    ) -> Result<Self, tonic::transport::Error> {
        let uri = if address.contains("://") {
            address.to_string()
        } else {
            format!("http://{address}")
        };
        let channel = Endpoint::from_shared(uri)?
            .connect_timeout(policy.deadline)
            .timeout(policy.deadline)
            .connect_lazy();
        Ok(Self {
            client: PersistenceServiceClient::new(channel),
            policy,
        })
    }

    async fn call<T, F, Fut>(&self, method: &str, mut attempt: F) -> Result<T, Status>
    where
        F: FnMut(PersistenceServiceClient<Channel>, Duration) -> Fut,
        Fut: Future<Output = Result<T, Status>>,
    {
        let mut delays = self.policy.retry_delays.iter();
        loop {
            let deadline = self.policy.deadline;
            let result = match tokio::time::timeout(
                deadline,
                attempt(self.client.clone(), deadline),
            )
            .await
            {
                Ok(result) => result,
                Err(_) => Err(Status::deadline_exceeded(format!(
                    "{method}: plazo de {deadline:?} agotado"
                ))),
            };
            match result {
                Err(status) if is_retryable(status.code()) => match delays.next() {
                    Some(delay) => {
                        tracing::debug!(method, code = ?status.code(), ?delay, "reintento");
                        tokio::time::sleep(*delay).await;
                    }
                    None => return Err(status),
                },
                other => return other,
            }
        }
    }
}

fn request<T>(message: T, deadline: Duration) -> Request<T> {
    let mut request = Request::new(message);
    request.set_timeout(deadline);
    request
}

fn store_error(status: &Status) -> StoreError {
    let message = format!("{:?}: {}", status.code(), status.message());
    match status.code() {
        Code::Unavailable | Code::DeadlineExceeded => StoreError::Unavailable(message),
        Code::AlreadyExists => StoreError::AlreadyExists(message),
        _ => StoreError::Failed(message),
    }
}

impl SnapshotStore for GrpcSnapshotStore {
    async fn load(&self) -> Result<Option<Snapshot>, StoreError> {
        let result = self
            .call("LoadSnapshot", |mut client, deadline| async move {
                client
                    .load_snapshot(request(persistence_pb::LoadSnapshotRequest {}, deadline))
                    .await
            })
            .await;
        match result {
            Ok(response) => match response.into_inner().snapshot {
                Some(snapshot) => Ok(Some(snapshot)),
                None => Err(StoreError::Failed("LoadSnapshot sin snapshot".to_string())),
            },
            Err(status) if status.code() == Code::NotFound => Ok(None),
            Err(status) => Err(store_error(&status)),
        }
    }

    async fn save(&self, snapshot: Snapshot) -> Result<(), StoreError> {
        self.call("SaveSnapshot", |mut client, deadline| {
            let snapshot = snapshot.clone();
            async move {
                client
                    .save_snapshot(request(
                        persistence_pb::SaveSnapshotRequest {
                            snapshot: Some(snapshot),
                        },
                        deadline,
                    ))
                    .await
            }
        })
        .await
        .map(|_| ())
        .map_err(|status| store_error(&status))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_transient_codes_are_retried() {
        assert!(is_retryable(Code::Unavailable));
        assert!(is_retryable(Code::DeadlineExceeded));
        for code in [
            Code::Ok,
            Code::NotFound,
            Code::AlreadyExists,
            Code::InvalidArgument,
            Code::Internal,
            Code::Unimplemented,
        ] {
            assert!(!is_retryable(code), "{code:?}");
        }
    }

    #[test]
    fn default_policy_follows_the_portability_rule() {
        let policy = CallPolicy::default();
        assert_eq!(policy.deadline, Duration::from_secs(2));
        assert_eq!(
            policy.retry_delays,
            vec![
                Duration::from_millis(100),
                Duration::from_millis(200),
                Duration::from_millis(400)
            ]
        );
    }

    #[test]
    fn status_codes_map_to_store_errors() {
        assert!(matches!(
            store_error(&Status::unavailable("x")),
            StoreError::Unavailable(_)
        ));
        assert!(matches!(
            store_error(&Status::deadline_exceeded("x")),
            StoreError::Unavailable(_)
        ));
        assert!(matches!(
            store_error(&Status::already_exists("x")),
            StoreError::AlreadyExists(_)
        ));
        assert!(matches!(
            store_error(&Status::internal("x")),
            StoreError::Failed(_)
        ));
    }
}
