import { SubstrateApp, packageDetail, webManifest } from "@substrate/react"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./App"
import { Item } from "./kinds"
import "./index.css"

// app.json's ids, package and scope: the approval installs the published
// package at version 1 or later, as the Apple build asks.
const manifest = webManifest({
  id: "template",
  name: "Substrate Template",
  scopes: ["write:substrate.reamde.dev/template/*"],
  details: [packageDetail("substrate.reamde.dev/template", 1)],
  packageNames: { "substrate.reamde.dev/template": "Template" },
  softwareVersion: __APP_VERSION__,
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <SubstrateApp
      manifest={manifest}
      kinds={[Item]}
      kept="items"
      tagline="Your items, kept in your own Substrate."
      icon={
        <img
          src={`${import.meta.env.BASE_URL}icon-512.png`}
          alt=""
          className="size-24 rounded-[22%] shadow-md"
        />
      }
    >
      <App />
    </SubstrateApp>
  </StrictMode>
)
