/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// The server serves the console at `/` and its own API beside it, so the app
// only ever speaks same-origin paths. Dev serves the console from vite instead,
// and these three prefixes are what it hands to the substrate: everything else
// is a console route the router owns.
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
