//! Casillas del valle.

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum TileStage {
    #[default]
    DrySoil,
    Grass,
    Shrub,
    Flowers,
    Tree,
    Water,
    Rock,
}

impl TileStage {
    /// Etapa a la que sube la casilla al acumular la humedad necesaria;
    /// `None` en el árbol, agua y roca.
    pub fn next(self) -> Option<Self> {
        match self {
            Self::DrySoil => Some(Self::Grass),
            Self::Grass => Some(Self::Shrub),
            Self::Shrub => Some(Self::Flowers),
            Self::Flowers => Some(Self::Tree),
            Self::Tree | Self::Water | Self::Rock => None,
        }
    }

    /// Etapa a la que baja al marchitarse; `None` en tierra seca, agua y roca.
    pub fn previous(self) -> Option<Self> {
        match self {
            Self::DrySoil | Self::Water | Self::Rock => None,
            Self::Grass => Some(Self::DrySoil),
            Self::Shrub => Some(Self::Grass),
            Self::Flowers => Some(Self::Shrub),
            Self::Tree => Some(Self::Flowers),
        }
    }

    /// `true` si es un elemento de terreno natural (agua o roca).
    pub fn is_natural(self) -> bool {
        matches!(self, Self::Water | Self::Rock)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum InstallationKind {
    Sprinkler,
    Drip,
    TreatmentPlant,
    TrashBin,
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
    pub remaining_durability: u32,
}

impl Tile {
    /// La casilla no admite plantas ni humedad: terreno natural, edificio o
    /// basura encima.
    pub fn is_blocked(&self) -> bool {
        self.stage.is_natural() || self.has_building || self.trash_level > 0
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Tile {
    pub stage: TileStage,
    pub humidity: u64,
    pub dry_ticks: u32,
    pub fire: Option<Fire>,
    pub installation: Option<Installation>,
    pub has_building: bool,
    pub trash_level: u32,
}
