//! Implementación de `WorldService` (la frontera gRPC).
//!
//! Solo traduce: mensajes del contrato → dominio → mensajes del contrato.

use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::codec;
use crate::domain::rank_players;
use crate::proto::world::v1 as pb;
use crate::proto::world::v1::world_service_server::WorldService;
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

#[tonic::async_trait]
impl<S: PersistenceStore> WorldService for WorldGrpc<S> {
    async fn step(
        &self,
        request: Request<pb::StepRequest>,
    ) -> Result<Response<pb::StepResponse>, Status> {
        let commands: Vec<_> = request
            .into_inner()
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
            boot_id: self.simulator.boot_id().to_string(),
        }))
    }

    /// RF-10 y RF-11: solo lee. Copia los puntajes con el bloqueo tomado y
    /// ordena después, con el bloqueo ya suelto, así `Step` no espera.
    async fn get_ranking(
        &self,
        request: Request<pb::GetRankingRequest>,
    ) -> Result<Response<pb::GetRankingResponse>, Status> {
        let limit = request.into_inner().limit;
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
