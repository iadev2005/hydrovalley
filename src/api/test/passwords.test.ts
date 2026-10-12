import { describe, expect, it } from "vitest";
import { createPasswordService } from "../src/passwords.js";

describe("password service", () => {
  it("hashes with Argon2id using the OWASP parameters (m=19456, t=2, p=1)", async () => {
    const passwords = await createPasswordService();
    const hash = await passwords.hash("correct horse");

    expect(hash).toMatch(/^\$argon2id\$v=19\$m=19456,t=2,p=1\$/);
    expect(hash).not.toContain("correct horse");
  });

  it("verifies the right password and rejects a wrong one", async () => {
    const passwords = await createPasswordService();
    const hash = await passwords.hash("correct horse");

    expect(await passwords.verify(hash, "correct horse")).toBe(true);
    expect(await passwords.verify(hash, "wrong horse")).toBe(false);
  });

  it("uses a different salt every time", async () => {
    const passwords = await createPasswordService();

    expect(await passwords.hash("same")).not.toBe(await passwords.hash("same"));
  });

  it("computes a dummy hash once with the same parameters", async () => {
    const passwords = await createPasswordService();

    expect(passwords.dummyHash).toMatch(/^\$argon2id\$v=19\$m=19456,t=2,p=1\$/);
    expect(await passwords.verify(passwords.dummyHash, "anything")).toBe(false);
  });

  it("treats a malformed stored hash as a failed verification", async () => {
    const passwords = await createPasswordService();

    expect(await passwords.verify("not-a-hash", "x")).toBe(false);
  });
});
