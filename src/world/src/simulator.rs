//! Servicio de aplicación: guarda el mundo, decide cuándo está listo y
//! envía las instantáneas en segundo plano.
//!
//! No conoce gRPC más allá del puerto [`SnapshotStore`]: la capa de red
//! (`crate::grpc`) traduce las peticiones y llama aquí.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use uuid::Uuid;

use crate::codec;
use crate::domain::{Command, Rules, StepOutcome, World, is_snapshot_tick};
use crate::store::SnapshotStore;

/// Esperas entre intentos de `LoadSnapshot` al arrancar: 1 s, 2 s, 4 s y
/// luego 5 s indefinidamente.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Backoff {
    pub initial: Duration,
    pub max: Duration,
}

impl Default for Backoff {
    fn default() -> Self {
        Self {
            initial: Duration::from_secs(1),
            max: Duration::from_secs(5),
        }
    }
}

impl Backoff {
    /// Espera que sigue a `current`.
    pub fn next(&self, current: Duration) -> Duration {
        current.saturating_mul(2).min(self.max)
    }
}

/// El mundo todavía no se ha cargado: `Step` responde UNAVAILABLE.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct NotReady;

/// Lo que devuelve un paso, más lo que la capa de red quiera leer del
/// mundo mientras sigue bloqueado.
#[derive(Debug)]
pub struct StepReport<R> {
    pub outcome: StepOutcome,
    pub restored_tick: u64,
    pub view: R,
}

enum State {
    Loading,
    Ready { world: World, restored_tick: u64 },
}

pub struct Simulator<S> {
    boot_id: String,
    rules: Rules,
    store: Arc<S>,
    state: Mutex<State>,
    /// Hay un guardado en curso (como mucho uno).
    saving: Arc<AtomicBool>,
}

impl<S: SnapshotStore> Simulator<S> {
    pub fn new(rules: Rules, store: Arc<S>) -> Self {
        Self {
            boot_id: Uuid::new_v4().to_string(),
            rules,
            store,
            state: Mutex::new(State::Loading),
            saving: Arc::new(AtomicBool::new(false)),
        }
    }

    /// UUID v4 generado al crear el simulador; va en todas las respuestas.
    pub fn boot_id(&self) -> &str {
        &self.boot_id
    }

    pub fn is_ready(&self) -> bool {
        matches!(*self.lock(), State::Ready { .. })
    }

    fn lock(&self) -> std::sync::MutexGuard<'_, State> {
        // Un pánico con el bloqueo tomado no deja el mundo a medias: cada
        // paso modifica el mundo solo desde el dominio, que no entra en
        // pánico por datos de entrada.
        self.state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }

    /// Carga la última instantánea y deja el mundo listo. Solo con NOT_FOUND
    /// empieza vacío en el tick 0; ante cualquier otro fallo reintenta sin
    /// fin, así el tick nunca retrocede.
    pub async fn load(&self, backoff: Backoff) {
        let mut delay = backoff.initial;
        let (world, restored_tick) = loop {
            match self.store.load().await {
                Ok(None) => {
                    tracing::info!("no hay instantáneas: el valle empieza en el tick 0");
                    break (World::new(self.rules.clone()), 0);
                }
                Ok(Some(snapshot)) => {
                    let tick = snapshot.tick;
                    match codec::decode_snapshot(snapshot, self.rules.clone()) {
                        Ok(world) => {
                            tracing::info!(tick, "instantánea cargada");
                            break (world, tick);
                        }
                        Err(error) => {
                            tracing::error!(tick, %error, ?delay, "instantánea ilegible; se reintenta");
                        }
                    }
                }
                Err(error) => {
                    tracing::warn!(%error, ?delay, "LoadSnapshot falló; se reintenta");
                }
            }
            tokio::time::sleep(delay).await;
            delay = backoff.next(delay);
        };
        let mut state = self.lock();
        if matches!(*state, State::Loading) {
            *state = State::Ready {
                world,
                restored_tick,
            };
        }
    }

    /// Aplica un tick. `view` lee el mundo resultante con el bloqueo tomado.
    pub fn step<R>(
        &self,
        commands: &[Command],
        view: impl FnOnce(&World) -> R,
    ) -> Result<StepReport<R>, NotReady> {
        let mut state = self.lock();
        let State::Ready {
            world,
            restored_tick,
        } = &mut *state
        else {
            return Err(NotReady);
        };
        let outcome = world.step(commands);
        if is_snapshot_tick(outcome.tick) {
            self.schedule_snapshot(world);
        }
        Ok(StepReport {
            outcome,
            restored_tick: *restored_tick,
            view: view(world),
        })
    }

    /// Copia el mundo y lo guarda desde una tarea en segundo plano: Step
    /// nunca espera a la persistencia. Si el guardado anterior sigue en
    /// curso, esta instantánea se salta.
    fn schedule_snapshot(&self, world: &World) {
        let tick = world.tick();
        if self.saving.swap(true, Ordering::AcqRel) {
            tracing::warn!(tick, "guardado anterior en curso: se salta la instantánea");
            return;
        }
        let copy = world.clone();
        let store = Arc::clone(&self.store);
        let saving = Arc::clone(&self.saving);
        tokio::spawn(async move {
            // Libera el turno al terminar, también si la tarea entra en pánico.
            let _turn = SavingTurn(saving);
            let snapshot = codec::encode_snapshot(&copy);
            match store.save(snapshot).await {
                Ok(()) => tracing::debug!(tick, "instantánea guardada"),
                Err(error) => tracing::error!(tick, %error, "SaveSnapshot falló"),
            }
        });
    }
}

struct SavingTurn(Arc<AtomicBool>);

impl Drop for SavingTurn {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

#[cfg(test)]
mod tests {
    use std::collections::VecDeque;
    use std::sync::atomic::AtomicUsize;

    use prost::Message;
    use tokio::sync::Semaphore;

    use super::*;
    use crate::domain::{CommandKind, CommandOutcome};
    use crate::proto::world::v1 as pb;
    use crate::store::{Snapshot, StoreError};

    const ANA: &str = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01";

    #[derive(Default)]
    struct FakeStore {
        loads: Mutex<VecDeque<Result<Option<Snapshot>, StoreError>>>,
        load_calls: AtomicUsize,
        save_calls: AtomicUsize,
        saved: Mutex<Vec<Snapshot>>,
        save_error: Mutex<Option<StoreError>>,
        /// Si existe, cada guardado espera un permiso.
        gate: Option<Semaphore>,
    }

    impl FakeStore {
        fn with_loads(loads: Vec<Result<Option<Snapshot>, StoreError>>) -> Self {
            Self {
                loads: Mutex::new(loads.into()),
                ..Self::default()
            }
        }

        fn saved_ticks(&self) -> Vec<u64> {
            self.saved.lock().unwrap().iter().map(|s| s.tick).collect()
        }
    }

    impl SnapshotStore for FakeStore {
        async fn load(&self) -> Result<Option<Snapshot>, StoreError> {
            self.load_calls.fetch_add(1, Ordering::SeqCst);
            self.loads.lock().unwrap().pop_front().unwrap_or(Ok(None))
        }

        async fn save(&self, snapshot: Snapshot) -> Result<(), StoreError> {
            self.save_calls.fetch_add(1, Ordering::SeqCst);
            if let Some(gate) = &self.gate {
                gate.acquire().await.unwrap().forget();
            }
            if let Some(error) = self.save_error.lock().unwrap().clone() {
                return Err(error);
            }
            self.saved.lock().unwrap().push(snapshot);
            Ok(())
        }
    }

    fn fast() -> Backoff {
        Backoff {
            initial: Duration::from_millis(1),
            max: Duration::from_millis(2),
        }
    }

    async fn ready(store: FakeStore) -> (Simulator<FakeStore>, Arc<FakeStore>) {
        let store = Arc::new(store);
        let simulator = Simulator::new(Rules::initial(), store.clone());
        simulator.load(fast()).await;
        (simulator, store)
    }

    fn steps(simulator: &Simulator<FakeStore>, count: u64) {
        for _ in 0..count {
            simulator.step(&[], |_| ()).unwrap();
        }
    }

    async fn eventually(what: &str, condition: impl Fn() -> bool) {
        for _ in 0..200 {
            if condition() {
                return;
            }
            tokio::time::sleep(Duration::from_millis(5)).await;
        }
        panic!("no se cumplió: {what}");
    }

    fn snapshot_at(tick: u64) -> Snapshot {
        let mut world = World::new(Rules::initial());
        world.step(&[Command {
            action_id: "00000000-0000-4000-8000-000000000001".to_string(),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::Drop { x: 1, y: 1 }),
        }]);
        let mut parts = world.parts();
        parts.tick = tick;
        codec::encode_snapshot(&World::restore(Rules::initial(), parts).unwrap())
    }

    #[test]
    fn backoff_doubles_from_one_second_up_to_five() {
        let backoff = Backoff::default();
        let mut delay = backoff.initial;
        let mut seen = vec![delay];
        for _ in 0..4 {
            delay = backoff.next(delay);
            seen.push(delay);
        }
        assert_eq!(seen, [1, 2, 4, 5, 5].map(Duration::from_secs).to_vec());
    }

    #[test]
    fn boot_id_is_a_fresh_uuid_v4() {
        let store = Arc::new(FakeStore::default());
        let a = Simulator::new(Rules::initial(), store.clone());
        let b = Simulator::new(Rules::initial(), store);
        let parsed = Uuid::parse_str(a.boot_id()).unwrap();
        assert_eq!(parsed.get_version_num(), 4);
        assert_ne!(a.boot_id(), b.boot_id());
    }

    #[test]
    fn step_is_unavailable_until_the_world_is_loaded() {
        let simulator = Simulator::new(Rules::initial(), Arc::new(FakeStore::default()));
        assert!(!simulator.is_ready());
        assert_eq!(simulator.step(&[], |_| ()).unwrap_err(), NotReady);
    }

    #[tokio::test]
    async fn not_found_starts_an_empty_world_at_tick_zero() {
        let (simulator, _) = ready(FakeStore::with_loads(vec![Ok(None)])).await;
        assert!(simulator.is_ready());
        let report = simulator
            .step(&[], |world| world.players().count())
            .unwrap();
        assert_eq!(report.outcome.tick, 1);
        assert_eq!(report.restored_tick, 0);
        assert_eq!(report.view, 0);
    }

    #[tokio::test]
    async fn a_found_snapshot_is_restored_and_reported_on_every_step() {
        let (simulator, _) = ready(FakeStore::with_loads(vec![Ok(Some(snapshot_at(150)))])).await;
        for expected in [151, 152] {
            let report = simulator
                .step(&[], |world| world.tile(1, 1).unwrap().humidity)
                .unwrap();
            assert_eq!(report.outcome.tick, expected);
            assert_eq!(report.restored_tick, 150);
            assert_eq!(report.view, 25);
        }
    }

    #[tokio::test]
    async fn load_keeps_retrying_until_persistence_answers() {
        let mut corrupt = snapshot_at(50);
        corrupt.world_state = vec![0xff];
        let store = FakeStore::with_loads(vec![
            Err(StoreError::Unavailable("down".into())),
            Err(StoreError::Unavailable("down".into())),
            Ok(Some(corrupt)),
            Err(StoreError::Failed("boom".into())),
            Ok(Some(snapshot_at(100))),
        ]);
        let (simulator, store) = ready(store).await;
        assert_eq!(store.load_calls.load(Ordering::SeqCst), 5);
        assert_eq!(simulator.step(&[], |_| ()).unwrap().restored_tick, 100);
    }

    #[tokio::test]
    async fn step_applies_commands_through_the_domain() {
        let (simulator, _) = ready(FakeStore::default()).await;
        let command = Command {
            action_id: "00000000-0000-4000-8000-000000000009".to_string(),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::Drop { x: 4, y: 4 }),
        };
        let report = simulator
            .step(std::slice::from_ref(&command), |world| {
                world.humidity_contributed()
            })
            .unwrap();
        assert_eq!(report.outcome.results[0].outcome, CommandOutcome::Applied);
        assert_eq!(report.view, 25);
    }

    #[tokio::test]
    async fn a_snapshot_is_saved_every_50_ticks() {
        let (simulator, store) = ready(FakeStore::default()).await;
        steps(&simulator, 49);
        tokio::task::yield_now().await;
        assert_eq!(store.save_calls.load(Ordering::SeqCst), 0);

        steps(&simulator, 1);
        eventually("guardado del tick 50", || store.saved_ticks() == vec![50]).await;
        steps(&simulator, 50);
        eventually("guardado del tick 100", || {
            store.saved_ticks() == vec![50, 100]
        })
        .await;

        let saved = store.saved.lock().unwrap();
        let inner = pb::WorldSnapshot::decode(saved[1].world_state.as_slice()).unwrap();
        assert_eq!(inner.tick, 100);
        assert_eq!(inner.tiles.len(), 1024);
    }

    #[tokio::test]
    async fn a_snapshot_is_skipped_while_the_previous_one_is_in_flight() {
        let store = FakeStore {
            gate: Some(Semaphore::new(0)),
            ..FakeStore::default()
        };
        let (simulator, store) = ready(store).await;
        steps(&simulator, 50);
        eventually("primer guardado en curso", || {
            store.save_calls.load(Ordering::SeqCst) == 1
        })
        .await;

        steps(&simulator, 50); // tick 100: se salta.
        tokio::task::yield_now().await;
        assert_eq!(store.save_calls.load(Ordering::SeqCst), 1);

        store.gate.as_ref().unwrap().add_permits(10);
        eventually("fin del primer guardado", || {
            store.saved_ticks() == vec![50]
        })
        .await;
        eventually("guardado libre", || {
            !simulator.saving.load(Ordering::SeqCst)
        })
        .await;
        steps(&simulator, 50); // tick 150
        eventually("guardado del tick 150", || {
            store.saved_ticks() == vec![50, 150]
        })
        .await;
    }

    #[tokio::test]
    async fn a_failed_save_never_stops_the_game_and_is_retried_next_time() {
        let store = FakeStore::default();
        *store.save_error.lock().unwrap() = Some(StoreError::AlreadyExists("otra".into()));
        let (simulator, store) = ready(store).await;
        steps(&simulator, 50);
        eventually("primer intento", || {
            store.save_calls.load(Ordering::SeqCst) == 1
        })
        .await;
        eventually("guardado libre", || {
            !simulator.saving.load(Ordering::SeqCst)
        })
        .await;

        *store.save_error.lock().unwrap() = None;
        steps(&simulator, 50);
        eventually("segundo intento", || store.saved_ticks() == vec![100]).await;
        assert_eq!(simulator.step(&[], |_| ()).unwrap().outcome.tick, 101);
    }
}
