// A stub the mychart importer's workstream replaces.
import { Effect } from "effect"
import type { Importer } from "../types"

export const mychart: Importer = {
  id: "mychart",
  name: "mychart",
  description: "Not built yet.",
  accepts: { kind: "folder" },
  detect: () => Effect.succeed(undefined),
  questions: () => Effect.succeed([]),
  read: () => Effect.fail({ _tag: "NotMine", message: "Not built yet." }),
}
