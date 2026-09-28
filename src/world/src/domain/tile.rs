//! Casillas del valle.

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum TileStage {
    #[default]
    DrySoil,
    Grass,
    Shrub,
    Flowers,
    Tree,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum InstallationKind {
    Sprinkler,
    Drip,
    TreatmentPlant,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UpgradeKind {
    BiggerDrop,
    BiggerTank,
}

/// Incendio en curso. Se guarda para que la instantánea viaje completa,
/// aunque el simulador todavía no enciende ni apaga fuegos.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Fire {
    pub drops_received: u32,
    pub burning_ticks: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Installation {
    pub kind: InstallationKind,
    pub owner_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Tile {
    pub stage: TileStage,
    pub humidity: u64,
    pub dry_ticks: u32,
    pub fire: Option<Fire>,
    pub installation: Option<Installation>,
}
