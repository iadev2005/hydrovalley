/// <reference types="vitest/config" />
import { resolve } from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Destino del plan B (nginx) para el servidor de desarrollo; se cambia con
// VITE_PROXY_TARGET si el stack corre en otro puerto.
const target = process.env.VITE_PROXY_TARGET ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      // Dos páginas: el juego y el panel del sistema.
      input: {
        main: resolve(import.meta.dirname, "index.html"),
        sistema: resolve(import.meta.dirname, "sistema.html"),
      },
    },
  },
  server: {
    proxy: {
      "/api": { target, changeOrigin: true },
      "/play": { target, ws: true, changeOrigin: true },
    },
  },
  test: {
    include: ["src/**/*.test.{ts,tsx}"],
    environment: "node",
  },
});
