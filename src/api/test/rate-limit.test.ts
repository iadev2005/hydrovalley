import { describe, expect, it } from "vitest";
import { createAttemptLimiter } from "../src/rate-limit.js";

describe("attempt limiter", () => {
  it("allows up to the limit and then blocks with a retry delay", () => {
    let now = 0;
    const limiter = createAttemptLimiter({ limit: 3, windowMs: 60_000, now: () => now });

    for (let i = 0; i < 3; i++) {
      expect(limiter.hit("ana|1.1.1.1").allowed).toBe(true);
    }
    now = 20_000;
    expect(limiter.hit("ana|1.1.1.1")).toEqual({ allowed: false, retryAfterSeconds: 40 });
  });

  it("counts each key on its own", () => {
    const limiter = createAttemptLimiter({ limit: 1, windowMs: 60_000, now: () => 0 });

    expect(limiter.hit("a").allowed).toBe(true);
    expect(limiter.hit("b").allowed).toBe(true);
    expect(limiter.hit("a").allowed).toBe(false);
  });

  it("starts a fresh window once the previous one ends", () => {
    let now = 0;
    const limiter = createAttemptLimiter({ limit: 1, windowMs: 1000, now: () => now });

    limiter.hit("a");
    expect(limiter.hit("a").allowed).toBe(false);
    now = 1000;
    expect(limiter.hit("a").allowed).toBe(true);
  });

  it("drops expired entries so memory stays bounded", () => {
    let now = 0;
    const limiter = createAttemptLimiter({
      limit: 1,
      windowMs: 1000,
      now: () => now,
      maxKeys: 10,
    });

    for (let i = 0; i < 10; i++) limiter.hit(`k${String(i)}`);
    expect(limiter.size()).toBe(10);
    now = 2000;
    limiter.hit("fresh");
    expect(limiter.size()).toBe(1);
  });

  it("evicts the oldest entry instead of growing past the cap", () => {
    const limiter = createAttemptLimiter({
      limit: 1,
      windowMs: 60_000,
      now: () => 0,
      maxKeys: 2,
    });

    limiter.hit("a");
    limiter.hit("b");
    expect(limiter.hit("c").allowed).toBe(true);
    expect(limiter.size()).toBe(2);
    // "a" fue expulsada: vuelve a empezar su ventana.
    expect(limiter.hit("a").allowed).toBe(true);
  });
});
