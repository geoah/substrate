# @substrate/vite

A Substrate web app's build, one plugin:

```ts
import { substrateSite } from "@substrate/vite"

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    substrateSite({
      app: new URL("../app.json", import.meta.url),
      shortName: "Template",
    }),
  ],
})
```

`SUBSTRATE_WEB_URL` is where the build is served, an https URL ending in
`/`. It names the deployment, so it comes from the environment, never from
the tree. With it, the build's base is that URL's path and it writes
`client.json`, the client metadata document the server fetches at the app's
client id (`<url>client.json`, one redirect `<url>`). Without it, the build
is served at `/` and signs in as a local app from a loopback address. Either
way it writes `manifest.webmanifest` from `app.json`, defines
`__APP_VERSION__` (`SUBSTRATE_WEB_VERSION`, else the web package's version)
and serves `pnpm dev` on 127.0.0.1.
