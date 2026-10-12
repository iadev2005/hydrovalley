import { useState } from "react";

export interface AdminPanelProps {
  hidden?: boolean;
  isOpen: boolean;
  demoMode: boolean;
  onToggleDemo: () => void;
  onResetValley: () => void;
}

export function AdminPanel({
  hidden = false,
  isOpen,
  demoMode,
  onToggleDemo,
  onResetValley,
}: AdminPanelProps) {
  const [confirmingReset, setConfirmingReset] = useState(false);

  const handleResetYes = () => {
    setConfirmingReset(false);
    onResetValley();
  };

  const handleResetNo = () => {
    setConfirmingReset(false);
  };

  return (
    <section id="admin-panel" hidden={hidden} aria-labelledby="admin-title">
      <h3 id="admin-title">Administración</h3>
      <div className="actions">
        <button
          id="demo-toggle"
          type="button"
          disabled={!isOpen}
          onClick={onToggleDemo}
        >
          {demoMode ? "Desactivar modo demo" : "Activar modo demo"}
        </button>
        <button
          id="reset"
          type="button"
          disabled={!isOpen}
          onClick={() => setConfirmingReset(true)}
        >
          Reiniciar valle
        </button>
      </div>
      <p id="reset-confirm" className="meta" hidden={!confirmingReset}>
        Se borrará el progreso de todos los jugadores. ¿Confirmas?{" "}
        <button id="reset-yes" type="button" onClick={handleResetYes}>
          Sí, reiniciar
        </button>{" "}
        <button id="reset-no" type="button" onClick={handleResetNo}>
          Cancelar
        </button>
      </p>
    </section>
  );
}
