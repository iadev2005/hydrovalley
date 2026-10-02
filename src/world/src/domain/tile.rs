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

impl TileStage {
    /// Etapa a la que sube la casilla al acumular la humedad necesaria;
    /// `None` en el árbol.
    pub fn next(self) -> Option<Self> {
        match self {
            Self::DrySoil => Some(Self::Grass),
            Self::Grass => Some(Self::Shrub),
            Self::Shrub => Some(Self::Flowers),
            Self::Flowers => Some(Self::Tree),
            Self::Tree => None,
        }
    }

    /// Etapa a la que baja al marchitarse; `None` en tierra seca.
    pub fn previous(self) -> Option<Self> {
        match self {
            Self::DrySoil => None,
            Self::Grass => Some(Self::DrySoil),
            Self::Shrub => Some(Self::Grass),
            Self::Flowers => Some(Self::Shrub),
            Self::Tree => Some(Self::Flowers),
        }
    }
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

/// Incendio en curso: gotas recibidas desde que arde y ticks ardiendo.
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
