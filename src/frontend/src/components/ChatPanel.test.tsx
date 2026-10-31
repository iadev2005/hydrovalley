// @vitest-environment happy-dom
import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ChatPanel } from "./ChatPanel.tsx";
import type { ChatMessage } from "../logic/chat.ts";

describe("ChatPanel", () => {
  const sampleMessages: ChatMessage[] = [
    {
      seq: 1,
      messageId: "m1",
      senderId: "p1",
      username: "ana",
      text: "hola valle",
      kind: "player",
      sentAt: "2026-10-17T12:00:00Z",
      serverEpoch: "ep1",
    },
    {
      seq: 2,
      messageId: "m2",
      senderId: "",
      username: "",
      text: "alerta de fuego",
      kind: "system",
      sentAt: "2026-10-17T12:00:05Z",
      serverEpoch: "ep1",
    },
  ];

  it("renderiza el título, el estado y los mensajes", () => {
    render(
      <ChatPanel
        messages={sampleMessages}
        status="connected"
        statusText="conectado"
        onSendMessage={vi.fn()}
      />,
    );

    expect(screen.getByRole("heading", { name: "Chat del Valle" })).toBeTruthy();
    expect(screen.getByText("conectado")).toBeTruthy();
    expect(screen.getByText(/ana:/)).toBeTruthy();
    expect(screen.getByText("hola valle")).toBeTruthy();
    expect(screen.getByText("[Sistema]")).toBeTruthy();
    expect(screen.getByText("alerta de fuego")).toBeTruthy();
  });

  it("deshabilita el input y botón de envío si el estado no es connected", () => {
    render(
      <ChatPanel
        messages={[]}
        status="connecting"
        statusText="conectando..."
        onSendMessage={vi.fn()}
      />,
    );

    const input = screen.getByLabelText("Mensaje para el chat") as HTMLInputElement;
    const button = screen.getByRole("button", { name: "Enviar" }) as HTMLButtonElement;

    expect(input.disabled).toBe(true);
    expect(button.disabled).toBe(true);
    expect(screen.getByText("No hay mensajes recientes en el valle.")).toBeTruthy();
  });

  it("permite escribir y enviar mensaje cuando está conectado", () => {
    const handleSend = vi.fn().mockReturnValue(true);
    render(
      <ChatPanel
        messages={[]}
        status="connected"
        statusText="conectado"
        onSendMessage={handleSend}
      />,
    );

    const input = screen.getByLabelText("Mensaje para el chat") as HTMLInputElement;
    const button = screen.getByRole("button", { name: "Enviar" }) as HTMLButtonElement;

    expect(input.disabled).toBe(false);
    expect(button.disabled).toBe(true); // input vacío

    fireEvent.change(input, { target: { value: "mensaje de prueba" } });
    expect(button.disabled).toBe(false);

    fireEvent.click(button);
    expect(handleSend).toHaveBeenCalledWith("mensaje de prueba");
    expect(input.value).toBe(""); // se resetea tras envío
  });
});
