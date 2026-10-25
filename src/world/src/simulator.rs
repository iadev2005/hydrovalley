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
use crate::domain::{
    Catalog, Command, CommandKind, SNAPSHOT_INTERVAL_TICKS, StepOutcome, World, is_snapshot_tick,
};
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

/// El `Step` trae un `hub_generation` menor que el máximo visto por el
/// simulador (D-B8-1): `Step` responde `FAILED_PRECONDITION` sin tocar el mundo.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Fenced;

/// Lo que devuelve un paso, más lo que la capa de red quiera leer del
/// mundo mientras sigue bloqueado.
#[derive(Debug)]
pub struct StepReport<R> {
    pub outcome: StepOutcome,
    pub restored_tick: u64,
    pub boot_id: String,
    pub view: R,
}

enum State {
    /// En espera (`WORLD_MODE=standby`): no carga la instantánea hasta recibir
    /// el primer `Step` de promoción (B8 W2).
    Standby,
    Loading,
    // En el montón: el mundo lleva el catálogo y es mucho mayor que `Loading`.
    Ready {
        world: Box<World>,
        restored_tick: u64,
        boot_id: String,
    },
}

pub struct Simulator<S> {
    initial_boot_id: String,
    /// Semilla del generador de azar cuando el valle empieza de cero. Si hay
    /// instantánea manda el estado guardado en ella, no esta semilla.
    seed: u64,
    load_backoff: Backoff,
    store: Arc<S>,
    state: Mutex<State>,
    /// Hay un guardado en curso (como mucho uno).
    saving: Arc<AtomicBool>,
    /// Tick de la última instantánea que está en la persistencia (la
    /// cargada al arrancar o la última guardada con éxito).
    saved_tick: Arc<AtomicU64>,
    /// Época interna de memoria para descartar guardados en vuelo previos a
    /// una recarga por despausado (`min_expected_tick`).
    epoch: Arc<AtomicU64>,
    /// Mayor `min_expected_tick` para el que ya se solicitó una carga o
    /// recarga desde persistencia, evitando bucles de recarga cuando la última
    /// instantánea guardada en base de datos es anterior a `min_expected_tick`
    /// dentro del margen RPO (`<= 50` ticks).
    acknowledged_min_tick: AtomicU64,
    /// Mayor `hub_generation` visto en `Step` para hacer fencing de hubs
    /// antiguos que despiertan de una pausa (D-B8-1).
    max_hub_generation: AtomicU64,
}

/// Plazo para volver a pedir el catálogo al reiniciar el valle. Es la mitad
/// del plazo de `Step` (200 ms): si la persistencia no contesta a tiempo, el
/// reinicio se aplica con el catálogo vigente en vez de hacer fallar el tick.
const RESET_CATALOG_TIMEOUT: Duration = Duration::from_millis(100);

/// Cada cuánto se comprueba, durante la parada, si terminó el guardado en
/// curso.
const SAVE_POLL: Duration = Duration::from_millis(10);

impl<S: PersistenceStore> Simulator<S> {
    pub fn new(store: Arc<S>, seed: u64) -> Self {
        Self::with_state(store, seed, Backoff::default(), State::Loading)
    }

    pub fn new_standby(store: Arc<S>, seed: u64, load_backoff: Backoff) -> Self {
        Self::with_state(store, seed, load_backoff, State::Standby)
    }

    fn with_state(store: Arc<S>, seed: u64, load_backoff: Backoff, initial: State) -> Self {
        Self {
            initial_boot_id: Uuid::new_v4().to_string(),
            seed,
            load_backoff,
            store,
            state: Mutex::new(initial),
            saving: Arc::new(AtomicBool::new(false)),
            saved_tick: Arc::new(AtomicU64::new(0)),
            epoch: Arc::new(AtomicU64::new(1)),
            acknowledged_min_tick: AtomicU64::new(0),
            max_hub_generation: AtomicU64::new(0),
        }
    }

    /// UUID v4 actual del simulador; se rota si el contenedor estuvo pausado
    /// y recarga desde persistencia al despausarse (D-B8-1 / D-B8-2).
    pub fn boot_id(&self) -> String {
        match &*self.lock() {
            State::Ready { boot_id, .. } => boot_id.clone(),
            _ => self.initial_boot_id.clone(),
        }
    }

    pub fn is_ready(&self) -> bool {
        matches!(*self.lock(), State::Ready { .. })
    }

    pub fn is_standby(&self) -> bool {
        matches!(*self.lock(), State::Standby)
    }

    /// Valida y actualiza la generación máxima de hub vista por el simulador
    /// (D-B8-1). Si `hub_generation > 0` y es menor que la máxima vista,
    /// devuelve `Err(Fenced)` sin tocar el mundo.
    pub fn check_hub_generation(&self, hub_generation: u64) -> Result<(), Fenced> {
        if hub_generation == 0 {
            return Ok(());
        }
        let mut current = self.max_hub_generation.load(Ordering::Acquire);
        loop {
            if hub_generation < current {
                tracing::warn!(
                    hub_generation,
                    max_hub_generation = current,
                    "Step rechazado por fencing de hub_generation"
                );
                return Err(Fenced);
            }
            if hub_generation == current {
                return Ok(());
            }
            match self.max_hub_generation.compare_exchange_weak(
                current,
                hub_generation,
                Ordering::AcqRel,
                Ordering::Acquire,
            ) {
                Ok(_) => {
                    tracing::info!(
                        previous_generation = current,
                        hub_generation,
                        "nueva generación de hub registrada"
                    );
                    return Ok(());
                }
                Err(actual) => current = actual,
            }
        }
    }

    /// Si el simulador está en `Standby` o si su `tick` en memoria quedó por
    /// debajo de `min_expected_tick` (contenedor despausado tras un relevo, o
    /// regreso `world-go → Rust`), invalida su estado en memoria, lanza la
    /// carga de la última instantánea en segundo plano con un nuevo `boot_id`
    /// y devuelve `Err(NotReady)` para que `Step` responda `UNAVAILABLE` hasta
    /// completar la recarga.
    pub fn ensure_active_and_fresh(
        self: &Arc<Self>,
        min_expected_tick: u64,
    ) -> Result<(), NotReady> {
        let mut state = self.lock();
        match &*state {
            State::Standby => {
                tracing::info!(
                    min_expected_tick,
                    "promoción de simulador standby: iniciando carga de instantánea"
                );
                self.acknowledged_min_tick
                    .fetch_max(min_expected_tick, Ordering::AcqRel);
                *state = State::Loading;
                self.spawn_background_load(true);
                Err(NotReady)
            }
            State::Loading => Err(NotReady),
            State::Ready {
                world,
                restored_tick,
                ..
            } => {
                let ack = self.acknowledged_min_tick.load(Ordering::Acquire);
                let just_loaded_same_window = world.tick() == *restored_tick
                    && min_expected_tick < restored_tick.saturating_add(SNAPSHOT_INTERVAL_TICKS);
                if min_expected_tick > world.tick()
                    && min_expected_tick > ack
                    && !just_loaded_same_window
                {
                    tracing::warn!(
                        local_tick = world.tick(),
                        min_expected_tick,
                        "estado en memoria desfasado tras pausa o relevo: descartando mundo y recargando instantánea"
                    );
                    self.acknowledged_min_tick
                        .store(min_expected_tick, Ordering::Release);
                    self.epoch.fetch_add(1, Ordering::AcqRel);
                    *state = State::Loading;
                    self.spawn_background_load(true);
                    return Err(NotReady);
                }
                Ok(())
            }
        }
    }

    fn spawn_background_load(self: &Arc<Self>, rotate_boot_id: bool) {
        let Ok(runtime) = tokio::runtime::Handle::try_current() else {
            return;
        };
        let sim = Arc::clone(self);
        let backoff = self.load_backoff;
        runtime.spawn(async move {
            sim.load_inner(backoff, rotate_boot_id).await;
        });
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
        self.load_inner(backoff, false).await;
    }

    async fn load_inner(&self, backoff: Backoff, rotate_boot_id: bool) {
        let catalog = self.fetch_catalog(backoff).await;
        let mut delay = backoff.initial;
        let (world, restored_tick) = loop {
            match self.store.load().await {
                Ok(None) => {
                    tracing::info!(
                        seed = self.seed,
                        "no hay instantáneas: el valle empieza en el tick 0 (con esta semilla se repite la partida)"
                    );
                    break (World::with_seed(catalog.clone(), self.seed), 0);
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
            let boot_id = if rotate_boot_id {
                Uuid::new_v4().to_string()
            } else {
                self.initial_boot_id.clone()
            };
            *state = State::Ready {
                world: Box::new(world),
                restored_tick,
                boot_id,
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

    /// Lectura sin modificar: ejecuta `view` sobre el mundo con el bloqueo
    /// tomado. `view` debe ser breve (copiar lo que necesite) y hacer el
    /// trabajo pesado después, ya sin el bloqueo, para no retrasar `Step`.
    pub fn read<R>(&self, view: impl FnOnce(&World) -> R) -> Result<R, NotReady> {
        match &*self.lock() {
            State::Ready { world, .. } => Ok(view(world)),
            _ => Err(NotReady),
        }
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
            boot_id,
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
            boot_id: boot_id.clone(),
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
        let epoch_ref = Arc::clone(&self.epoch);
        let expected_epoch = epoch_ref.load(Ordering::Acquire);
        runtime.spawn(async move {
            // Libera el turno al terminar, también si la tarea entra en pánico.
            let _turn = SavingTurn(saving);
            if epoch_ref.load(Ordering::Acquire) != expected_epoch {
                return;
            }
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

    use crate::domain::rng::SplitMix64;
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
        let simulator = Simulator::new(store.clone(), 0);
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
        let mut world = World::with_seed(Catalog::d01(), 0);
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
        let a = Simulator::new(store.clone(), 0);
        let b = Simulator::new(store, 0);
        let parsed = Uuid::parse_str(&a.boot_id()).unwrap();
        assert_eq!(parsed.get_version_num(), 4);
        assert_ne!(a.boot_id(), b.boot_id());
    }

    #[test]
    fn step_is_unavailable_until_the_world_is_loaded() {
        let simulator = Simulator::new(Arc::new(FakeStore::default()), 0);
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
    async fn an_empty_world_starts_from_the_configured_seed_and_a_snapshot_keeps_its_own() {
        let seeded = |seed, loads| async move {
            let simulator = Simulator::new(Arc::new(FakeStore::with_loads(loads)), seed);
            simulator.load(fast()).await;
            simulator
                .step(&[], |world| world.parts().rng_state)
                .unwrap()
                .view
        };
        let fresh = seeded(99, vec![Ok(None)]).await;
        assert_eq!(fresh, SplitMix64::new(99).to_bytes());

        // La instantánea trae su generador (semilla 0 más ningún sorteo, pues
        // el valle de la prueba no tiene casillas elegibles): la semilla de
        // configuración no lo pisa.
        let restored = seeded(99, vec![Ok(Some(snapshot_at(150)))]).await;
        assert_eq!(restored, SplitMix64::new(0).to_bytes());
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
        let simulator = Simulator::new(store.clone(), 0);
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
        let loading = Simulator::new(store.clone(), 0);
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
        let simulator = Simulator::new(store.clone(), 0);
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
        let simulator = Simulator::new(Arc::new(HangingCatalog), 0);
        *simulator.state.lock().unwrap() = State::Ready {
            world: Box::new(World::with_seed(Catalog::d01(), 0)),
            restored_tick: 0,
            boot_id: simulator.boot_id(),
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

    #[tokio::test]
    async fn standby_mode_does_not_load_snapshot_until_first_step_promotion() {
        let store = Arc::new(FakeStore::with_loads(vec![Ok(Some(snapshot_at(200)))]));
        let simulator = Arc::new(Simulator::new_standby(store.clone(), 0, fast()));
        assert!(simulator.is_standby());
        assert!(!simulator.is_ready());
        assert_eq!(store.load_calls.load(Ordering::SeqCst), 0);

        // El primer intento promueve Standby -> Loading y devuelve NotReady.
        assert_eq!(simulator.ensure_active_and_fresh(0), Err(NotReady));
        eventually("promoción de standby completada", || simulator.is_ready()).await;
        assert_eq!(store.load_calls.load(Ordering::SeqCst), 1);

        let report = simulator.step(&[], |_| ()).unwrap();
        assert_eq!(report.restored_tick, 200);
        assert_eq!(report.outcome.tick, 201);
    }

    #[tokio::test]
    async fn min_expected_tick_higher_than_local_tick_forces_reload_and_rotates_boot_id() {
        let store = Arc::new(FakeStore::with_loads(vec![
            Ok(Some(snapshot_at(100))),
            Ok(Some(snapshot_at(250))),
        ]));
        let simulator = Arc::new(Simulator::new(store.clone(), 0));
        simulator.load(fast()).await;

        let first = simulator.step(&[], |_| ()).unwrap();
        assert_eq!(first.outcome.tick, 101);
        let original_boot_id = first.boot_id.clone();

        // El contenedor estuvo en pausa mientras otro simulador (world-go)
        // avanzó hasta el tick 260 y guardó la instantánea del tick 250.
        assert_eq!(simulator.ensure_active_and_fresh(260), Err(NotReady));
        eventually("recarga tras despausado completada", || {
            simulator.is_ready()
        })
        .await;

        // El siguiente sondeo del hub sigue trayendo min_expected_tick = 260
        // hasta que un Step completo devuelva el nuevo boot_id; no debe volver
        // a disparar otra recarga en bucle infinito.
        assert_eq!(simulator.ensure_active_and_fresh(260), Ok(()));
        let resumed = simulator.step(&[], |_| ()).unwrap();
        assert_eq!(resumed.restored_tick, 250);
        assert_eq!(resumed.outcome.tick, 251);
        assert_ne!(resumed.boot_id, original_boot_id);
    }

    #[tokio::test]
    async fn freshly_loaded_simulator_after_crash_accepts_min_expected_tick_in_same_snapshot_window()
     {
        // Tras un SIGKILL en tick 1410, la última instantánea periódica en
        // PostgreSQL es la del tick 1400 (ventana de 50 ticks). Al reiniciar,
        // el primer Step trae min_expected_tick = 1410 y debe aceptarse sin
        // recargar en bucle.
        let store = Arc::new(FakeStore::with_loads(vec![Ok(Some(snapshot_at(1400)))]));
        let simulator = Arc::new(Simulator::new(store.clone(), 0));
        simulator.load(fast()).await;

        assert_eq!(simulator.ensure_active_and_fresh(1410), Ok(()));
        assert_eq!(store.load_calls.load(Ordering::SeqCst), 1);
        let report = simulator.step(&[], |_| ()).unwrap();
        assert_eq!(report.restored_tick, 1400);
        assert_eq!(report.outcome.tick, 1401);
    }

    #[tokio::test]
    async fn check_hub_generation_fences_lower_generation() {
        let (simulator, _) = ready(FakeStore::default()).await;
        assert_eq!(simulator.check_hub_generation(0), Ok(()));
        assert_eq!(simulator.check_hub_generation(1), Ok(()));
        assert_eq!(simulator.check_hub_generation(1), Ok(()));
        assert_eq!(simulator.check_hub_generation(2), Ok(()));
        assert_eq!(simulator.check_hub_generation(1), Err(Fenced));
        assert_eq!(simulator.check_hub_generation(2), Ok(()));
    }
}
