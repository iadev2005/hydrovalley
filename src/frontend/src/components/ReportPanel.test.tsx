// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ReportPanel } from "./ReportPanel.tsx";
import type { ReportView } from "../logic/reports.ts";

const mockReport: ReportView = {
  scope: "me",
  period: "Últimas 24 horas",
  note: "Agua reciclada en plantas",
  rows: [
    { key: "reduction", label: "Reducción", value: "35%" },
    { key: "reuse", label: "Reutilización", value: "80 L" },
  ],
};

describe("ReportPanel", () => {
  it("renders 3R rows, period and note", () => {
    const onSelectScope = vi.fn();
    render(
      <ReportPanel
        report={mockReport}
        reportScope="me"
        statusText=""
        onSelectScope={onSelectScope}
      />,
    );

    expect(screen.getByText("Últimas 24 horas")).toBeDefined();
    expect(screen.getByText("Agua reciclada en plantas")).toBeDefined();
    expect(screen.getByText("Reducción")).toBeDefined();
    expect(screen.getByText("35%")).toBeDefined();

    const globalTab = screen.getByRole("tab", { name: "Valle" });
    fireEvent.click(globalTab);
    expect(onSelectScope).toHaveBeenCalledWith("global");
  });
});
