import { packageDetail, webManifest } from "@substrate/react"

/** The packages and scopes app.json names, in its order: the approval
 * installs the three packages at these versions or later (a test holds this
 * to app.json). */
export const PACKAGES = [
  ["substrate.reamde.dev/healthspan", 1],
  ["substrate.reamde.dev/health", 3],
  ["substrate.reamde.dev/labs", 2],
] as const

export const SCOPES = [
  "write:substrate.reamde.dev/healthspan/*",
  "write:substrate.reamde.dev/labs/labreport",
  "write:substrate.reamde.dev/labs/labresult",
  "read:substrate.reamde.dev/labs/*",
  "write:substrate.reamde.dev/observations/source",
  "write:substrate.reamde.dev/observations/sourcelink",
  "read:substrate.reamde.dev/observations/*",
  "read:substrate.reamde.dev/health/*",
  "read:substrate.reamde.dev/clinical/*",
  "read:substrate.reamde.dev/core/contact",
  "read:substrate.reamde.dev/core/setting",
]

export const manifest = webManifest({
  id: "healthspan",
  name: "Substrate Healthspan",
  scopes: SCOPES,
  details: PACKAGES.map(([name, version]) => packageDetail(name, version)),
  packageNames: {
    "substrate.reamde.dev/healthspan": "Healthspan",
    "substrate.reamde.dev/health": "Health",
    "substrate.reamde.dev/labs": "Labs",
  },
  softwareVersion: __APP_VERSION__,
})
