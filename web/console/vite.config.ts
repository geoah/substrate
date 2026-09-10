/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// The console speaks same-origin paths only, because the server serves it at
// `/` with its own API beside it. Dev serves the console from vite instead, so
// vite forwards three prefixes to the substrate: `/api`, `/healthz` and
// `/.well-known`.
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
    // `node` is the default because most suites are pure logic: a jsdom window
    // per file costs more than every assertion in the suite put together. A
    // suite that renders declares `// @vitest-environment jsdom` on its first
    // line, and `src/test-setup.ts` installs the browser gaps only there.
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test-setup.ts"],
  },
})
