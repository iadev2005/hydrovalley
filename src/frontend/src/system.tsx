import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { SystemApp } from "./containers/SystemApp.tsx";
import "../styles.css";

const rootElement = document.getElementById("root");
if (rootElement) {
  createRoot(rootElement).render(
    <StrictMode>
      <SystemApp />
    </StrictMode>,
  );
}
