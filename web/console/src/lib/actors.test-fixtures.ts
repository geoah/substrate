/** Actor records the tests read names from, as the engine writes them
 * (decision 0270): one per hand, linked by an opaque id. seedActors puts
 * them in the actor directory, as the app shell does from the server's. */

import { setActorDirectory } from "@/lib/actor-directory"
import { CORE_PACKAGE } from "@/lib/core-kinds"
import type { ActorInfo, ActorType } from "@/lib/api/actors"

function actor(
  id: string,
  type: ActorType,
  name: string,
  extra: Partial<ActorInfo> = {}
): ActorInfo {
  return { id, type, name, disabled: false, ...extra }
}

const fn = (id: string, link: string) => actor(id, "function", link, { link })
const agent = (id: string, link: string) => actor(id, "agent", link, { link })
const pkg = (id: string, link: string) => actor(id, "package", link, { link })

/** The fixture's actors by what they are. */
export const ACTORS = {
  person: actor("act-person", "person", "ada.localhost"),
  system: actor("act-system", "system", "substrate"),
  localApp: actor("act-local-app", "app", "local", { publisher: "local" }),
  notesApp: actor("act-notes-app", "app", "notes.example.org", {
    publisher: "notes.example.org",
  }),
  substrateAgent: agent("act-agent-substrate", "ada.localhost/llm/substrate"),
  editorAgent: agent("act-agent-editor", "ada.localhost/llm/editor"),
  googleContactsSync: fn(
    "act-fn-google-synccontacts",
    "providers.substrate.reamde.dev/google/synccontacts"
  ),
  linearSync: fn(
    "act-fn-linear-linearsync",
    "providers.substrate.reamde.dev/linear/linearsync"
  ),
  beeperMessagesSync: fn(
    "act-fn-beeper-messagessync",
    "providers.substrate.reamde.dev/beeper/messagessync"
  ),
  beeperSync: fn(
    "act-fn-beeper-beepersync",
    "providers.substrate.reamde.dev/beeper/beepersync"
  ),
  githubSync: fn(
    "act-fn-github-githubsync",
    "providers.substrate.reamde.dev/github/githubsync"
  ),
  notesSave: fn("act-fn-notes-save", "ada.localhost/notes/save"),
  googlePackage: pkg("act-pkg-google", "providers.substrate.reamde.dev/google"),
  notesPackage: pkg("act-pkg-notes", "ada.localhost/notes"),
  readingListPackage: pkg("act-pkg-readinglist", "ada.localhost/readinglist"),
  corePackage: pkg("act-pkg-core", "substrate.reamde.dev/core"),
} satisfies Record<string, ActorInfo>

/** The other actors the tests' changes and managers link. */
const MORE: readonly ActorInfo[] = [
  agent("act-agent-llm-helper", "ada.example.com/llm/helper"),
  agent("act-agent-llm-notekeeper", "ada.example.com/llm/notekeeper"),
  agent("act-agent-llm-scribe", "ada.example.com/llm/scribe"),
  agent("act-agent-llm-substrate", "ada.localhost/llm/substrate"),
  agent("act-agent-notes-notekeeper", "ada.localhost/notes/notekeeper"),
  actor("act-app-workouts-example-org", "app", "workouts.example.org", {
    publisher: "workouts.example.org",
  }),
  fn("act-fn-google-sync", "providers.substrate.reamde.dev/google/sync"),
  fn(
    "act-fn-google-syncgmail",
    "providers.substrate.reamde.dev/google/syncgmail"
  ),
  fn(
    "act-fn-linear-linearsync",
    "providers.substrate.reamde.dev/linear/linearsync"
  ),
  fn("act-fn-people-promotecontact", "example.com/people/promotecontact"),
]

/** Fills the actor directory with the fixture's actors and any more. */
export function seedActors(...more: ActorInfo[]): void {
  setActorDirectory([...Object.values(ACTORS), ...MORE, ...more])
}

/** The link a mark names an actor record by. */
export function actorLink(id: string): { ref: string } {
  return { ref: `${CORE_PACKAGE}/actor/${id}` }
}
