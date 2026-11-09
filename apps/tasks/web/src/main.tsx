import { SubstrateApp, packageDetail, webManifest } from "@substrate/react"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./App"
import { List, Task } from "./model"
import "./index.css"

// app.json's ids, package and scope: the approval installs the published
// package at version 1 or later, as the Apple and Android builds ask.
const manifest = webManifest({
  id: "tasks",
  name: "Substrate Tasks",
  scopes: ["write:substrate.reamde.dev/tasks/*"],
  details: [packageDetail("substrate.reamde.dev/tasks", 2)],
  packageNames: { "substrate.reamde.dev/tasks": "Tasks" },
  softwareVersion: __APP_VERSION__,
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <SubstrateApp
      manifest={manifest}
      kinds={[Task, List]}
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
  </StrictMode>
)
