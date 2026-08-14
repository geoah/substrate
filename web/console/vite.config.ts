/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// One host serves both the substrate API and the connectors control plane in
// production; dev talks to it through this proxy so the app itself only ever
// speaks same-origin paths. `/connectors` is NOT proxied: the console's own
// /connectors route reads the entity surface (core.substrate.reamde.dev/connectors +
// syncruns) and never calls the legacy control plane — proxying it would
// shadow the page with the API's 404 (slice 4).
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
