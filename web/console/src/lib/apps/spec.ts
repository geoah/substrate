/** THE CONTRACT between an app record and the host that mounts it.
 *
 * An app is one `substrate.reamde.dev/core/app` record. `appSpec` (in
 * `app-spec.ts`) decodes it once into an `AppSpec`: every reference already
 * read down to the identity it names, every default resolved (`attach` to the
 * launcher, `sdk` to 1), and every problem one row can answer collected under
 * `problems` with the path it sits at. Nothing in an AppSpec executes: the
 * `source` is text the guest transforms and runs behind the bridge, and the
 * grant is what the bridge holds every call to (`lib/apps/bridge/host.ts`).
 *
 * Problems have two severities. An `error` blocks the mount (the screen shows
 * `problems.tsx` instead of the frame): a source over the cap, a reserved
 * module key, an SDK major the host does not serve, a package below the floor
 * the app requires. A `warning` greys or dents: a grant reference that
 * resolves to no kind (the launcher greys the row and names the package), a
 * `via` no read kind declares. */

/** The two shells the guest runs. The set is closed and moves with the
 * binary; a value is permanent once a live row carries it. */
export type Runtime = "react" | "html"

export type Attach = "launcher" | "home" | "record" | "browse"

/** The SDK major this console serves. A record whose `sdk` is above it is
 * refused before anything is mounted. */
export const SDK_MAJOR = 1

/** The cap the host holds `source` to, in bytes: what `function.source` is
 * held to. The server has no byte `max` for text. */
export const SOURCE_CAP = 262_144

/** The specifiers the guest's import map already owns; a `modules` key
 * spelled as one would shadow the SDK, React or the kit. */
export const RESERVED_MODULES: readonly string[] = [
  "react",
  "react/jsx-runtime",
  "react-dom/client",
  "substrate/app",
  "substrate/ui",
]

/** The name the entry module carries: the app's `source`, transformed and
 * minted first (`frame/load.ts`). It is not an import map key, nothing
 * imports it and the bootstrap takes its blob URL directly, so it is
 * reserved apart from `RESERVED_MODULES`, which the prompt hands the model
 * as the specifiers it may import. A `modules.source` is refused all the
 * same: it would share the entry's name in every runtime error, and with a
 * loader that keyed the entry by name, its place. */
export const SOURCE_MODULE = "source"

/** One thing wrong with an app, at the path it sits at
 * (`permissions.reads.kinds[1]`, `modules.react`). */
export interface Problem {
  path: string
  message: string
  severity: "error" | "warning"
}

/** The grant, references read down to identities: kinds and traits for
 * reads, kinds for writes, function identities for `call`, agent identities
 * for `agents`. Absent arms are empty lists, which refuse everything. */
export interface Grant {
  reads: { kinds: string[]; traits: string[] }
  writes: string[]
  call: string[]
  agents: string[]
}

export interface InputSpec {
  /** The kind identity whose records satisfy the input; empty when the row's
   * reference was malformed (a problem says so). */
  kind: string
  description?: string
}

export interface AppSpec {
  /** The app record's id: the `/apps/$id` segment. */
  id: string
  name: string
  description?: string
  /** A lucide icon name, kebab-case. */
  icon?: string
  runtime: Runtime
  source: string
  /** Further modules, keyed by the name they are imported as (`#name`). */
  modules: Record<string, string>
  /** The SDK major the source was written against. */
  sdk: number
  permissions: Grant
  inputs: Record<string, InputSpec>
  /** Input name → the bound record's path. */
  bindings: Record<string, string>
  /** Package identity → least version this app renders against. */
  requiresAtLeast: Record<string, number>
  attach: Attach[]
  /** The reference property that scopes the app on a record page. */
  via?: string
  home: boolean
  problems: Problem[]
}

/** What the guest or the host reports about a running app, in the shape the
 * errors strip draws (`components/apps/errors.tsx`): the phase that failed,
 * the message, and for the two code phases the author's own line and column
 * in `source` or in one of `modules`. A `grant` error names the call and the
 * kind the grant lacks; a `provenance` error says the row needs the owner's
 * review. */
export interface AppError {
  phase: "transform" | "runtime" | "grant" | "provenance" | "host"
  message: string
  /** The property the fix lands on (`permissions.reads.kinds`). */
  path?: string
  line?: number
  column?: number
  /** `source`, or a key of `modules`. */
  module?: string
  stack?: string
}

export function blockingProblems(problems: Problem[]): Problem[] {
  return problems.filter((p) => p.severity === "error")
}
