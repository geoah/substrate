// A stub the randox importer's workstream replaces.
import { Effect } from "effect"
import type { Importer } from "../types"

export const randox: Importer = {
  id: "randox",
  name: "randox",
  description: "Not built yet.",
  accepts: { kind: "folder" },
  detect: () => Effect.succeed(undefined),
  questions: () => Effect.succeed([]),
  read: () => Effect.fail({ _tag: "NotMine", message: "Not built yet." }),
}
