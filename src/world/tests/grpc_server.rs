//! Prueba de integración: el servidor gRPC del simulador, en el mismo
//! proceso, contra una persistencia falsa servida también con tonic.

use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use prost::Message;
use tokio::net::TcpListener;
use tokio::sync::oneshot;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::{Channel, Server};
use tonic::{Code, Request, Response, Status};
use tonic_health::pb::HealthCheckRequest;
use tonic_health::pb::health_check_response::ServingStatus;
use tonic_health::pb::health_client::HealthClient;

mod common;

use world::proto::persistence::v1 as persistence_pb;
use world::proto::persistence::v1::catalog_service_server::{CatalogService, CatalogServiceServer};
use world::proto::persistence::v1::persistence_service_server::{
    PersistenceService, PersistenceServiceServer,
};
use world::proto::world::v1 as pb;
use world::proto::world::v1::world_service_client::WorldServiceClient;
use world::server::{Settings, WORLD_SERVICE_NAME, serve};
use world::simulator::Backoff;
use world::store::CallPolicy;

const ANA: &str = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01";

#[derive(Clone)]
enum LoadMode {
    NotFound,
    Unavailable,
    Found(persistence_pb::Snapshot),
}

struct FakePersistence {
    mode: Mutex<LoadMode>,
    /// Si es `false`, GetCatalog responde UNAVAILABLE.
    catalog_available: AtomicBool,
    catalog_calls: AtomicUsize,
    load_calls: AtomicUsize,
    saved: Mutex<Vec<persistence_pb::Snapshot>>,
    /// Metadato `x-request-id` de cada llamada recibida, en orden.
    request_ids: Mutex<Vec<(&'static str, Option<String>)>>,
}

impl FakePersistence {
    fn record(&self, method: &'static str, metadata: &tonic::metadata::MetadataMap) {
        let id = metadata
            .get("x-request-id")
            .and_then(|v| v.to_str().ok())
            .map(str::to_string);
        self.request_ids.lock().unwrap().push((method, id));
    }

    fn ids_of(&self, method: &str) -> Vec<Option<String>> {
        let all = self.request_ids.lock().unwrap();
        all.iter()
            .filter(|(m, _)| *m == method)
            .map(|(_, id)| id.clone())
            .collect()
    }

    fn new(mode: LoadMode) -> Arc<Self> {
        Arc::new(Self {
            mode: Mutex::new(mode),
            catalog_available: AtomicBool::new(true),
            catalog_calls: AtomicUsize::new(0),
            load_calls: AtomicUsize::new(0),
            saved: Mutex::new(Vec::new()),
            request_ids: Mutex::new(Vec::new()),
        })
    }

    fn saved_ticks(&self) -> Vec<u64> {
        self.saved.lock().unwrap().iter().map(|s| s.tick).collect()
    }
}

#[derive(Clone)]
struct FakeService(Arc<FakePersistence>);

#[tonic::async_trait]
impl PersistenceService for FakeService {
    async fn save_snapshot(
        &self,
        request: Request<persistence_pb::SaveSnapshotRequest>,
    ) -> Result<Response<persistence_pb::SaveSnapshotResponse>, Status> {
        self.0.record("SaveSnapshot", request.metadata());
        let snapshot = request
            .into_inner()
            .snapshot
            .ok_or_else(|| Status::invalid_argument("falta snapshot"))?;
        self.0.saved.lock().unwrap().push(snapshot);
        Ok(Response::new(persistence_pb::SaveSnapshotResponse {}))
    }

    async fn load_snapshot(
        &self,
        request: Request<persistence_pb::LoadSnapshotRequest>,
    ) -> Result<Response<persistence_pb::LoadSnapshotResponse>, Status> {
        self.0.record("LoadSnapshot", request.metadata());
        self.0.load_calls.fetch_add(1, Ordering::SeqCst);
        let mode = self.0.mode.lock().unwrap().clone();
        match mode {
            LoadMode::NotFound => Err(Status::not_found("no hay ninguna instantánea")),
            LoadMode::Unavailable => Err(Status::unavailable("sin base de datos")),
            LoadMode::Found(snapshot) => Ok(Response::new(persistence_pb::LoadSnapshotResponse {
                snapshot: Some(snapshot),
            })),
        }
    }
}

#[tonic::async_trait]
impl CatalogService for FakeService {
    async fn get_catalog(
        &self,
        request: Request<persistence_pb::GetCatalogRequest>,
    ) -> Result<Response<persistence_pb::GetCatalogResponse>, Status> {
        self.0.record("GetCatalog", request.metadata());
        self.0.catalog_calls.fetch_add(1, Ordering::SeqCst);
        if !self.0.catalog_available.load(Ordering::SeqCst) {
            return Err(Status::unavailable("sin base de datos"));
        }
        Ok(Response::new(persistence_pb::GetCatalogResponse {
            catalog: Some(common::catalog()),
        }))
    }
}

struct Harness {
    world: WorldServiceClient<Channel>,
    health: HealthClient<Channel>,
    persistence: Arc<FakePersistence>,
    stop_world: oneshot::Sender<()>,
    server: tokio::task::JoinHandle<()>,
    _stop_fake: oneshot::Sender<()>,
}

async fn listener() -> (TcpListener, SocketAddr) {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    (listener, addr)
}

async fn start(mode: LoadMode) -> Harness {
    start_with(FakePersistence::new(mode)).await
}

async fn start_with(persistence: Arc<FakePersistence>) -> Harness {
    let (fake_listener, fake_addr) = listener().await;
    let (stop_fake, fake_stopped) = oneshot::channel::<()>();
    let service = PersistenceServiceServer::new(FakeService(persistence.clone()));
    let catalog_service = CatalogServiceServer::new(FakeService(persistence.clone()));
    tokio::spawn(async move {
        Server::builder()
            .add_service(service)
            .add_service(catalog_service)
            .serve_with_incoming_shutdown(TcpListenerStream::new(fake_listener), async {
                fake_stopped.await.ok();
            })
            .await
            .unwrap();
    });

    let (world_listener, world_addr) = listener().await;
    let (stop_world, world_stopped) = oneshot::channel::<()>();
    let settings = Settings {
        persistence_addr: fake_addr.to_string(),
        call_policy: CallPolicy {
            deadline: Duration::from_millis(500),
            retry_delays: vec![Duration::from_millis(10)],
        },
        load_backoff: Backoff {
            initial: Duration::from_millis(10),
            max: Duration::from_millis(50),
        },
        shutdown_timeout: Duration::from_secs(5),
        seed: 7,
    };
    let server = tokio::spawn(async move {
        serve(settings, world_listener, async {
            world_stopped.await.ok();
        })
        .await
        .unwrap();
    });

    let channel = Channel::from_shared(format!("http://{world_addr}"))
        .unwrap()
        .connect()
        .await
        .unwrap();
    Harness {
        world: WorldServiceClient::new(channel.clone()),
        health: HealthClient::new(channel),
        persistence,
        stop_world,
        server,
        _stop_fake: stop_fake,
    }
}

impl Harness {
    async fn ranking(&mut self, limit: u32) -> Result<pb::GetRankingResponse, Status> {
        self.world
            .get_ranking(pb::GetRankingRequest { limit })
            .await
            .map(Response::into_inner)
    }

    async fn step(&mut self, commands: Vec<pb::Command>) -> Result<pb::StepResponse, Status> {
        self.world
            .step(pb::StepRequest { commands })
            .await
            .map(Response::into_inner)
    }

    /// Primer Step correcto: espera a que la salud diga SERVING (el mundo
    /// ya está cargado) y solo entonces llama a Step, que debe responder.
    /// Así las comprobaciones de salud posteriores no compiten con la tarea
    /// que la actualiza.
    async fn first_step(&mut self) -> pb::StepResponse {
        self.wait_serving().await;
        self.step(Vec::new())
            .await
            .expect("Step debe responder con la salud en SERVING")
    }

    async fn wait_serving(&mut self) {
        for _ in 0..400 {
            if self.health(WORLD_SERVICE_NAME).await == ServingStatus::Serving
                && self.health("").await == ServingStatus::Serving
            {
                return;
            }
            tokio::time::sleep(Duration::from_millis(5)).await;
        }
        panic!("la salud nunca llegó a SERVING");
    }

    /// Envía la señal de parada, cierra los clientes y espera a que
    /// `serve` termine.
    async fn stop(self) {
        let Harness {
            world,
            health,
            stop_world,
            server,
            ..
        } = self;
        drop((world, health));
        stop_world.send(()).unwrap();
        tokio::time::timeout(Duration::from_secs(10), server)
            .await
            .expect("serve debe terminar")
            .unwrap();
    }

    async fn health(&mut self, service: &str) -> ServingStatus {
        self.health
            .check(HealthCheckRequest {
                service: service.to_string(),
            })
            .await
            .unwrap()
            .into_inner()
            .status()
    }
}

fn drop_command(n: u32, x: u32, y: u32) -> pb::Command {
    pb::Command {
        action_id: format!("00000000-0000-4000-8000-{n:012}"),
        player_id: ANA.to_string(),
        kind: Some(pb::command::Kind::Drop(pb::DropCommand { x, y })),
    }
}

async fn eventually(what: &str, condition: impl Fn() -> bool) {
    for _ in 0..400 {
        if condition() {
            return;
        }
        tokio::time::sleep(Duration::from_millis(5)).await;
    }
    panic!("no se cumplió: {what}");
}

#[tokio::test]
async fn starts_empty_at_tick_zero_when_there_is_no_snapshot() {
    let mut h = start(LoadMode::NotFound).await;
    let response = h.first_step().await;

    assert_eq!(response.tick, 1);
    assert_eq!(response.restored_tick, 0);
    assert!(response.results.is_empty());
    let state = response.state.expect("state");
    assert_eq!((state.width, state.height), (32, 32));
    assert_eq!(state.tiles.len(), 1024);
    assert!(state.players.is_empty());
    assert_eq!(h.health(WORLD_SERVICE_NAME).await, ServingStatus::Serving);
    assert_eq!(h.health("").await, ServingStatus::Serving);
}

#[tokio::test]
async fn step_applies_a_drop() {
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await;

    let response = h
        .step(vec![drop_command(1, 14, 9), drop_command(2, 32, 0)])
        .await
        .unwrap();
    assert_eq!(response.tick, 2);
    let statuses: Vec<_> = response
        .results
        .iter()
        .map(|r| (r.status(), r.reason()))
        .collect();
    assert_eq!(
        statuses,
        vec![
            (pb::CommandStatus::Applied, pb::RejectReason::Unspecified),
            (pb::CommandStatus::Rejected, pb::RejectReason::OutOfBounds),
        ]
    );
    let state = response.state.unwrap();
    assert_eq!(state.tiles[9 * 32 + 14].humidity, 25);
    assert_eq!(state.humidity_contributed, 25);
    assert_eq!(state.players.len(), 1);
    assert_eq!(state.players[0].water, 19);

    let again = h.step(vec![drop_command(1, 14, 9)]).await.unwrap();
    assert_eq!(again.results[0].status(), pb::CommandStatus::Duplicate);
}

#[tokio::test]
async fn boot_id_is_a_uuid_and_stable_across_steps() {
    let mut h = start(LoadMode::NotFound).await;
    let first = h.first_step().await;
    let second = h.step(Vec::new()).await.unwrap();

    assert!(uuid::Uuid::parse_str(&first.boot_id).is_ok());
    assert_eq!(first.boot_id, second.boot_id);
    assert_eq!(second.restored_tick, first.restored_tick);
}

/// RNF-07: cada llamada del simulador a la persistencia lleva su propio
/// `x-request-id` (`world-<uuid>`), para correlacionar los dos registros.
#[tokio::test]
async fn every_persistence_call_carries_its_own_request_id() {
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await;
    h.step(vec![drop_command(1, 0, 0)]).await.unwrap();
    for _ in 3..=50 {
        h.step(Vec::new()).await.unwrap();
    }
    let persistence = h.persistence.clone();
    eventually("SaveSnapshot del tick 50", || {
        !persistence.ids_of("SaveSnapshot").is_empty()
    })
    .await;

    let mut seen = Vec::new();
    for method in ["GetCatalog", "LoadSnapshot", "SaveSnapshot"] {
        let ids = persistence.ids_of(method);
        assert!(!ids.is_empty(), "{method} no llegó");
        for id in ids {
            let id = id.unwrap_or_else(|| panic!("{method} sin x-request-id"));
            assert!(id.starts_with("world-"), "{method}: {id}");
            assert!(world::request_id::is_safe(&id), "{method}: {id}");
            seen.push(id);
        }
    }
    let mut unique = seen.clone();
    unique.sort();
    unique.dedup();
    assert_eq!(
        unique.len(),
        seen.len(),
        "los ids deben ser distintos por llamada"
    );
    h.stop().await;
}

#[tokio::test]
async fn a_snapshot_is_saved_at_tick_50() {
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await;
    h.step(vec![drop_command(1, 0, 0)]).await.unwrap();
    for _ in 3..50 {
        h.step(Vec::new()).await.unwrap();
    }
    assert!(h.persistence.saved_ticks().is_empty());

    let response = h.step(Vec::new()).await.unwrap();
    assert_eq!(response.tick, 50);
    let persistence = h.persistence.clone();
    eventually("SaveSnapshot del tick 50", || {
        persistence.saved_ticks() == vec![50]
    })
    .await;

    let saved = h.persistence.saved.lock().unwrap()[0].clone();
    assert_eq!(saved.epoch, 0);
    assert_eq!(saved.catalog_version, common::CATALOG_VERSION);
    assert_eq!(saved.players.len(), 1);
    let inner = pb::WorldSnapshot::decode(saved.world_state.as_slice()).unwrap();
    assert_eq!(inner.tick, 50);
    assert_eq!(inner.catalog_version, common::CATALOG_VERSION);
    assert_eq!(inner.humidity_contributed, 25);
    assert_eq!(inner.players[0].recent_actions.len(), 1);
}

#[tokio::test]
async fn restores_the_latest_snapshot_at_startup() {
    let mut first = start(LoadMode::NotFound).await;
    first.first_step().await;
    first.step(vec![drop_command(1, 3, 3)]).await.unwrap();
    for _ in 3..=50 {
        first.step(Vec::new()).await.unwrap();
    }
    let persistence = first.persistence.clone();
    eventually("SaveSnapshot del tick 50", || {
        persistence.saved_ticks() == vec![50]
    })
    .await;
    let snapshot = first.persistence.saved.lock().unwrap()[0].clone();

    let mut second = start(LoadMode::Found(snapshot)).await;
    let response = second.first_step().await;
    assert_eq!(response.tick, 51);
    assert_eq!(response.restored_tick, 50);
    assert_ne!(
        response.boot_id,
        first.step(Vec::new()).await.unwrap().boot_id
    );
    // 25 de la gota menos 5 evaporaciones de 2 (ticks 10, 20, 30, 40 y 50).
    assert_eq!(response.state.unwrap().tiles[3 * 32 + 3].humidity, 15);

    let again = second.step(vec![drop_command(1, 3, 3)]).await.unwrap();
    assert_eq!(again.results[0].status(), pb::CommandStatus::Duplicate);
}

#[tokio::test]
async fn step_is_unavailable_until_persistence_answers_then_recovers() {
    let mut h = start(LoadMode::Unavailable).await;
    let persistence = h.persistence.clone();
    eventually("varios LoadSnapshot", || {
        persistence.load_calls.load(Ordering::SeqCst) >= 3
    })
    .await;

    let status = h.step(Vec::new()).await.unwrap_err();
    assert_eq!(status.code(), Code::Unavailable);
    assert_eq!(
        h.health(WORLD_SERVICE_NAME).await,
        ServingStatus::NotServing
    );
    assert_eq!(h.health("").await, ServingStatus::NotServing);

    *h.persistence.mode.lock().unwrap() = LoadMode::NotFound;
    let response = h.first_step().await;
    assert_eq!(response.tick, 1);
    assert_eq!(response.restored_tick, 0);
    assert_eq!(h.health(WORLD_SERVICE_NAME).await, ServingStatus::Serving);
}

#[tokio::test]
async fn shutdown_saves_a_final_snapshot_of_the_last_tick() {
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await; // tick 1
    h.step(vec![drop_command(1, 2, 2)]).await.unwrap(); // tick 2
    h.step(Vec::new()).await.unwrap(); // tick 3
    assert!(h.persistence.saved_ticks().is_empty());

    let persistence = h.persistence.clone();
    h.stop().await;
    assert_eq!(persistence.saved_ticks(), vec![3]);
    let saved = persistence.saved.lock().unwrap()[0].clone();
    let inner = pb::WorldSnapshot::decode(saved.world_state.as_slice()).unwrap();
    assert_eq!(inner.humidity_contributed, 25);
}

#[tokio::test]
async fn the_world_is_not_ready_until_the_catalog_is_available() {
    // La persistencia falsa arranca con GetCatalog caído.
    let persistence = FakePersistence::new(LoadMode::NotFound);
    persistence.catalog_available.store(false, Ordering::SeqCst);
    let mut h = start_with(persistence).await;
    let persistence = h.persistence.clone();
    eventually("varios GetCatalog", || {
        persistence.catalog_calls.load(Ordering::SeqCst) >= 3
    })
    .await;

    let status = h.step(Vec::new()).await.unwrap_err();
    assert_eq!(status.code(), Code::Unavailable);
    assert_eq!(h.health("").await, ServingStatus::NotServing);
    // Sin catálogo no se toca la instantánea: el tick nunca retrocede.
    assert_eq!(h.persistence.load_calls.load(Ordering::SeqCst), 0);

    h.persistence
        .catalog_available
        .store(true, Ordering::SeqCst);
    let response = h.first_step().await;
    assert_eq!(response.tick, 1);
    assert_eq!(h.persistence.load_calls.load(Ordering::SeqCst), 1);
    h.stop().await;
}

fn admin_command(n: u32, kind: pb::command::Kind) -> pb::Command {
    pb::Command {
        action_id: format!("00000000-0000-4000-8000-{n:012}"),
        player_id: ANA.to_string(),
        kind: Some(kind),
    }
}

#[tokio::test]
async fn admin_commands_switch_demo_mode_and_reset_the_valley_refetching_the_catalog() {
    use pb::command::Kind;
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await;
    assert_eq!(h.persistence.catalog_calls.load(Ordering::SeqCst), 1);

    let demo = h
        .step(vec![
            admin_command(
                1,
                Kind::SetDemoMode(pb::SetDemoModeCommand { enabled: true }),
            ),
            drop_command(2, 3, 3),
        ])
        .await
        .unwrap();
    assert!(
        demo.results
            .iter()
            .all(|r| r.status() == pb::CommandStatus::Applied)
    );
    let state = demo.state.unwrap();
    assert!(state.demo_mode);
    assert_eq!(state.tiles[3 * 32 + 3].humidity, 1);
    assert_eq!(state.humidity_contributed, 1);
    assert_eq!(h.persistence.catalog_calls.load(Ordering::SeqCst), 1);

    let reset = h
        .step(vec![admin_command(
            3,
            Kind::ResetValley(pb::ResetValleyCommand {}),
        )])
        .await
        .unwrap();
    assert_eq!(reset.results[0].status(), pb::CommandStatus::Applied);
    assert_eq!(reset.tick, demo.tick + 1, "the tick keeps growing");
    let state = reset.state.unwrap();
    assert_eq!(state.epoch, 1);
    assert_eq!(state.tiles[3 * 32 + 3].humidity, 0);
    assert_eq!(
        h.persistence.catalog_calls.load(Ordering::SeqCst),
        2,
        "the reset asks for the catalog again"
    );
}

const BOB: &str = "0a0a0a0a-2d0e-4f4e-9c1e-0c6c2d9b8a02";
const CARL: &str = "ffffffff-2d0e-4f4e-9c1e-0c6c2d9b8a03";

/// `count` gotas de `player` sobre la casilla `(x, y)`; con 4 gotas de 25
/// la casilla pasa a pasto y quien la hizo crecer gana 1 hoja.
fn drops_by(player: &str, first: u32, count: u32, x: u32, y: u32) -> Vec<pb::Command> {
    (first..first + count)
        .map(|n| pb::Command {
            action_id: format!("00000000-0000-4000-8000-{n:012}"),
            player_id: player.to_string(),
            kind: Some(pb::command::Kind::Drop(pb::DropCommand { x, y })),
        })
        .collect()
}

#[tokio::test]
async fn ranking_is_unavailable_until_the_world_is_loaded() {
    let mut h = start(LoadMode::Unavailable).await;
    let status = h.ranking(0).await.unwrap_err();
    assert_eq!(status.code(), Code::Unavailable);
}

#[tokio::test]
async fn ranking_of_an_empty_valley_is_empty_and_carries_the_tick() {
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await;
    let response = h.ranking(0).await.unwrap();
    assert_eq!(response.tick, 1);
    assert!(response.entries.is_empty());
}

#[tokio::test]
async fn ranking_orders_by_leaves_earned_with_ties_sharing_the_rank() {
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await;
    let mut commands = drops_by(ANA, 1, 8, 0, 0); // 4 gotas: pasto (+1 hoja), 8: arbusto (+2)
    commands.extend(drops_by(BOB, 100, 4, 5, 5)); // 1 hoja
    commands.extend(drops_by(CARL, 200, 1, 9, 9)); // 0 hojas
    h.step(commands).await.unwrap();

    let response = h.ranking(0).await.unwrap();
    assert_eq!(response.tick, 2);
    let rows: Vec<_> = response
        .entries
        .iter()
        .map(|e| (e.rank, e.player_id.as_str(), e.leaves_earned))
        .collect();
    assert_eq!(rows, vec![(1, ANA, 3), (2, BOB, 1), (3, CARL, 0)]);
}

#[tokio::test]
async fn ranking_applies_the_limit_and_does_not_mutate_the_world() {
    let mut h = start(LoadMode::NotFound).await;
    h.first_step().await;
    let mut commands = drops_by(ANA, 1, 4, 0, 0);
    commands.extend(drops_by(BOB, 100, 4, 5, 5));
    commands.extend(drops_by(CARL, 200, 1, 9, 9));
    h.step(commands).await.unwrap();

    let top = h.ranking(2).await.unwrap();
    // Empate ANA/BOB con 1 hoja: ambos primeros; manda el player_id (BOB < ANA).
    let rows: Vec<_> = top
        .entries
        .iter()
        .map(|e| (e.rank, e.player_id.as_str()))
        .collect();
    assert_eq!(rows, vec![(1, BOB), (1, ANA)]);

    // Leer el ranking no avanza el tick.
    assert_eq!(h.ranking(0).await.unwrap().tick, top.tick);
    assert_eq!(h.step(Vec::new()).await.unwrap().tick, top.tick + 1);
}
