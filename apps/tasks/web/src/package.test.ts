import { describe, expect, it } from "vitest"
import { packageVersion } from "./package"

interface AppJson {
  asks: { package: string; atLeast: number }[]
}
interface Closure {
  package: string
  version: number
}

const appJson = Object.values(
  import.meta.glob<AppJson>("../../app.json", {
    eager: true,
    import: "default",
  })
)[0]
const closure = Object.values(
  import.meta.glob<Closure>(
    "../../TasksKit/Sources/TasksCore/Resources/Package/tasks.json",
    { eager: true, import: "default" }
  )
)[0]

/** The web build asks for the version the Apple and Android builds ask for:
 * app.json's floor, which is the closure they carry. A web build asking for
 * less would leave a repository on an older version through its approval. */
describe("the package floor", () => {
  it("is app.json's and the carried closure's", () => {
    const ask = appJson.asks.find(
      (a) => a.package === "substrate.reamde.dev/tasks"
    )
    expect(ask?.atLeast).toBe(packageVersion)
    expect(closure.package).toBe("substrate.reamde.dev/tasks")
    expect(closure.version).toBe(packageVersion)
  })
})
