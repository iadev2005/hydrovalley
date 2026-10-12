import type { HubStats } from "../../src/hub-stats.js";

export const VALID_STATS: HubStats = {
  tick: "18446744073709551615",
  paused: false,
  breaker: "closed",
  queue: 3,
  sessions: 6,
  goroutines: 25,
  actions_per_second: { admitted: 1.1, applied: 1, window_seconds: 10 },
  latency_ms: { p50: 77.8, p95: 107.3, samples: 11 },
  step_ms: { last: 1.2, p95: 2.57, failures_total: 0 },
  humidity_total: "12345",
  humidity_contributed: "99",
  accepted_drops: "42",
  active_fires: 2,
  boot_id: "6f1c2f6e-0000-4000-8000-000000000001",
  restored_tick: "50",
  generated_at: "2026-10-03T12:00:00Z",
};
