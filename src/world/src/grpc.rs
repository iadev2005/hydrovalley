//! Implementación de `WorldService` (la frontera gRPC).
//!
//! Solo traduce: mensajes del contrato → dominio → mensajes del contrato.

use std::sync::Arc;

use tonic::{Request, Response, Status};
use tracing::Instrument;

use crate::codec;
use crate::domain::{StepOutcome, rank_players};
use crate::proto::world::v1 as pb;
use crate::proto::world::v1::world_service_server::WorldService;
use crate::request_id;
use crate::simulator::Simulator;
use crate::store::PersistenceStore;

pub struct WorldGrpc<S> {
    simulator: Arc<Simulator<S>>,
}

impl<S> WorldGrpc<S> {
    pub fn new(simulator: Arc<Simulator<S>>) -> Self {
        Self { simulator }
    }
}

impl<S: PersistenceStore> WorldGrpc<S> {
    async fn step_inner(
        &self,
        request: Request<pb::StepRequest>,
    ) -> Result<Response<pb::StepResponse>, Status> {
        let inner = request.into_inner();
        self.simulator
            .check_hub_generation(inner.hub_generation)
            .map_err(|_| {
                Status::failed_precondition("hub desplazado por una generación superior")
            })?;
        let min_expected_tick = inner.min_expected_tick;
        self.simulator
            .wait_active_and_fresh(min_expected_tick)
            .await
            .map_err(|_| Status::unavailable("el simulador todavía está cargando el mundo"))?;
        let commands: Vec<_> = inner
            .commands
            .into_iter()
            .map(codec::command_from_proto)
            .collect();
        // Solo con un ResetValley vuelve a pedir el catálogo (RF-31).
        let fresh_catalog = self.simulator.catalog_for(&commands).await;
        let report = self
            .simulator
            .step_with(&commands, fresh_catalog, codec::world_state)
            .map_err(|_| Status::unavailable("el simulador todavía está cargando el mundo"))?;
        log_step(&report.outcome);
        Ok(Response::new(pb::StepResponse {
            tick: report.outcome.tick,
            restored_tick: report.restored_tick,
            results: report
                .outcome
                .results
                .into_iter()
                .map(codec::result_to_proto)
                .collect(),
            state: Some(report.view),
            boot_id: report.boot_id,
        }))
    }
}

/// Resultado del paso y de cada comando, a nivel `debug`. Sin `debug`
/// activado ni siquiera se recorren los resultados.
fn log_step(outcome: &StepOutcome) {
    if !tracing::enabled!(tracing::Level::DEBUG) {
        return;
    }
    tracing::debug!(
        tick = outcome.tick,
        commands = outcome.results.len(),
        "Step"
    );
    for result in &outcome.results {
        tracing::debug!(action_id = %result.action_id, outcome = ?result.outcome, "comando");
    }
}

#[tonic::async_trait]
impl<S: PersistenceStore> WorldService for WorldGrpc<S> {
    /// El id de petición (RNF-07) es el metadato `x-request-id` que envía el
    /// hub (`tick-<n>`); va como campo del *span* de la llamada, así que todas
    /// sus líneas lo llevan. Todo el registro del camino caliente es a nivel
    /// `debug`: con `info` el `Step` no escribe nada.
    async fn step(
        &self,
        request: Request<pb::StepRequest>,
    ) -> Result<Response<pb::StepResponse>, Status> {
        let span = tracing::debug_span!("step", request_id = %request_id::from_metadata(request.metadata()));
        self.step_inner(request).instrument(span).await
    }

    /// RF-10 y RF-11: solo lee. Copia los puntajes con el bloqueo tomado y
    /// ordena después, con el bloqueo ya suelto, así `Step` no espera.
    async fn get_ranking(
        &self,
        request: Request<pb::GetRankingRequest>,
    ) -> Result<Response<pb::GetRankingResponse>, Status> {
        let span = tracing::info_span!(
            "get_ranking",
            request_id = %request_id::from_metadata(request.metadata())
        );
        let _entered = span.enter();
        let limit = request.into_inner().limit;
        tracing::info!(limit, "GetRanking");
        let (tick, scores) = self
            .simulator
            .read(|world| (world.tick(), world.scores()))
            .map_err(|_| Status::unavailable("el simulador todavía está cargando el mundo"))?;
        let entries = rank_players(scores, limit)
            .into_iter()
            .map(|entry| pb::RankingEntry {
                rank: entry.rank,
                player_id: entry.player_id,
                leaves_earned: entry.leaves_earned,
            })
            .collect();
        Ok(Response::new(pb::GetRankingResponse { tick, entries }))
    }
}

#[cfg(test)]
mod tests {
    use std::io;
    use std::sync::Mutex;

    use tracing::Level;

    use super::*;
    use crate::domain::Catalog;
    use crate::simulator::Backoff;
    use crate::store::{Snapshot, StoreError};

    const ANA: &str = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01";
    const ACTION: &str = "00000000-0000-4000-8000-000000000001";

    struct NoStore;

    impl PersistenceStore for NoStore {
        async fn get_catalog(&self) -> Result<Catalog, StoreError> {
            Ok(Catalog::d01())
        }
        async fn load(&self) -> Result<Option<Snapshot>, StoreError> {
            Ok(None)
        }
        async fn save(&self, _: Snapshot) -> Result<(), StoreError> {
            Ok(())
        }
    }

    /// Escritor de registros en memoria para inspeccionar lo que se escribe.
    #[derive(Clone, Default)]
    struct Captured(Arc<Mutex<Vec<u8>>>);

    impl io::Write for Captured {
        fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
            self.0.lock().unwrap().extend_from_slice(buf);
            Ok(buf.len())
        }
        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    impl Captured {
        fn text(&self) -> String {
            String::from_utf8(self.0.lock().unwrap().clone()).unwrap()
        }
    }

    fn capture(max: Level) -> (Captured, tracing::subscriber::DefaultGuard) {
        let out = Captured::default();
        let writer = out.clone();
        let subscriber = tracing_subscriber::fmt()
            .with_max_level(max)
            .with_ansi(false)
            .with_writer(move || writer.clone())
            .finish();
        (out, tracing::subscriber::set_default(subscriber))
    }

    async fn ready() -> WorldGrpc<NoStore> {
        let simulator = Arc::new(Simulator::new(Arc::new(NoStore), 7));
        simulator
            .load(Backoff {
                initial: std::time::Duration::from_millis(1),
                max: std::time::Duration::from_millis(1),
            })
            .await;
        WorldGrpc::new(simulator)
    }

    fn drop_request(id: Option<&str>) -> Request<pb::StepRequest> {
        let mut request = Request::new(pb::StepRequest {
            commands: vec![pb::Command {
                action_id: ACTION.to_string(),
                player_id: ANA.to_string(),
                kind: Some(pb::command::Kind::Drop(pb::DropCommand { x: 0, y: 0 })),
            }],
            hub_generation: 0,
            min_expected_tick: 0,
        });
        if let Some(id) = id {
            crate::request_id::tag(&mut request, id);
        }
        request
    }

    #[tokio::test]
    async fn step_logs_the_request_id_and_each_action_at_debug() {
        let grpc = ready().await;
        let (out, _guard) = capture(Level::DEBUG);
        grpc.step(drop_request(Some("tick-5"))).await.unwrap();
        let text = out.text();
        assert!(text.contains("request_id=tick-5"), "{text}");
        assert!(text.contains(ACTION), "{text}");
        assert!(text.contains("Applied"), "{text}");
        assert!(text.contains("tick=1"), "{text}");
    }

    #[tokio::test]
    async fn step_without_a_usable_id_logs_a_dash() {
        let grpc = ready().await;
        let (out, _guard) = capture(Level::DEBUG);
        grpc.step(drop_request(None)).await.unwrap();
        grpc.step(drop_request(Some("con espacio"))).await.unwrap();
        let text = out.text();
        assert!(text.matches("request_id=-").count() >= 2, "{text}");
        assert!(!text.contains("con espacio"), "{text}");
    }

    #[tokio::test]
    async fn step_logs_nothing_per_command_at_info() {
        let grpc = ready().await;
        let (out, _guard) = capture(Level::INFO);
        grpc.step(drop_request(Some("tick-6"))).await.unwrap();
        assert_eq!(
            out.text(),
            "",
            "el camino caliente no registra a nivel info"
        );
    }

    #[tokio::test]
    async fn get_ranking_logs_the_request_id_at_info() {
        let grpc = ready().await;
        let (out, _guard) = capture(Level::INFO);
        let mut request = Request::new(pb::GetRankingRequest { limit: 5 });
        crate::request_id::tag(&mut request, "3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b");
        grpc.get_ranking(request).await.unwrap();
        let text = out.text();
        assert!(
            text.contains("request_id=3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b"),
            "{text}"
        );
        assert!(text.contains("GetRanking"), "{text}");
    }

    #[tokio::test]
    async fn step_rejects_lower_hub_generation_with_failed_precondition() {
        let grpc = ready().await;
        let step_gen = |hub_generation: u64| {
            Request::new(pb::StepRequest {
                commands: Vec::new(),
                hub_generation,
                min_expected_tick: 0,
            })
        };
        let r1 = grpc.step(step_gen(1)).await.unwrap().into_inner();
        assert_eq!(r1.tick, 1);

        let r2 = grpc.step(step_gen(2)).await.unwrap().into_inner();
        assert_eq!(r2.tick, 2);

        let err = grpc.step(step_gen(1)).await.unwrap_err();
        assert_eq!(err.code(), tonic::Code::FailedPrecondition);

        let r3 = grpc.step(step_gen(2)).await.unwrap().into_inner();
        assert_eq!(r3.tick, 3);
    }
}
