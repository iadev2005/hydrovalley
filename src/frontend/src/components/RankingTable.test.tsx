// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { RankingTable } from "./RankingTable.tsx";
import type { RankingView } from "../logic/reports.ts";

const mockRanking: RankingView = {
  tick: "123",
  rows: [
    { rank: 1, username: "alice", leaves: "5000", isMe: true },
    { rank: 2, username: "bob", leaves: "3200", isMe: false },
  ],
};

describe("RankingTable", () => {
  it("renders leaderboard rows and marks current user with class me", () => {
    const onRefresh = vi.fn();
    render(
      <RankingTable
        ranking={mockRanking}
        statusText="Tick 123"
        onRefresh={onRefresh}
      />,
    );

    expect(screen.getByText("alice")).toBeDefined();
    expect(screen.getByText("bob")).toBeDefined();
    expect(screen.getByText("Tick 123")).toBeDefined();

    const aliceRow = screen.getByText("alice").closest("tr");
    expect(aliceRow?.className).toBe("me");

    const refreshBtn = screen.getByRole("button", { name: "Actualizar" });
    fireEvent.click(refreshBtn);
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });
});
