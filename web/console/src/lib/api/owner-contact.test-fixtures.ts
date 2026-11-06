/** A fake of the four routes the owner's name touches: the setting's GET and
 * PUT, the contact create (`POST /api/v1/records`) and a contact's GET and
 * PATCH. It keeps what was written, so a test reads the order of the writes
 * and their bodies off `writes()`. */

import { collectionPath, seg, splitKind } from "./http"
import { RECORDS_PATH } from "./records"
import { SETTING_KIND } from "./settings"
import type { SubstrateRecord } from "./types"
import { CONTACT_KIND, OWNER_CONTACT_SETTING_ID } from "@/lib/core-kinds"

function pathOf(kind: string, id: string): string {
  const { authority, pkg, name } = splitKind(kind)
  return `${collectionPath(authority, pkg, name)}/${seg(id)}`
}

export const OWNER_SETTING_PATH = pathOf(SETTING_KIND, OWNER_CONTACT_SETTING_ID)

export const contactPath = (id: string) => pathOf(CONTACT_KIND, id)

export function json(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

const notFound = (what: string) =>
  json(404, { error: { code: "not_found", message: `${what} not found` } })

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>,
  deletedAt?: string
): SubstrateRecord {
  return {
    kind,
    id,
    version: 1,
    properties,
    labels: {},
    createdAt: "2026-11-06T00:00:00Z",
    updatedAt: "2026-11-06T00:00:00Z",
    ...(deletedAt ? { deletedAt } : {}),
  }
}

interface FakeCall {
  method: string
  path: string
  body: Record<string, unknown> | undefined
}

export function fakeOwnerServer({
  settingValue,
  contacts = [],
  failSettingWrites = 0,
  other,
}: {
  /** The stored setting's value; absent means no setting. */
  settingValue?: string
  contacts?: Array<{ id: string; name: string; deletedAt?: string }>
  /** How many setting PUTs answer 503 before one succeeds. */
  failSettingWrites?: number
  /** Answers a route this fake does not know, such as `/register`. */
  other?: (path: string, method: string) => Response | undefined
} = {}) {
  const calls: FakeCall[] = []
  const stored = new Map(
    contacts.map((c) => [
      c.id,
      record(CONTACT_KIND, c.id, { name: c.name, title: c.name }, c.deletedAt),
    ])
  )
  let setting =
    settingValue === undefined
      ? undefined
      : record(SETTING_KIND, OWNER_CONTACT_SETTING_ID, { value: settingValue })
  let minted = 0
  let failures = failSettingWrites

  function impl(
    input: RequestInfo | URL,
    init?: RequestInit
  ): Promise<Response> {
    const path =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.href
          : input.url
    const method = init?.method ?? "GET"
    const body =
      typeof init?.body === "string"
        ? (JSON.parse(init.body) as Record<string, unknown>)
        : undefined
    calls.push({ method, path, body })
    return Promise.resolve(other?.(path, method) ?? answer(path, method, body))
  }

  function answer(
    path: string,
    method: string,
    body: Record<string, unknown> | undefined
  ): Response {
    const properties = (body?.properties ?? {}) as Record<string, unknown>

    if (path === OWNER_SETTING_PATH) {
      if (method === "GET") return setting ? json(200, setting) : notFound(path)
      if (method === "PUT") {
        if (failures > 0) {
          failures--
          return json(503, {
            error: { code: "unavailable", message: "the server is busy" },
          })
        }
        setting = record(SETTING_KIND, OWNER_CONTACT_SETTING_ID, {
          ...setting?.properties,
          ...properties,
        })
        return json(200, setting)
      }
    }
    if (path === RECORDS_PATH && method === "POST") {
      const id = `c-minted-${++minted}`
      const created = record(CONTACT_KIND, id, properties)
      stored.set(id, created)
      return json(201, created)
    }
    const prefix = contactPath("")
    if (path.startsWith(prefix)) {
      const id = decodeURIComponent(path.slice(prefix.length))
      const held = stored.get(id)
      if (!held) return notFound(path)
      if (method === "GET") return json(200, held)
      if (method === "PATCH") {
        const next = {
          ...held,
          properties: { ...held.properties, ...properties },
        }
        stored.set(id, next)
        return json(200, next)
      }
    }
    return notFound(path)
  }

  return {
    impl,
    calls,
    /** Every call but the reads, in order. */
    writes: () => calls.filter((c) => c.method !== "GET"),
    contacts: stored,
    setting: () => setting,
  }
}
