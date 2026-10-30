// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import {
  useKeyboardShortcuts,
  isEditableElement,
  type UseKeyboardShortcutsOptions,
} from "./useKeyboardShortcuts.ts";

describe("useKeyboardShortcuts", () => {
  let defaultOptions: UseKeyboardShortcutsOptions;

  beforeEach(() => {
    defaultOptions = {
      enabled: true,
      selected: { x: 5, y: 5 },
      onSelectTile: vi.fn(),
      onDrop: vi.fn(),
      onPlaceInstallation: vi.fn(),
      onToggleView: vi.fn(),
      onFocusFire: vi.fn(),
      onCenterSelected: vi.fn(),
      onResetCamera: vi.fn(),
      canActOnTile: true,
      gridSize: 32,
    };
  });

  const fireKey = (key: string, options: Partial<KeyboardEventInit> = {}) => {
    const event = new KeyboardEvent("keydown", {
      key,
      bubbles: true,
      cancelable: true,
      ...options,
    });
    window.dispatchEvent(event);
    return event;
  };

  it("navigates selection with WASD and Arrow keys", () => {
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey("ArrowUp");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 5, y: 4 });

    fireKey("w");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 5, y: 4 });

    fireKey("ArrowDown");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 5, y: 6 });

    fireKey("s");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 5, y: 6 });

    fireKey("ArrowLeft");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 4, y: 5 });

    fireKey("a");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 4, y: 5 });

    fireKey("ArrowRight");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 6, y: 5 });

    fireKey("d");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 6, y: 5 });
  });

  it("selects center tile if no tile is currently selected when moving", () => {
    defaultOptions.selected = null;
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey("w");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 16, y: 15 });
  });

  it("clamps coordinates to grid boundaries (0 to gridSize - 1)", () => {
    defaultOptions.selected = { x: 0, y: 0 };
    const { rerender } = renderHook((opts) => useKeyboardShortcuts(opts), {
      initialProps: defaultOptions,
    });

    fireKey("ArrowUp");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 0, y: 0 });

    fireKey("ArrowLeft");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 0, y: 0 });

    const atMax = { ...defaultOptions, selected: { x: 31, y: 31 } };
    rerender(atMax);

    fireKey("ArrowDown");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 31, y: 31 });

    fireKey("ArrowRight");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith({ x: 31, y: 31 });
  });

  it("triggers onDrop with Space or Enter when canActOnTile is true", () => {
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey(" ");
    expect(defaultOptions.onDrop).toHaveBeenCalledTimes(1);

    fireKey("Enter");
    expect(defaultOptions.onDrop).toHaveBeenCalledTimes(2);
  });

  it("does not trigger onDrop when canActOnTile is false", () => {
    defaultOptions.canActOnTile = false;
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey(" ");
    expect(defaultOptions.onDrop).not.toHaveBeenCalled();
  });

  it("triggers onPlaceInstallation with keys 1, 2, 3", () => {
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey("1");
    expect(defaultOptions.onPlaceInstallation).toHaveBeenCalledWith("sprinkler");

    fireKey("2");
    expect(defaultOptions.onPlaceInstallation).toHaveBeenCalledWith("drip");

    fireKey("3");
    expect(defaultOptions.onPlaceInstallation).toHaveBeenCalledWith("treatment_plant");
  });

  it("triggers tactical shortcuts (F, C, R, V, Esc)", () => {
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey("f");
    expect(defaultOptions.onFocusFire).toHaveBeenCalledTimes(1);

    fireKey("c");
    expect(defaultOptions.onCenterSelected).toHaveBeenCalledTimes(1);

    fireKey("r");
    expect(defaultOptions.onResetCamera).toHaveBeenCalledTimes(1);

    fireKey("0");
    expect(defaultOptions.onResetCamera).toHaveBeenCalledTimes(2);

    fireKey("v");
    expect(defaultOptions.onToggleView).toHaveBeenCalledTimes(1);

    fireKey("Escape");
    expect(defaultOptions.onSelectTile).toHaveBeenCalledWith(null);
  });

  it("ignores hotkeys when typing inside an editable element", () => {
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();

    expect(isEditableElement(document.activeElement)).toBe(true);

    fireKey(" ");
    fireKey("w");
    fireKey("1");
    fireKey("f");

    expect(defaultOptions.onDrop).not.toHaveBeenCalled();
    expect(defaultOptions.onSelectTile).not.toHaveBeenCalled();
    expect(defaultOptions.onPlaceInstallation).not.toHaveBeenCalled();
    expect(defaultOptions.onFocusFire).not.toHaveBeenCalled();

    document.body.removeChild(input);
  });

  it("ignores hotkeys with browser modifiers (Ctrl, Alt, Meta)", () => {
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey("r", { ctrlKey: true });
    fireKey("v", { metaKey: true });
    fireKey("1", { altKey: true });

    expect(defaultOptions.onResetCamera).not.toHaveBeenCalled();
    expect(defaultOptions.onToggleView).not.toHaveBeenCalled();
    expect(defaultOptions.onPlaceInstallation).not.toHaveBeenCalled();
  });

  it("does not listen when enabled is false", () => {
    defaultOptions.enabled = false;
    renderHook(() => useKeyboardShortcuts(defaultOptions));

    fireKey(" ");
    expect(defaultOptions.onDrop).not.toHaveBeenCalled();
  });
});
