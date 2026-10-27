import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { GameApp } from "./containers/GameApp.tsx";
import "./fonts.ts";
import "../styles.css";

const rootElement = document.getElementById("root");
if (rootElement) {
  createRoot(rootElement).render(
    <StrictMode>
      <GameApp />
    </StrictMode>,
  );
}
