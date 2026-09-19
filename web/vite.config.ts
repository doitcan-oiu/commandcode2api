import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const apiTarget = process.env.VITE_API_TARGET || "http://127.0.0.1:3050";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  // Preserve the browser's Host header so the backend's same-origin checks
  // continue to protect mutations when requests pass through Vite in development.
  server: { host: "127.0.0.1", proxy: { "/api": apiTarget, "/v1": apiTarget } },
  test: { environment: "jsdom", setupFiles: "./src/test/setup.ts" },
});
