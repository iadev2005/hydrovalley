//! Montaje del servidor gRPC: WorldService, salud estándar y carga del
//! mundo en segundo plano.

use std::future::Future;
use std::sync::Arc;
use std::time::Duration;

use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tonic_health::ServingStatus;

use crate::domain::Rules;
use crate::grpc::WorldGrpc;
use crate::proto::world::v1::world_service_server::WorldServiceServer;
use crate::simulator::{Backoff, Simulator};
use crate::store::{CallPolicy, GrpcSnapshotStore};

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
}

impl Settings {
    /// Plazos y reintentos de CLAUDE.md.
    pub fn new(persistence_addr: impl Into<String>) -> Self {
        Self {
            persistence_addr: persistence_addr.into(),
            call_policy: CallPolicy::default(),
            load_backoff: Backoff::default(),
            shutdown_timeout: Duration::from_secs(5),
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
/// el mundo se carga, Step responde UNAVAILABLE y la salud está en
/// NOT_SERVING.
pub async fn serve(
    settings: Settings,
    listener: TcpListener,
    shutdown: impl Future<Output = ()> + Send,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let store = Arc::new(GrpcSnapshotStore::connect_lazy(
        &settings.persistence_addr,
        settings.call_policy.clone(),
    )?);
    let simulator = Arc::new(Simulator::new(Rules::initial(), store));
    tracing::info!(boot_id = simulator.boot_id(), "simulador creado");

    let (reporter, health_service) = tonic_health::server::health_reporter();
    for name in ["", WORLD_SERVICE_NAME] {
        reporter
            .set_service_status(name, ServingStatus::NotServing)
            .await;
    }

    let loader = {
        let simulator = Arc::clone(&simulator);
        let backoff = settings.load_backoff;
        tokio::spawn(async move {
            simulator.load(backoff).await;
            for name in ["", WORLD_SERVICE_NAME] {
                reporter
                    .set_service_status(name, ServingStatus::Serving)
                    .await;
            }
            tracing::info!("mundo listo: Step atiende");
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
