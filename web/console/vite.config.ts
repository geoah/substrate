/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// In production the substrate serves the console at `/` beside its own API, so
// the app only ever speaks same-origin paths. Dev keeps that true: the console
// runs on :5173 and `/api`, `/healthz` and `/.well-known` are proxied to the
// substrate — nothing else is, so every other path stays the console's own.
const SUBSTRATE = process.env.VITE_PROXY_SUBSTRATE ?? "http://localhost:8080"

const proxyTarget = { target: SUBSTRATE, changeOrigin: true }

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": proxyTarget,
      "/healthz": proxyTarget,
      "/.well-known": proxyTarget,
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test-setup.ts"],
  },
})
