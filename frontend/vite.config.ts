import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    // `npm run dev` talks to a local `envgrid serve` (with ENVGRID_SECURE_COOKIES=false).
    proxy: { "/api": "http://localhost:8080" },
  },
  build: { outDir: "dist", emptyOutDir: true },
  test: { include: ["src/**/*.test.ts"] },
});
