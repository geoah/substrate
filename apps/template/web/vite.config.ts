import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { substrateSite } from "@substrate/vite"
import { defineConfig } from "vite"

// SUBSTRATE_WEB_URL is where a build is served (an https URL ending in /);
// without it, the build is a local app served at / (@substrate/vite). The
// root is this folder wherever the build runs from: the version stamped is
// its package.json's.
export default defineConfig({
  root: import.meta.dirname,
  plugins: [
    react(),
    tailwindcss(),
    substrateSite({
      app: new URL("../app.json", import.meta.url),
      shortName: "Template",
    }),
  ],
  server: { port: 5174, strictPort: true },
  build: { target: "es2022", sourcemap: true },
})
