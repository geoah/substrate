import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { substrateSite } from "@substrate/vite"
import { defineConfig } from "vite"

// SUBSTRATE_WEB_URL is where a build is served (an https URL ending in /);
// without it, the build is a local app served at / (@substrate/vite).
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    substrateSite({
      app: new URL("../app.json", import.meta.url),
      shortName: "Tasks",
    }),
  ],
  server: { port: 5173, strictPort: true },
  build: { target: "es2022", sourcemap: true },
})
