/** The text an agent is handed before it writes an app: the rules the guest
 * and the bridge enforce, spelled as a checklist, and the kinds this
 * repository holds in their rehomed spelling so every reference the model
 * writes is one the grant can resolve. v0 carries the checklist and the
 * kinds; v1 adds the SDK's generated `.d.ts`, the kit's catalog and the
 * examples, assembled from the build rather than written here. */

import type { KindInfo } from "@/lib/api/types"
import { RESERVED_MODULES, SDK_MAJOR } from "./spec"

const CHECKLIST = [
  "Default-export one React component from `source` (`runtime: react`), or write a whole HTML document (`runtime: html`).",
  `Import only ${RESERVED_MODULES.map((m) => `\`${m}\``).join(", ")} and your own \`#<module>\` keys; nothing else resolves.`,
  "No `fetch`, no `window.open`, no `localStorage`: the guest has no network and no origin. Records come through `substrate/app` alone.",
  "Every kind the source lists, gets, puts, patches, transitions or subscribes to appears in `permissions.reads.kinds` or `permissions.writes`; a trait in `permissions.reads.traits` reads every kind that implements it.",
  "A state moves by `records.transition`, never by writing the property.",
  "A filter is the records API's JSON grammar: `eq`, `in`, `gte`, `lt`, `contains` (membership, not substring). `orderBy` is `<property>:asc|desc`, camelCase.",
  "`source` and `modules` are written by `propose`, never `mutate`; the owner accepts before the code runs.",
  `Write \`sdk: ${SDK_MAJOR}\`, or leave it out.`,
]

/** One line per kind: the identity and the properties it declares. */
function kindLine(kind: KindInfo): string {
  const properties = kind.definition.properties
  const names =
    properties && typeof properties === "object"
      ? Object.keys(properties as Record<string, unknown>).sort()
      : []
  return `- ${kind.identity}${names.length ? `: ${names.join(", ")}` : ""}`
}

/** The prompt, assembled from the live registry. `kinds` is every kind the
 * repository holds, or the subset the app will touch. */
export function appPrompt(kinds: KindInfo[]): string {
  const rules = CHECKLIST.map((rule, i) => `${i + 1}. ${rule}`).join("\n")
  const registry = [...kinds]
    .sort((a, b) => a.identity.localeCompare(b.identity))
    .map(kindLine)
    .join("\n")
  return [
    "# Writing a Substrate app",
    "",
    "An app is one `substrate.reamde.dev/core/app` record: a `name`, a `runtime`, a `source`, and a `permissions` grant. The console runs the source behind a sandboxed guest and checks every call against the grant.",
    "",
    "## Rules",
    "",
    rules,
    "",
    "## Kinds in this repository",
    "",
    registry || "(none)",
    "",
  ].join("\n")
}
