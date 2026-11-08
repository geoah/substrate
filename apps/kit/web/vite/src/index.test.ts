import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { pathToFileURL } from "node:url"
import { describe, expect, it } from "vitest"
import { substrateSite } from "./index"

describe("substrateSite", () => {
  it("stamps the version of the Vite root's package, wherever the build runs from", () => {
    const app = mkdtempSync(join(tmpdir(), "substrate-site-"))
    writeFileSync(
      join(app, "app.json"),
      JSON.stringify({ id: "template", name: "Substrate Template" })
    )
    mkdirSync(join(app, "web"))
    writeFileSync(
      join(app, "web", "package.json"),
      JSON.stringify({ name: "@substrate/template-web", version: "1.4.2" })
    )
    const plugin = substrateSite({
      app: pathToFileURL(join(app, "app.json")),
      shortName: "Template",
    })
    const config = plugin.config as (
      user: { root?: string },
      env: { command: "build" | "serve"; mode: string }
    ) => { define: Record<string, string> }
    const built = config(
      { root: join(app, "web") },
      { command: "build", mode: "production" }
    )
    expect(built.define.__APP_VERSION__).toBe('"1.4.2"')
  })
})
