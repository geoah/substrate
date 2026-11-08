import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { readFileSync } from "node:fs"
import { defineConfig, type Plugin } from "vite"

// Where a build is served, as a URL ending in `/`, such as
// https://tasks.example.com/ or https://example.com/substrate/tasks/. It names
// the deployment, so it comes from the environment, never from this tree.
// The app's client id is `<url>client.json` and its sign-in comes back to
// `<url>`, both on that one origin (decision 0194). Without it, a build is
// served at / and signs in as a local app from a loopback address.
const siteURL = process.env.SUBSTRATE_WEB_URL
if (siteURL && !/^https:\/\/[^/]+\/(.*\/)?$/.test(siteURL)) {
  throw new Error(`SUBSTRATE_WEB_URL is an https URL ending in /: ${siteURL}`)
}
const base = siteURL ? new URL(siteURL).pathname : "/"
const app = JSON.parse(
  readFileSync(new URL("../app.json", import.meta.url), "utf8")
)
const version =
  process.env.SUBSTRATE_WEB_VERSION ??
  JSON.parse(readFileSync(new URL("package.json", import.meta.url), "utf8"))
    .version

// The client metadata document the server fetches at the client id, and the
// web manifest, both written for the URL the build is served at.
function siteDocuments(): Plugin {
  return {
    name: "substrate-site-documents",
    generateBundle() {
      if (siteURL) {
        this.emitFile({
          type: "asset",
          fileName: "client.json",
          source:
            JSON.stringify(
              {
                client_id: `${siteURL}client.json`,
                client_name: app.name,
                client_uri: siteURL,
                logo_uri: `${siteURL}icon-512.png`,
                redirect_uris: [siteURL],
                token_endpoint_auth_method: "none",
              },
              null,
              2
            ) + "\n",
        })
      }
      this.emitFile({
        type: "asset",
        fileName: "manifest.webmanifest",
        source: JSON.stringify({
          name: app.name,
          short_name: "Tasks",
          start_url: "./",
          scope: "./",
          display: "standalone",
          background_color: "#f2f2f7",
          theme_color: app.accent.light,
          icons: [
            { src: "icon-512.png", sizes: "512x512", type: "image/png" },
            {
              src: "apple-touch-icon.png",
              sizes: "180x180",
              type: "image/png",
            },
          ],
        }),
      })
    },
  }
}

export default defineConfig({
  base,
  plugins: [react(), tailwindcss(), siteDocuments()],
  define: { __APP_VERSION__: JSON.stringify(version) },
  server: { port: 5173, strictPort: true },
  build: { target: "es2022", sourcemap: true },
})
