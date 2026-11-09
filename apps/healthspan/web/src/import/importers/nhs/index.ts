// A stub the nhs importer's workstream replaces.
import { Effect } from "effect"
import type { Importer } from "../types"

export const nhs: Importer = {
  id: "nhs",
  name: "nhs",
  description: "Not built yet.",
  accepts: { kind: "folder" },
  detect: () => Effect.succeed(undefined),
  questions: () => Effect.succeed([]),
  read: () => Effect.fail({ _tag: "NotMine", message: "Not built yet." }),
}
