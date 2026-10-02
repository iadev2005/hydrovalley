//! Servicio de aplicación: guarda el mundo, decide cuándo está listo y
//! envía las instantáneas en segundo plano.
//!
//! No conoce gRPC más allá del puerto [`PersistenceStore`] (instantáneas y
//! catálogo): la capa de red
//! (`crate::grpc`) traduce las peticiones y llama aquí.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use uuid::Uuid;

use crate::codec;
use crate::domain::{Catalog, Command, CommandKind, StepOutcome, World, is_snapshot_tick};
use crate::store::PersistenceStore;

/// Esperas entre intentos de `GetCatalog` y `LoadSnapshot` al arrancar: 1 s, 2 s, 4 s y
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
    // En el montón: el mundo lleva el catálogo y es mucho mayor que `Loading`.
    Ready {
        world: Box<World>,
        restored_tick: u64,
    },
}

pub struct Simulator<S> {
    boot_id: String,
    store: Arc<S>,
    state: Mutex<State>,
    /// Hay un guardado en curso (como mucho uno).
    saving: Arc<AtomicBool>,
    /// Tick de la última instantánea que está en la persistencia (la
    /// cargada al arrancar o la última guardada con éxito).
    saved_tick: Arc<AtomicU64>,
}

/// Plazo para volver a pedir el catálogo al reiniciar el valle. Es la mitad
/// del plazo de `Step` (200 ms): si la persistencia no contesta a tiempo, el
/// reinicio se aplica con el catálogo vigente en vez de hacer fallar el tick.
const RESET_CATALOG_TIMEOUT: Duration = Duration::from_millis(100);

/// Cada cuánto se comprueba, durante la parada, si terminó el guardado en
/// curso.
const SAVE_POLL: Duration = Duration::from_millis(10);

impl<S: PersistenceStore> Simulator<S> {
    pub fn new(store: Arc<S>) -> Self {
        Self {
            boot_id: Uuid::new_v4().to_string(),
            store,
            state: Mutex::new(State::Loading),
            saving: Arc::new(AtomicBool::new(false)),
            saved_tick: Arc::new(AtomicU64::new(0)),
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

    /// Pide el catálogo a la persistencia y reintenta sin fin hasta
    /// obtenerlo: sin cifras no se puede simular. Es el punto donde también
    /// se enganchará el reinicio del valle (RF-31), que vuelve a pedirlo.
    pub async fn fetch_catalog(&self, backoff: Backoff) -> Catalog {
        let mut delay = backoff.initial;
        loop {
            match self.store.get_catalog().await {
                Ok(catalog) => {
                    tracing::info!(catalog_version = %catalog.version, "catálogo cargado");
                    return catalog;
                }
                Err(error) => {
                    tracing::warn!(%error, ?delay, "GetCatalog falló; se reintenta");
                }
            }
            tokio::time::sleep(delay).await;
            delay = backoff.next(delay);
        }
    }

    /// Carga el catálogo y la última instantánea, y deja el mundo listo.
    /// Solo con NOT_FOUND empieza vacío en el tick 0; ante cualquier otro
    /// fallo (del catálogo o de la instantánea) reintenta sin fin, así el
    /// tick nunca retrocede. El mundo aplica el catálogo vigente, también
    /// si la instantánea se tomó con otra versión.
    pub async fn load(&self, backoff: Backoff) {
        let catalog = self.fetch_catalog(backoff).await;
        let mut delay = backoff.initial;
        let (world, restored_tick) = loop {
            match self.store.load().await {
                Ok(None) => {
                    tracing::info!("no hay instantáneas: el valle empieza en el tick 0");
                    break (World::new(catalog.clone()), 0);
                }
                Ok(Some(snapshot)) => {
                    let tick = snapshot.tick;
                    let saved_version = snapshot.catalog_version.clone();
                    match codec::decode_snapshot(snapshot, catalog.clone()) {
                        Ok(world) => {
                            if saved_version != catalog.version {
                                tracing::info!(
                                    tick,
                                    saved = %saved_version,
                                    current = %catalog.version,
                                    "la instantánea es de otra versión del catálogo: se aplica la vigente"
                                );
                            }
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
            self.saved_tick.store(restored_tick, Ordering::Release);
            *state = State::Ready {
                world: Box::new(world),
                restored_tick,
            };
        }
    }

    /// Catálogo fresco para un tick que trae un `ResetValley` (RF-31): el
    /// reinicio vuelve a pedirlo para que un cambio de cifras en `catalog/`
    /// surta efecto sin reiniciar Rust. Hace un solo intento, con plazo de
    /// [`RESET_CATALOG_TIMEOUT`], antes de tomar el bloqueo del mundo: si la
    /// persistencia falla o tarda, devuelve `None` y el reinicio se aplica con
    /// el catálogo vigente (se registra el aviso). Sin reinicio (o con el
    /// mundo sin cargar) no llama a la persistencia.
    pub async fn catalog_for(&self, commands: &[Command]) -> Option<Catalog> {
        let resets = commands
            .iter()
            .any(|command| matches!(command.kind, Some(CommandKind::ResetValley)));
        if !resets || !self.is_ready() {
            return None;
        }
        match tokio::time::timeout(RESET_CATALOG_TIMEOUT, self.store.get_catalog()).await {
            Ok(Ok(catalog)) => {
                tracing::info!(catalog_version = %catalog.version, "catálogo recargado para el reinicio");
                Some(catalog)
            }
            Ok(Err(error)) => {
                tracing::warn!(%error, "GetCatalog falló al reiniciar: se usa el catálogo vigente");
                None
            }
            Err(_) => {
                tracing::warn!(timeout = ?RESET_CATALOG_TIMEOUT, "GetCatalog tardó demasiado al reiniciar: se usa el catálogo vigente");
                None
            }
        }
    }

    /// Aplica un tick. `view` lee el mundo resultante con el bloqueo tomado.
    pub fn step<R>(
        &self,
        commands: &[Command],
        view: impl FnOnce(&World) -> R,
    ) -> Result<StepReport<R>, NotReady> {
        self.step_with(commands, None, view)
    }

    /// Como [`step`](Self::step), pero ofrece `catalog` (lo devuelve
    /// [`catalog_for`](Self::catalog_for)) al reinicio del tick: el mundo lo
    /// adopta solo si el `ResetValley` se aplica.
    pub fn step_with<R>(
        &self,
        commands: &[Command],
        catalog: Option<Catalog>,
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
        let outcome = world.step_with_reset_catalog(commands, catalog);
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
    ///
    /// La tarea se lanza en el runtime de Tokio actual, si lo hay. Fuera de
    /// un runtime (p. ej. una prueba síncrona) no hay dónde lanzarla: la
    /// instantánea se salta con un error en el registro en lugar de entrar
    /// en pánico con el mundo bloqueado, y el siguiente tick de instantánea
    /// lo vuelve a intentar.
    fn schedule_snapshot(&self, world: &World) {
        let tick = world.tick();
        let Ok(runtime) = tokio::runtime::Handle::try_current() else {
            tracing::error!(tick, "sin runtime de Tokio: se salta la instantánea");
            return;
        };
        if self.saving.swap(true, Ordering::AcqRel) {
            tracing::warn!(tick, "guardado anterior en curso: se salta la instantánea");
            return;
        }
        let copy = world.clone();
        let store = Arc::clone(&self.store);
        let saving = Arc::clone(&self.saving);
        let saved_tick = Arc::clone(&self.saved_tick);
        runtime.spawn(async move {
            // Libera el turno al terminar, también si la tarea entra en pánico.
            let _turn = SavingTurn(saving);
            save_copy(store.as_ref(), &saved_tick, &copy).await;
        });
    }

    /// Parada ordenada, cuando el servidor ya no acepta peticiones: espera
    /// al guardado en curso y, si el mundo está cargado y el tick avanzó
    /// desde la última instantánea guardada, envía una instantánea final
    /// del tick actual (aunque no sea múltiplo de 50). Todo con un plazo
    /// total de `timeout`; devuelve `false` si se agotó.
    pub async fn shutdown(&self, timeout: Duration) -> bool {
        let finished = tokio::time::timeout(timeout, self.final_snapshot())
            .await
            .is_ok();
        if !finished {
            tracing::error!(?timeout, "la instantánea final no terminó a tiempo");
        }
        finished
    }

    async fn final_snapshot(&self) {
        // Espera a que quede libre el turno de guardado y lo toma.
        while self.saving.swap(true, Ordering::AcqRel) {
            tokio::time::sleep(SAVE_POLL).await;
        }
        let _turn = SavingTurn(Arc::clone(&self.saving));
        let copy = match &*self.lock() {
            State::Ready { world, .. }
                if world.tick() > self.saved_tick.load(Ordering::Acquire) =>
            {
                world.clone()
            }
            _ => return,
        };
        tracing::info!(tick = copy.tick(), "instantánea final antes de parar");
        save_copy(self.store.as_ref(), &self.saved_tick, &copy).await;
    }
}

/// Serializa la copia del mundo, la guarda y, si sale bien, anota su tick.
async fn save_copy<S: PersistenceStore>(store: &S, saved_tick: &AtomicU64, copy: &World) {
    let tick = copy.tick();
    let snapshot = codec::encode_snapshot(copy);
    match store.save(snapshot).await {
        Ok(()) => {
            saved_tick.fetch_max(tick, Ordering::AcqRel);
            tracing::debug!(tick, "instantánea guardada");
        }
        Err(error) => tracing::error!(tick, %error, "SaveSnapshot falló"),
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
        /// Respuestas de `get_catalog`; agotadas, el catálogo de D-01.
        catalogs: Mutex<VecDeque<Result<Catalog, StoreError>>>,
        catalog_calls: AtomicUsize,
        /// Orden de las llamadas de carga: "catalog" y "load".
        calls: Mutex<Vec<&'static str>>,
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

    impl PersistenceStore for FakeStore {
        async fn get_catalog(&self) -> Result<Catalog, StoreError> {
            self.catalog_calls.fetch_add(1, Ordering::SeqCst);
            self.calls.lock().unwrap().push("catalog");
            self.catalogs
                .lock()
                .unwrap()
                .pop_front()
                .unwrap_or_else(|| Ok(Catalog::d01()))
        }

        async fn load(&self) -> Result<Option<Snapshot>, StoreError> {
            self.load_calls.fetch_add(1, Ordering::SeqCst);
            self.calls.lock().unwrap().push("load");
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
        let simulator = Simulator::new(store.clone());
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
        let mut world = World::new(Catalog::d01());
        world.step(&[Command {
            action_id: "00000000-0000-4000-8000-000000000001".to_string(),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::Drop { x: 1, y: 1 }),
        }]);
        let mut parts = world.parts();
        parts.tick = tick;
        codec::encode_snapshot(&World::restore(Catalog::d01(), parts).unwrap())
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
        let a = Simulator::new(store.clone());
        let b = Simulator::new(store);
        let parsed = Uuid::parse_str(a.boot_id()).unwrap();
        assert_eq!(parsed.get_version_num(), 4);
        assert_ne!(a.boot_id(), b.boot_id());
    }

    #[test]
    fn step_is_unavailable_until_the_world_is_loaded() {
        let simulator = Simulator::new(Arc::new(FakeStore::default()));
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

    #[test]
    fn step_outside_a_tokio_runtime_skips_the_snapshot_without_panicking() {
        let store = Arc::new(FakeStore::default());
        let simulator = Simulator::new(store.clone());
        tokio::runtime::Builder::new_current_thread()
            .enable_time()
            .build()
            .unwrap()
            .block_on(simulator.load(fast()));
        steps(&simulator, 50); // tick 50 sin runtime: no hay dónde lanzar el guardado.
        assert!(!simulator.saving.load(Ordering::SeqCst));
        assert_eq!(store.save_calls.load(Ordering::SeqCst), 0);
        assert_eq!(simulator.step(&[], |_| ()).unwrap().outcome.tick, 51);
    }

    #[tokio::test]
    async fn shutdown_waits_for_the_save_in_flight_and_saves_the_last_tick() {
        let store = FakeStore {
            gate: Some(Semaphore::new(0)),
            ..FakeStore::default()
        };
        let (simulator, store) = ready(store).await;
        steps(&simulator, 50);
        eventually("guardado del tick 50 en curso", || {
            store.save_calls.load(Ordering::SeqCst) == 1
        })
        .await;
        steps(&simulator, 20); // tick 70
        let release = async {
            tokio::time::sleep(Duration::from_millis(30)).await;
            assert!(store.saved_ticks().is_empty(), "shutdown must wait");
            store.gate.as_ref().unwrap().add_permits(10);
        };
        let (finished, ()) = tokio::join!(simulator.shutdown(Duration::from_secs(5)), release);
        assert!(finished);
        assert_eq!(store.saved_ticks(), vec![50, 70]);
    }

    #[tokio::test]
    async fn shutdown_saves_nothing_when_the_tick_did_not_advance() {
        let (simulator, store) = ready(FakeStore::default()).await;
        steps(&simulator, 50);
        eventually("guardado del tick 50", || store.saved_ticks() == vec![50]).await;
        assert!(simulator.shutdown(Duration::from_secs(5)).await);
        assert_eq!(store.save_calls.load(Ordering::SeqCst), 1);

        // Recién restaurado, sin pasos: la instantánea cargada ya es la última.
        let (restored, store) =
            ready(FakeStore::with_loads(vec![Ok(Some(snapshot_at(150)))])).await;
        assert!(restored.shutdown(Duration::from_secs(5)).await);
        assert_eq!(store.save_calls.load(Ordering::SeqCst), 0);

        // Sin mundo cargado no hay nada que guardar.
        let store = Arc::new(FakeStore::default());
        let loading = Simulator::new(store.clone());
        assert!(loading.shutdown(Duration::from_secs(5)).await);
        assert_eq!(store.save_calls.load(Ordering::SeqCst), 0);
    }

    #[tokio::test]
    async fn shutdown_is_bounded_when_the_save_never_finishes() {
        let store = FakeStore {
            gate: Some(Semaphore::new(0)),
            ..FakeStore::default()
        };
        let (simulator, _store) = ready(store).await;
        steps(&simulator, 51);
        let started = std::time::Instant::now();
        assert!(!simulator.shutdown(Duration::from_millis(50)).await);
        assert!(started.elapsed() < Duration::from_secs(2));
    }

    #[tokio::test]
    async fn the_catalog_is_fetched_before_the_snapshot() {
        let (simulator, store) = ready(FakeStore::default()).await;
        assert!(simulator.is_ready());
        assert_eq!(*store.calls.lock().unwrap(), vec!["catalog", "load"]);
    }

    #[tokio::test]
    async fn an_unavailable_catalog_keeps_the_world_not_ready_and_is_retried() {
        let store = Arc::new(FakeStore {
            catalogs: Mutex::new(
                vec![
                    Err(StoreError::Unavailable("sin base de datos".to_string())),
                    Err(StoreError::Failed("catálogo no válido".to_string())),
                ]
                .into(),
            ),
            ..FakeStore::default()
        });
        let simulator = Simulator::new(store.clone());
        simulator.load(fast()).await;

        assert_eq!(store.catalog_calls.load(Ordering::SeqCst), 3);
        // Sin catálogo no se consultó la instantánea.
        assert_eq!(
            *store.calls.lock().unwrap(),
            vec!["catalog", "catalog", "catalog", "load"]
        );
        assert!(simulator.is_ready());
    }

    #[tokio::test]
    async fn snapshots_carry_the_catalog_version() {
        let (simulator, store) = ready(FakeStore::default()).await;
        steps(&simulator, 50);
        eventually("SaveSnapshot del tick 50", || {
            store.saved_ticks() == vec![50]
        })
        .await;

        let saved = store.saved.lock().unwrap()[0].clone();
        assert_eq!(saved.catalog_version, Catalog::D01_VERSION);
        let inner = pb::WorldSnapshot::decode(saved.world_state.as_slice()).unwrap();
        assert_eq!(inner.catalog_version, Catalog::D01_VERSION);
    }

    fn drop_at(n: u32, x: u32, y: u32) -> Command {
        Command {
            action_id: format!("00000000-0000-4000-8000-{n:012}"),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::Drop { x, y }),
        }
    }

    #[tokio::test]
    async fn a_restored_world_uses_the_current_catalog_version() {
        let old = snapshot_at(50);
        let mut current = Catalog::d01();
        current.version = "nuevo".to_string();
        current.drop.humidity = 40;
        let store = FakeStore {
            catalogs: Mutex::new(vec![Ok(current)].into()),
            ..FakeStore::with_loads(vec![Ok(Some(old))])
        };
        let (simulator, _store) = ready(store).await;

        let report = simulator
            .step(&[drop_at(2, 0, 0)], |world| {
                (
                    world.catalog_version().to_string(),
                    world.tile(0, 0).unwrap().humidity,
                )
            })
            .unwrap();
        assert_eq!(report.view, ("nuevo".to_string(), 40));
    }

    fn reset_command() -> Command {
        Command {
            action_id: "00000000-0000-4000-8000-0000000000aa".to_string(),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::ResetValley),
        }
    }

    fn version_of(world: &World) -> String {
        world.catalog_version().to_string()
    }

    #[tokio::test]
    async fn a_reset_fetches_the_catalog_again_and_the_world_applies_it() {
        let mut next = Catalog::d01();
        next.version = "v2".to_string();
        let store = FakeStore::default();
        store
            .catalogs
            .lock()
            .unwrap()
            .extend([Ok(Catalog::d01()), Ok(next)]);
        let (simulator, store) = ready(store).await;
        assert_eq!(store.catalog_calls.load(Ordering::SeqCst), 1);

        let commands = [reset_command()];
        let fresh = simulator.catalog_for(&commands).await;
        assert_eq!(store.catalog_calls.load(Ordering::SeqCst), 2);
        let report = simulator.step_with(&commands, fresh, version_of).unwrap();
        assert_eq!(report.view, "v2");
        assert_eq!(report.outcome.results[0].outcome, CommandOutcome::Applied);
    }

    #[tokio::test]
    async fn without_a_reset_the_catalog_is_not_fetched() {
        let (simulator, store) = ready(FakeStore::default()).await;
        let commands = [Command {
            action_id: "00000000-0000-4000-8000-000000000001".to_string(),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::Drop { x: 1, y: 1 }),
        }];
        assert!(simulator.catalog_for(&commands).await.is_none());
        assert!(simulator.catalog_for(&[]).await.is_none());
        assert_eq!(store.catalog_calls.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn a_reset_still_applies_when_the_catalog_cannot_be_fetched() {
        let store = FakeStore::default();
        store.catalogs.lock().unwrap().extend([
            Ok(Catalog::d01()),
            Err(StoreError::Unavailable("sin persistencia".to_string())),
        ]);
        let (simulator, _) = ready(store).await;

        let commands = [reset_command()];
        let fresh = simulator.catalog_for(&commands).await;
        assert!(fresh.is_none(), "keeps the current catalog");
        let report = simulator.step_with(&commands, fresh, version_of).unwrap();
        assert_eq!(report.view, Catalog::D01_VERSION);
        assert_eq!(report.outcome.results[0].outcome, CommandOutcome::Applied);
    }

    /// Persistencia cuyo catálogo nunca responde.
    struct HangingCatalog;

    impl PersistenceStore for HangingCatalog {
        async fn get_catalog(&self) -> Result<Catalog, StoreError> {
            std::future::pending().await
        }
        async fn load(&self) -> Result<Option<Snapshot>, StoreError> {
            Ok(None)
        }
        async fn save(&self, _: Snapshot) -> Result<(), StoreError> {
            Ok(())
        }
    }

    #[tokio::test(start_paused = true)]
    async fn a_slow_catalog_does_not_hold_the_step_past_its_timeout() {
        let simulator = Simulator::new(Arc::new(HangingCatalog));
        *simulator.state.lock().unwrap() = State::Ready {
            world: Box::new(World::new(Catalog::d01())),
            restored_tick: 0,
        };
        // Con el reloj en pausa, Tokio lo adelanta solo al quedar inactivo:
        // el tiempo transcurrido es exactamente el plazo.
        let started = tokio::time::Instant::now();
        let fresh = simulator.catalog_for(&[reset_command()]).await;
        assert!(fresh.is_none());
        assert_eq!(started.elapsed(), RESET_CATALOG_TIMEOUT);
        assert!(RESET_CATALOG_TIMEOUT < Duration::from_millis(200));
    }

    #[tokio::test]
    async fn a_duplicate_or_rejected_reset_does_not_swap_the_catalog() {
        let mut next = Catalog::d01();
        next.version = "v2".to_string();
        let store = FakeStore::default();
        store
            .catalogs
            .lock()
            .unwrap()
            .extend([Ok(Catalog::d01()), Ok(next.clone()), Ok(next)]);
        let (simulator, _) = ready(store).await;

        let applied = [reset_command()];
        simulator.step(&applied, |_| ()).unwrap();

        let duplicate = simulator.catalog_for(&applied).await;
        assert!(duplicate.is_some());
        let report = simulator
            .step_with(&applied, duplicate, version_of)
            .unwrap();
        assert_eq!(report.outcome.results[0].outcome, CommandOutcome::Duplicate);
        assert_eq!(report.view, Catalog::D01_VERSION);

        let rejected = [Command {
            action_id: "not-a-uuid".to_string(),
            ..reset_command()
        }];
        let fresh = simulator.catalog_for(&rejected).await;
        assert!(fresh.is_some());
        let report = simulator.step_with(&rejected, fresh, version_of).unwrap();
        assert!(matches!(
            report.outcome.results[0].outcome,
            CommandOutcome::Rejected(_)
        ));
        assert_eq!(report.view, Catalog::D01_VERSION);
    }
}
