/** One app record → one `AppSpec`. Every reference is read down to the
 * identity it names (the `core/kind/`, `core/trait/`, `core/function/` and
 * `core/agent/` prefixes stripped), the defaults are filled in, and the
 * problems one row can answer are collected with the path they sit at. The
 * registry decides which grant references resolve; presence is a warning
 * here because an app may precede its package and the write never refused
 * it, and the launcher greys the row rather than hiding it. */

import {
  AGENT_RECORD_PREFIX,
  FUNCTION_RECORD_PREFIX,
  KIND_RECORD_PREFIX,
  TRAIT_RECORD_PREFIX,
} from "@/lib/api/apps"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import {
  declaredReferences,
  kindByIdentity,
  resolveReferenceTarget,
  splitKind,
} from "@/lib/definition"
import {
  RESERVED_MODULES,
  SDK_MAJOR,
  SOURCE_CAP,
  SOURCE_MODULE,
  type AppSpec,
  type Attach,
  type Grant,
  type InputSpec,
  type Problem,
  type Runtime,
} from "./spec"

const ATTACH: readonly Attach[] = ["launcher", "home", "record", "browse"]

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() ? v : undefined
}

function obj(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {}
}

/** A stored reference value read down to what it names, the given prefix
 * stripped; a bare identity is accepted as it stands. */
export function refTarget(value: unknown, prefix: string): string | undefined {
  const held = readReference(value)
  if (!held) return undefined
  return held.path.startsWith(prefix)
    ? held.path.slice(prefix.length)
    : held.path
}

function refList(value: unknown, prefix: string): string[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((v) => {
    const t = refTarget(v, prefix)
    return t ? [t] : []
  })
}

/** The package identity a kind identity belongs to, `<authority>/<package>`. */
export function packageOf(identity: string): string {
  const { authority, pkg } = splitKind(identity)
  return authority ? `${authority}/${pkg}` : identity
}

/** The message a missing grant reference carries; the launcher reads the
 * package back out of it (`missingPackages`). */
const NEEDS = "needs "

/** The packages an app's grant names and the repository lacks, in the order
 * the problems were found and without repeats. */
export function missingPackages(spec: AppSpec): string[] {
  return [
    ...new Set(
      spec.problems
        .filter(
          (p) =>
            p.severity === "warning" &&
            p.path.startsWith("permissions.") &&
            p.message.startsWith(NEEDS)
        )
        .map((p) => p.message.slice(NEEDS.length))
    ),
  ]
}

/** The size of a string on the wire, in UTF-8 bytes. */
function bytes(text: string): number {
  return new TextEncoder().encode(text).byteLength
}

export function appSpec(
  record: SubstrateRecord,
  kinds: KindInfo[],
  /** Package identity → the version the repository holds, for the
   * `requiresAtLeast` floor. Absent packages are not checked. */
  packages: Record<string, number> = {}
): AppSpec {
  const p = record.properties
  const problems: Problem[] = []

  const runtime: Runtime = p.runtime === "html" ? "html" : "react"
  if (p.runtime !== "react" && p.runtime !== "html") {
    problems.push({
      path: "runtime",
      message: `an app runs as react or html, not ${JSON.stringify(p.runtime)}`,
      severity: "error",
    })
  }

  const source = typeof p.source === "string" ? p.source : ""
  if (!source) {
    problems.push({
      path: "source",
      message: "an app has a source",
      severity: "error",
    })
  } else if (bytes(source) > SOURCE_CAP) {
    problems.push({
      path: "source",
      message: `${SOURCE_CAP} bytes is the cap`,
      severity: "error",
    })
  }

  const modules: Record<string, string> = {}
  for (const [name, text] of Object.entries(obj(p.modules))) {
    if (typeof text !== "string") continue
    const taken = RESERVED_MODULES.includes(name)
      ? `${name} is the import map's; a module is imported as #${name}`
      : name === SOURCE_MODULE
        ? `${name} is the entry's name; a module has its own`
        : undefined
    if (taken) {
      problems.push({
        path: `modules.${name}`,
        message: taken,
        severity: "error",
      })
      continue
    }
    modules[name] = text
  }

  const sdk = typeof p.sdk === "number" && p.sdk >= 1 ? Math.floor(p.sdk) : 1
  if (sdk > SDK_MAJOR) {
    problems.push({
      path: "sdk",
      message: `this console serves SDK ${SDK_MAJOR}; ${sdk} is newer than that`,
      severity: "error",
    })
  }

  const grant = obj(p.permissions)
  const reads = obj(grant.reads)
  const permissions: Grant = {
    reads: {
      kinds: refList(reads.kinds, KIND_RECORD_PREFIX),
      traits: refList(reads.traits, TRAIT_RECORD_PREFIX),
    },
    writes: refList(grant.writes, KIND_RECORD_PREFIX),
    call: refList(grant.call, FUNCTION_RECORD_PREFIX),
    agents: refList(grant.agents, AGENT_RECORD_PREFIX),
  }
  if (!Object.keys(grant).length) {
    problems.push({
      path: "permissions",
      message: "the app declares no grant, so every read and write is refused",
      severity: "warning",
    })
  }
  const missing = (path: string, identities: string[]) =>
    identities.forEach((identity, i) => {
      if (!kindByIdentity(kinds, identity)) {
        problems.push({
          path: `${path}[${i}]`,
          message: `${NEEDS}${packageOf(identity)}`,
          severity: "warning",
        })
      }
    })
  missing("permissions.reads.kinds", permissions.reads.kinds)
  missing("permissions.writes", permissions.writes)

  const inputs: Record<string, InputSpec> = {}
  for (const [name, raw] of Object.entries(obj(p.inputs))) {
    const input = obj(raw)
    const kind = refTarget(input.kind, KIND_RECORD_PREFIX) ?? ""
    inputs[name] = { kind, description: str(input.description) }
    if (!kind) {
      problems.push({
        path: `inputs.${name}.kind`,
        message: "an input names the kind whose records satisfy it",
        severity: "error",
      })
    }
  }

  const bindings: Record<string, string> = {}
  for (const [name, raw] of Object.entries(obj(p.bindings))) {
    const held = readReference(raw)
    if (held) bindings[name] = held.path
  }

  const requiresAtLeast: Record<string, number> = {}
  for (const [pkg, floor] of Object.entries(obj(p.requiresAtLeast))) {
    if (typeof floor !== "number") continue
    requiresAtLeast[pkg] = floor
    const held = packages[pkg]
    if (held !== undefined && held < floor) {
      problems.push({
        path: `requiresAtLeast.${pkg}`,
        message: `this repository holds ${pkg} at version ${held}; the app needs ${floor}`,
        severity: "error",
      })
    }
  }

  const rawAttach = Array.isArray(p.attach) ? p.attach : []
  const attach = rawAttach.filter((a): a is Attach =>
    (ATTACH as string[]).includes(String(a))
  )
  const via = str(p.via)
  if (via) {
    const declares = permissions.reads.kinds.some((identity) => {
      const kind = kindByIdentity(kinds, identity)
      return kind && declaredReferences(kind).some((r) => r.name === via)
    })
    if (!declares) {
      problems.push({
        path: "via",
        message: `no kind the app reads declares a reference called ${via}`,
        severity: "warning",
      })
    }
  }

  return {
    id: record.id,
    name: str(p.name) ?? record.id,
    description: str(p.description),
    icon: str(p.icon),
    runtime,
    source,
    modules,
    sdk,
    permissions,
    inputs,
    bindings,
    requiresAtLeast,
    attach: attach.length ? attach : ["launcher"],
    via,
    home: p.home === true,
    problems,
  }
}

/** The kind identity a `via` reference points at, resolved against the read
 * kinds: the first read kind that declares `via` as a reference decides. */
export function viaTarget(
  spec: AppSpec,
  kinds: KindInfo[]
): string | undefined {
  if (!spec.via) return undefined
  for (const identity of spec.permissions.reads.kinds) {
    const kind = kindByIdentity(kinds, identity)
    if (!kind) continue
    const ref = declaredReferences(kind).find((r) => r.name === spec.via)
    if (!ref?.to) continue
    return resolveReferenceTarget(kinds, kind, ref.to)?.identity
  }
  return undefined
}

/** Whether the app mounts as a card on a record of `kindIdentity`: with
 * `via`, the kind its via points at and no other; without, any kind it
 * reads. */
export function attachesToRecord(
  spec: AppSpec,
  kinds: KindInfo[],
  kindIdentity: string
): boolean {
  if (!spec.attach.includes("record")) return false
  if (spec.via) return viaTarget(spec, kinds) === kindIdentity
  return spec.permissions.reads.kinds.includes(kindIdentity)
}
