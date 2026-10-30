import { useCallback, useEffect, useRef, useState } from "react";
import type { StateFrame } from "../logic/play.ts";

export type TacticalAlertVariant = "success" | "danger" | "warning" | "info";

export interface TacticalAlert {
  id: string;
  variant: TacticalAlertVariant;
  icon: string;
  title: string;
  message: string;
  actionLabel?: string;
  onAction?: () => void;
  durationMs?: number;
}

export interface UseTacticalAlertsOptions {
  gameState: StateFrame | null;
  onFocusFire?: () => void;
  maxAlerts?: number;
}

export function useTacticalAlerts({
  gameState,
  onFocusFire,
  maxAlerts = 4,
}: UseTacticalAlertsOptions) {
  const [alerts, setAlerts] = useState<TacticalAlert[]>([]);
  const prevFiresCountRef = useRef<number | null>(null);
  const prevInstallationsCountRef = useRef<number | null>(null);
  const prevWaterRef = useRef<bigint | null>(null);
  const isFirstStateRef = useRef<boolean>(true);

  const dismissAlert = useCallback((id: string) => {
    setAlerts((prev) => prev.filter((a) => a.id !== id));
  }, []);

  const addAlert = useCallback(
    (alert: Omit<TacticalAlert, "id">) => {
      const id = `alert-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
      const newAlert: TacticalAlert = { ...alert, id };

      setAlerts((prev) => [newAlert, ...prev].slice(0, maxAlerts));

      const timer = setTimeout(() => {
        dismissAlert(id);
      }, alert.durationMs ?? 4200);

      return () => clearTimeout(timer);
    },
    [dismissAlert, maxAlerts],
  );

  useEffect(() => {
    if (!gameState) return;

    const currentFires = gameState.fires.length;
    const currentInstallations = gameState.installations.length;
    const currentWater = gameState.player?.water ?? null;

    if (isFirstStateRef.current) {
      isFirstStateRef.current = false;
      prevFiresCountRef.current = currentFires;
      prevInstallationsCountRef.current = currentInstallations;
      prevWaterRef.current = currentWater;

      if (currentFires > 0) {
        addAlert({
          variant: "danger",
          icon: "🔥",
          title: "Incendios en curso",
          message: `Hay ${currentFires} foco(s) activo(s) en el valle.`,
          actionLabel: onFocusFire ? "Enfocar (F)" : undefined,
          onAction: onFocusFire,
          durationMs: 5000,
        });
      }
      return;
    }

    const prevFires = prevFiresCountRef.current ?? 0;
    if (currentFires > prevFires) {
      const diff = currentFires - prevFires;
      addAlert({
        variant: "danger",
        icon: "🚨",
        title: "¡Nuevo incendio detectado!",
        message: `${diff === 1 ? "Se ha iniciado 1 foco" : `Se han iniciado ${diff} focos`} de fuego en el valle.`,
        actionLabel: onFocusFire ? "Enfocar (F)" : undefined,
        onAction: onFocusFire,
        durationMs: 4500,
      });
    } else if (currentFires < prevFires) {
      const extinguished = prevFires - currentFires;
      addAlert({
        variant: "success",
        icon: "✨",
        title: "Fuego extinguido",
        message: `${extinguished === 1 ? "Se apagó 1 foco" : `Se apagaron ${extinguished} focos`} de incendio en el valle.`,
        durationMs: 3800,
      });
    }
    prevFiresCountRef.current = currentFires;

    const prevInstallations = prevInstallationsCountRef.current ?? 0;
    if (currentInstallations > prevInstallations) {
      addAlert({
        variant: "info",
        icon: "🌱",
        title: "Tecnología 3R desplegada",
        message: "Se ha añadido una nueva instalación para la sostenibilidad del valle.",
        durationMs: 3500,
      });
    }
    prevInstallationsCountRef.current = currentInstallations;

    if (
      currentWater !== null &&
      currentWater === 0n &&
      (prevWaterRef.current === null || prevWaterRef.current > 0n)
    ) {
      addAlert({
        variant: "warning",
        icon: "💧",
        title: "Reserva de agua agotada",
        message: "Tu depósito está vacío. Espera a que recargue o compra mejoras de capacidad.",
        durationMs: 4000,
      });
    }
    prevWaterRef.current = currentWater;
  }, [gameState, addAlert, onFocusFire]);

  return {
    alerts,
    addAlert,
    dismissAlert,
  };
}
