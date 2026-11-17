import { SubstrateApp, packageDetail, webManifest } from "@substrate/react"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./App"
import { packageVersion } from "./package"
import { linkKindsStore, useStore } from "./state"
import "./index.css"

// app.json's ids, package and scopes: the approval installs the published
// package at `packageVersion` or later, as the Apple and Android builds ask, and
// reads every kind with a status, which is how the app finds the
// collections the owner may turn on.
const manifest = webManifest({
  id: "tasks",
  name: "Substrate Tasks",
  scopes: [
    "write:substrate.reamde.dev/tasks/*",
    "read:implements(substrate.reamde.dev/core/lifecycle)",
  ],
  details: [packageDetail("substrate.reamde.dev/tasks", packageVersion)],
  packageNames: { "substrate.reamde.dev/tasks": "Tasks" },
  softwareVersion: __APP_VERSION__,
})

/** The shell over the kinds the app reads now: the collections the owner
 * turned on change them, and the open link takes the change. */
function Root() {
  const kinds = useStore(linkKindsStore)
  return (
    <SubstrateApp
      manifest={manifest}
      kinds={kinds}
      kept="tasks"
      tagline="Your to-do list, kept in your own Substrate."
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
  )
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Root />
  </StrictMode>
)
