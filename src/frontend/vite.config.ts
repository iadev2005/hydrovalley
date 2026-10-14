/// <reference types="vitest/config" />
import { resolve } from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Destino del plan B (nginx, HTTPS con certificado autofirmado) para el
// servidor de desarrollo; se cambia con VITE_PROXY_TARGET si el stack corre en
// otro puerto. `secure: false` acepta ese certificado autofirmado.
const target = process.env.VITE_PROXY_TARGET ?? "https://localhost:8080";

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
      "/api": { target, changeOrigin: true, secure: false },
      // El hub compara Origin con Host: sin changeOrigin, el Host sigue siendo
      // el de Vite (localhost:5173), igual que el Origin del navegador.
      "/play": { target, ws: true, changeOrigin: false, secure: false },
    },
  },
  test: {
    include: ["src/**/*.test.{ts,tsx}"],
    environment: "node",
    setupFiles: ["src/test-setup.ts"],
    testTimeout: 10000,
  },
});
