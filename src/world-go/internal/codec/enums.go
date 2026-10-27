package codec

import (
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

const (
	tileStageWater worldv1.TileStage = 6
	tileStageRock  worldv1.TileStage = 7
)

func stageToProto(stage domain.TileStage) worldv1.TileStage {
	switch stage {
	case domain.DrySoil:
		return worldv1.TileStage_TILE_STAGE_DRY_SOIL
	case domain.Grass:
		return worldv1.TileStage_TILE_STAGE_GRASS
	case domain.Shrub:
		return worldv1.TileStage_TILE_STAGE_SHRUB
	case domain.Flowers:
		return worldv1.TileStage_TILE_STAGE_FLOWERS
	case domain.Tree:
		return worldv1.TileStage_TILE_STAGE_TREE
	case domain.Water:
		return tileStageWater
	case domain.Rock:
		return tileStageRock
	default:
		return worldv1.TileStage_TILE_STAGE_UNSPECIFIED
	}
}

// stageFromProto rechaza lo sin especificar y lo desconocido, como Rust.
func stageFromProto(value worldv1.TileStage) (domain.TileStage, error) {
	switch value {
	case worldv1.TileStage_TILE_STAGE_DRY_SOIL:
		return domain.DrySoil, nil
	case worldv1.TileStage_TILE_STAGE_GRASS:
		return domain.Grass, nil
	case worldv1.TileStage_TILE_STAGE_SHRUB:
		return domain.Shrub, nil
	case worldv1.TileStage_TILE_STAGE_FLOWERS:
		return domain.Flowers, nil
	case worldv1.TileStage_TILE_STAGE_TREE:
		return domain.Tree, nil
	case tileStageWater:
		return domain.Water, nil
	case tileStageRock:
		return domain.Rock, nil
	default:
		return 0, &UnknownEnumError{Field: "Tile.stage", Value: int32(value)}
	}
}

func installationToProto(kind domain.InstallationKind) worldv1.InstallationKind {
	switch kind {
	case domain.Sprinkler:
		return worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER
	case domain.Drip:
		return worldv1.InstallationKind_INSTALLATION_KIND_DRIP
	case domain.TreatmentPlant:
		return worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT
	default:
		return worldv1.InstallationKind_INSTALLATION_KIND_UNSPECIFIED
	}
}

// installationFromProto rechaza lo sin especificar y lo desconocido; field
// nombra el campo en el error.
func installationFromProto(value worldv1.InstallationKind, field string) (domain.InstallationKind, error) {
	switch value {
	case worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER:
		return domain.Sprinkler, nil
	case worldv1.InstallationKind_INSTALLATION_KIND_DRIP:
		return domain.Drip, nil
	case worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT:
		return domain.TreatmentPlant, nil
	default:
		return 0, &UnknownEnumError{Field: field, Value: int32(value)}
	}
}

func upgradeToProto(kind domain.UpgradeKind) worldv1.UpgradeKind {
	switch kind {
	case domain.BiggerDrop:
		return worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_DROP
	case domain.BiggerTank:
		return worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_TANK
	default:
		return worldv1.UpgradeKind_UPGRADE_KIND_UNSPECIFIED
	}
}

func upgradeFromProto(value worldv1.UpgradeKind) (domain.UpgradeKind, error) {
	switch value {
	case worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_DROP:
		return domain.BiggerDrop, nil
	case worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_TANK:
		return domain.BiggerTank, nil
	default:
		return 0, &UnknownEnumError{Field: "PlayerRecord.upgrades", Value: int32(value)}
	}
}
