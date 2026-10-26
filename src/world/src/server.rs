//! Montaje del servidor gRPC: WorldService, salud estándar y carga del
//! catálogo y del mundo en segundo plano.

use std::future::Future;
use std::sync::Arc;
use std::time::Duration;

use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tonic_health::ServingStatus;

use crate::config::{WorldMode, seed_from_clock};
use crate::grpc::WorldGrpc;
use crate::proto::world::v1::world_service_server::WorldServiceServer;
use crate::simulator::{Backoff, Simulator};
use crate::store::{CallPolicy, GrpcPersistenceStore};

/// Nombre completo del servicio para `grpc.health.v1.Health`.
pub const WORLD_SERVICE_NAME: &str = "hydrovalley.world.v1.WorldService";

#[derive(Debug, Clone)]
pub struct Settings {
    pub persistence_addr: String,
    pub call_policy: CallPolicy,
    pub load_backoff: Backoff,
    /// Plazo de la parada ordenada: esperar al guardado en curso y enviar
    /// la instantánea final.
    pub shutdown_timeout: Duration,
    /// Semilla del azar para un valle que empieza de cero (`WORLD_SEED`).
    pub seed: u64,
    /// Modo de arranque (`WORLD_MODE=active|standby`).
    pub mode: WorldMode,
}

impl Settings {
    /// Plazos y reintentos de CLAUDE.md.
    pub fn new(persistence_addr: impl Into<String>) -> Self {
        Self {
            persistence_addr: persistence_addr.into(),
            call_policy: CallPolicy::default(),
            load_backoff: Backoff::default(),
            shutdown_timeout: Duration::from_secs(5),
            seed: seed_from_clock(),
            mode: WorldMode::Active,
        }
    }
}

/// Sirve en `listener` hasta que `shutdown` termine.
///
/// Al terminar `shutdown`, el servidor deja de aceptar peticiones y espera a
/// las que están en curso; después el simulador espera al guardado en curso
/// y envía una instantánea final si el tick avanzó desde la última
/// guardada, con un plazo de `settings.shutdown_timeout`.
///
/// El gRPC atiende desde el principio (regla 5 de portabilidad): mientras
/// el catálogo y el mundo se cargan, Step responde UNAVAILABLE y la salud está en
/// NOT_SERVING.
pub async fn serve(
    settings: Settings,
    listener: TcpListener,
    shutdown: impl Future<Output = ()> + Send,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let store = Arc::new(GrpcPersistenceStore::connect_lazy(
        &settings.persistence_addr,
        settings.call_policy.clone(),
    )?);
    let simulator = match settings.mode {
        WorldMode::Active => Arc::new(Simulator::new(store, settings.seed)),
        WorldMode::Standby => Arc::new(Simulator::new_standby(
            store,
            settings.seed,
            settings.load_backoff,
        )),
    };
    tracing::info!(
        boot_id = %simulator.boot_id(),
        mode = ?settings.mode,
        "simulador creado"
    );

    let (reporter, health_service) = tonic_health::server::health_reporter();
    for name in ["", WORLD_SERVICE_NAME] {
        reporter
            .set_service_status(name, ServingStatus::NotServing)
            .await;
    }

    let loader = {
        let simulator = Arc::clone(&simulator);
        let backoff = settings.load_backoff;
        let mode = settings.mode;
        tokio::spawn(async move {
            if mode == WorldMode::Active {
                simulator.load(backoff).await;
            }
            loop {
                let status = if simulator.is_ready() || simulator.is_standby() {
                    ServingStatus::Serving
                } else {
                    ServingStatus::NotServing
                };
                for name in ["", WORLD_SERVICE_NAME] {
                    reporter.set_service_status(name, status).await;
                }
                tokio::time::sleep(Duration::from_millis(200)).await;
            }
        })
    };

    let result = Server::builder()
        .add_service(health_service)
        .add_service(WorldServiceServer::new(WorldGrpc::new(Arc::clone(
            &simulator,
        ))))
        .serve_with_incoming_shutdown(TcpListenerStream::new(listener), shutdown)
        .await;
    loader.abort();
    simulator.shutdown(settings.shutdown_timeout).await;
    result.map_err(Into::into)
}
