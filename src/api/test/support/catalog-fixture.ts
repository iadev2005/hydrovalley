import type { Catalog } from "../../src/catalog-source.js";

export const MAX_U64 = 18_446_744_073_709_551_615n;

/** Catálogo de prueba con cifras distintas entre sí, para detectar campos cruzados. */
export const CATALOG: Catalog = {
  catalogVersion: "9f2b7c1d",
  drop: { waterCost: 1n, humidity: 25n, demoHumidity: 1n },
  tank: { capacity: 20n, refillAmount: 3n, refillIntervalSeconds: 2 },
  growth: {
    humidityPerStage: 100n,
    leavesGrass: 1n,
    leavesShrub: 2n,
    leavesFlowers: 4n,
    leavesTree: MAX_U64,
    treeMaxHumidity: 101n,
  },
  evaporation: { humidityPerSecond: 2n, witherAfterTicks: 100 },
  fire: {
    dropsToExtinguish: 3,
    extinguishBonusLeaves: 5n,
    burnOutTicks: 300,
    ignitionChancePpm: 50,
    propagationChancePpm: 2000,
  },
  sprinkler: { price: 10n, areaSide: 3, humidity: 26n, intervalSeconds: 4, durability: 30 },
  drip: { price: 25n, areaSide: 5, evaporationDivisor: 2, durability: 30 },
  treatmentPlant: { price: 40n, refillAmount: 6n, intervalSeconds: 7, durability: 30 },
  trashBin: { price: 15n, areaSide: 3, intervalSeconds: 2, durability: 30 },
  installationPriceIncreasePercent: 15,
  biggerDrop: { price: 30n, areaSide: 3 },
  biggerTank: { price: 20n, extraCapacity: 10n },
  offlineInstallationSeconds: 7200n,
};
