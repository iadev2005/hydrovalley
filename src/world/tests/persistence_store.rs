//! Prueba de integración del adaptador `GrpcPersistenceStore` contra una
//! persistencia falsa servida con tonic: reintentos acotados solo ante
//! UNAVAILABLE o DEADLINE_EXCEEDED y plazo por intento.

use std::collections::VecDeque;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tokio::net::TcpListener;
use tokio::sync::oneshot;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tonic::{Code, Request, Response, Status};

use world::proto::persistence::v1 as persistence_pb;
use world::proto::persistence::v1::persistence_service_server::{
    PersistenceService, PersistenceServiceServer,
};
mod common;

use world::proto::persistence::v1::catalog_service_server::{CatalogService, CatalogServiceServer};
use world::store::{CallPolicy, GrpcPersistenceStore, PersistenceStore, StoreError};

/// Qué hace la persistencia falsa en un intento concreto.
#[derive(Clone, Copy)]
enum Step {
    /// Espera `Duration` y luego responde con éxito.
    Delay(Duration),
    /// Responde con ese código de error.
    Fail(Code),
}

/// Guion de respuestas: se consume un paso por llamada; agotado el guion,
/// responde con éxito sin demora.
struct Script {
    steps: Mutex<VecDeque<Step>>,
    calls: AtomicUsize,
    /// Lo que responde GetCatalog cuando el guion no falla.
    catalog: Mutex<persistence_pb::Catalog>,
}

impl Script {
    fn new(steps: impl IntoIterator<Item = Step>) -> Arc<Self> {
        Arc::new(Self {
            steps: Mutex::new(steps.into_iter().collect()),
            calls: AtomicUsize::new(0),
            catalog: Mutex::new(common::catalog()),
        })
    }

    fn calls(&self) -> usize {
        self.calls.load(Ordering::SeqCst)
    }

    async fn next(&self) -> Result<(), Status> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        let step = self.steps.lock().unwrap().pop_front();
        match step {
            None => Ok(()),
            Some(Step::Delay(delay)) => {
                tokio::time::sleep(delay).await;
                Ok(())
            }
            Some(Step::Fail(code)) => Err(Status::new(code, "fallo programado")),
        }
    }
}

#[derive(Clone)]
struct FakeService(Arc<Script>);

#[tonic::async_trait]
impl PersistenceService for FakeService {
    async fn save_snapshot(
        &self,
        _: Request<persistence_pb::SaveSnapshotRequest>,
    ) -> Result<Response<persistence_pb::SaveSnapshotResponse>, Status> {
        self.0.next().await?;
        Ok(Response::new(persistence_pb::SaveSnapshotResponse {}))
    }

    async fn load_snapshot(
        &self,
        _: Request<persistence_pb::LoadSnapshotRequest>,
    ) -> Result<Response<persistence_pb::LoadSnapshotResponse>, Status> {
        self.0.next().await?;
        Ok(Response::new(persistence_pb::LoadSnapshotResponse {
            snapshot: Some(persistence_pb::Snapshot {
                tick: 7,
                ..Default::default()
            }),
        }))
    }
}

#[tonic::async_trait]
impl CatalogService for FakeService {
    async fn get_catalog(
        &self,
        _: Request<persistence_pb::GetCatalogRequest>,
    ) -> Result<Response<persistence_pb::GetCatalogResponse>, Status> {
        self.0.next().await?;
        Ok(Response::new(persistence_pb::GetCatalogResponse {
            catalog: Some(self.0.catalog.lock().unwrap().clone()),
        }))
    }
}

struct Harness {
    store: GrpcPersistenceStore,
    script: Arc<Script>,
    _stop: oneshot::Sender<()>,
}

const DEADLINE: Duration = Duration::from_millis(100);
const RETRY: Duration = Duration::from_millis(5);

fn policy() -> CallPolicy {
    CallPolicy {
        deadline: DEADLINE,
        retry_delays: vec![RETRY; 3],
    }
}

async fn start(steps: impl IntoIterator<Item = Step>) -> Harness {
    let script = Script::new(steps);
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let (stop, stopped) = oneshot::channel::<()>();
    let service = PersistenceServiceServer::new(FakeService(script.clone()));
    let catalog_service = CatalogServiceServer::new(FakeService(script.clone()));
    tokio::spawn(async move {
        Server::builder()
            .add_service(service)
            .add_service(catalog_service)
            .serve_with_incoming_shutdown(TcpListenerStream::new(listener), async {
                stopped.await.ok();
            })
            .await
            .unwrap();
    });
    let store = GrpcPersistenceStore::connect_lazy(&addr.to_string(), policy()).unwrap();
    Harness {
        store,
        script,
        _stop: stop,
    }
}

fn snapshot() -> persistence_pb::Snapshot {
    persistence_pb::Snapshot {
        tick: 50,
        ..Default::default()
    }
}

#[tokio::test]
async fn transient_failures_are_retried_until_success() {
    let harness = start([
        Step::Fail(Code::Unavailable),
        Step::Fail(Code::DeadlineExceeded),
        Step::Fail(Code::Unavailable),
    ])
    .await;

    let loaded = harness.store.load().await.unwrap();

    assert_eq!(loaded.map(|s| s.tick), Some(7));
    assert_eq!(harness.script.calls(), 4, "1 intento + 3 reintentos");
}

#[tokio::test]
async fn at_most_three_retries_then_unavailable() {
    let harness = start([Step::Fail(Code::Unavailable); 10]).await;

    let error = harness.store.save(snapshot()).await.unwrap_err();

    assert!(matches!(error, StoreError::Unavailable(_)), "{error:?}");
    assert_eq!(harness.script.calls(), 4, "1 intento + 3 reintentos");
}

#[tokio::test]
async fn non_transient_codes_are_not_retried() {
    for (code, expected) in [
        (Code::Internal, "Failed"),
        (Code::InvalidArgument, "Failed"),
        (Code::AlreadyExists, "AlreadyExists"),
        (Code::Cancelled, "Failed"),
    ] {
        let harness = start([Step::Fail(code), Step::Fail(code)]).await;

        let error = harness.store.save(snapshot()).await.unwrap_err();

        let kind = match error {
            StoreError::Failed(_) => "Failed",
            StoreError::AlreadyExists(_) => "AlreadyExists",
            StoreError::Unavailable(_) => "Unavailable",
        };
        assert_eq!(kind, expected, "{code:?}");
        assert_eq!(harness.script.calls(), 1, "{code:?} no se reintenta");
    }
}

#[tokio::test]
async fn not_found_on_load_is_an_empty_store_without_retry() {
    let harness = start([Step::Fail(Code::NotFound)]).await;

    assert_eq!(harness.store.load().await.unwrap(), None);
    assert_eq!(harness.script.calls(), 1);
}

#[tokio::test]
async fn a_slow_attempt_is_cut_at_the_deadline_and_retried() {
    let harness = start([Step::Delay(DEADLINE * 10)]).await;
    let started = Instant::now();

    let loaded = harness.store.load().await.unwrap();

    assert_eq!(loaded.map(|s| s.tick), Some(7));
    assert_eq!(harness.script.calls(), 2);
    assert!(
        started.elapsed() < DEADLINE * 5,
        "el intento lento no se cortó en su plazo: {:?}",
        started.elapsed()
    );
}

#[tokio::test]
async fn attempts_that_always_time_out_end_as_unavailable_after_three_retries() {
    let harness = start([Step::Delay(DEADLINE * 10); 10]).await;
    let started = Instant::now();

    let error = harness.store.save(snapshot()).await.unwrap_err();

    assert!(matches!(error, StoreError::Unavailable(_)), "{error:?}");
    assert_eq!(harness.script.calls(), 4, "1 intento + 3 reintentos");
    // 4 plazos + 3 esperas, con holgura; muy por debajo de 4 × 1 s.
    assert!(
        started.elapsed() < DEADLINE * 8,
        "cada intento debe cortarse en su plazo: {:?}",
        started.elapsed()
    );
}

// --- GetCatalog ---------------------------------------------------------------

#[tokio::test]
async fn get_catalog_returns_the_validated_catalog() {
    let h = start([]).await;
    let catalog = h.store.get_catalog().await.unwrap();

    assert_eq!(catalog.version, common::CATALOG_VERSION);
    assert_eq!(catalog.drop.humidity, 25);
    assert_eq!(catalog.tank.capacity, 20);
    assert_eq!(catalog.offline_installation_seconds, 7200);
    assert_eq!(h.script.calls(), 1);
}

#[tokio::test]
async fn get_catalog_retries_transient_failures() {
    let h = start([
        Step::Fail(Code::Unavailable),
        Step::Fail(Code::DeadlineExceeded),
    ])
    .await;
    assert!(h.store.get_catalog().await.is_ok());
    assert_eq!(h.script.calls(), 3);
}

#[tokio::test]
async fn get_catalog_gives_up_after_the_retries_with_unavailable() {
    let h = start([Step::Fail(Code::Unavailable); 4]).await;
    assert!(matches!(
        h.store.get_catalog().await,
        Err(StoreError::Unavailable(_))
    ));
    // Un intento y tres reintentos.
    assert_eq!(h.script.calls(), 4);
}

#[tokio::test]
async fn get_catalog_does_not_retry_other_errors() {
    let h = start([Step::Fail(Code::Internal)]).await;
    assert!(matches!(
        h.store.get_catalog().await,
        Err(StoreError::Failed(_))
    ));
    assert_eq!(h.script.calls(), 1);
}

#[tokio::test]
async fn get_catalog_rejects_an_unusable_catalog() {
    let h = start([]).await;
    h.script.catalog.lock().unwrap().growth = None;
    let error = h.store.get_catalog().await.unwrap_err();
    assert!(matches!(&error, StoreError::Failed(message) if message.contains("growth")));
}
