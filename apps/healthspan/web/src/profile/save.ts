// Saving the profile (docs/plans/healthspan.md, The package, Creating it).
// Every device names the one row, so the outbox's create, which takes a
// row it meets for its own retry and drops what it carried, never creates
// it: a create is a PUT at `ifVersion: 0` through the client, and a 409
// reads the row and compares it with what the owner filled.

import { SubstrateError, describe } from "@substrate/client"
import type { SubstrateRecord } from "@substrate/react"
import type { ProfileSave } from "../data/profile"
import { KIND, PROFILE_ID, Profile, type ProfileDraft } from "../kinds"

/** The profile's title, the `name` its kind requires. */
export const PROFILE_NAME = "Healthspan profile"

/** The fields a form fills: a 409 compares these. */
export const FILLED_FIELDS = ["birthDate", "sex"] as const

/** What a create reads and writes through. */
export interface ProfileClient {
  put(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number
  ): Promise<SubstrateRecord>
  record(kind: string, id: string): Promise<SubstrateRecord | null>
}

/** The properties of a new profile: its title, then the draft, with what a
 * new row cannot clear left out. */
export function createProperties(
  draft: Partial<ProfileDraft>
): Record<string, unknown> {
  const props = Profile.write({ name: PROFILE_NAME, ...draft })
  return Object.fromEntries(
    Object.entries(props).filter(
      ([, v]) => v !== null && !(Array.isArray(v) && v.length === 0)
    )
  )
}

export type Created =
  | { state: "created"; record: SubstrateRecord }
  /** The row was there: another device created it first. */
  | { state: "exists"; theirs: Profile }
  | { state: "failed"; words: string }

/** A PUT at `ifVersion: 0`; on a 409 the row as it stands. */
export async function createProfile(
  client: ProfileClient,
  draft: Partial<ProfileDraft>
): Promise<Created> {
  try {
    const record = await client.put(
      KIND.profile,
      PROFILE_ID,
      createProperties(draft),
      0
    )
    return { state: "created", record }
  } catch (e) {
    if (!(e instanceof SubstrateError && e.conflict))
      return { state: "failed", words: describe(e) }
  }
  try {
    const record = await client.record(KIND.profile, PROFILE_ID)
    if (!record)
      return {
        state: "failed",
        words:
          "Your Healthspan profile was deleted in your Substrate. Restore it in the console, then save again.",
      }
    return { state: "exists", theirs: Profile.read(record) }
  } catch (e) {
    return { state: "failed", words: describe(e) }
  }
}

/** How a draft stands against a row another device wrote: the filled
 * fields it holds another value in, and what the draft adds that the row
 * lacks (a filled field, the contact, `startedAt`). A field the row holds
 * nothing in is never a difference: nobody chose another value. */
export function compareWithTheirs(
  draft: Partial<ProfileDraft>,
  theirs: Profile
): { differs: (typeof FILLED_FIELDS)[number][]; adds: Partial<ProfileDraft> } {
  const differs: (typeof FILLED_FIELDS)[number][] = []
  const adds: Partial<ProfileDraft> = {}
  for (const f of FILLED_FIELDS) {
    const mine = draft[f]
    if (mine === undefined || mine === null) continue
    const there = theirs[f]
    if (there === undefined) Object.assign(adds, { [f]: mine })
    else if (there !== mine) differs.push(f)
  }
  if (draft.contact && !theirs.contact) adds.contact = draft.contact
  if (draft.startedAt && !theirs.startedAt) adds.startedAt = draft.startedAt
  return { differs, adds }
}

/** Where the profile stands when a save starts: absent, the row, or its
 * read failed. */
export type Current =
  | { state: "absent" }
  | { state: "present"; value: Profile }
  | { state: "failed"; words: string }

/** What a save needs from the screen's session. */
export interface SaveDeps {
  /** The profile once its read has answered. */
  current(): Promise<Current>
  client: ProfileClient
  /** An edit through the outbox on the version named; its words when the
   * browser could not keep it. */
  edit(
    draft: Partial<ProfileDraft>,
    version: number
  ): Promise<{ kept: true } | { kept: false; words: string }>
  /** Lays a row written past the outbox into the records the screens
   * read. */
  laid(): Promise<void>
}

/** A form's save. `seenVersion` is the version the form showed, 0 where it
 * showed no profile; left out, the version shown now. On a profile the
 * form saw it is an edit on that version (a change since parks it as a
 * conflict, Keep My Edit or Use Theirs); a profile the form never saw is
 * compared with the draft, as a create that met one is. `createWith` goes
 * on a new row and onto one that lacks it (the owner's contact). */
export async function saveProfile(
  deps: SaveDeps,
  draft: Partial<ProfileDraft>,
  seenVersion?: number,
  createWith: Partial<ProfileDraft> = {}
): Promise<ProfileSave> {
  const now = await deps.current()
  if (now.state === "failed") return now
  if (now.state === "present" && seenVersion !== 0) {
    const edited = await deps.edit(draft, seenVersion ?? now.value.version)
    return edited.kept
      ? { state: "saved" }
      : { state: "failed", words: edited.words }
  }
  const full = { ...createWith, ...draft }
  let theirs: Profile
  if (now.state === "present") theirs = now.value
  else {
    const created = await createProfile(deps.client, full)
    if (created.state === "failed") return created
    if (created.state === "created") {
      await deps.laid()
      return { state: "saved" }
    }
    theirs = created.theirs
    await deps.laid()
  }
  const { differs, adds } = compareWithTheirs(full, theirs)
  if (differs.length) return { state: "conflict", theirs }
  if (Object.keys(adds).length) {
    const edited = await deps.edit(adds, theirs.version)
    if (!edited.kept) return { state: "failed", words: edited.words }
  }
  return { state: "saved" }
}

/** What a change needs: `saveProfile`'s, and the outbox's change. */
export interface ChangeDeps extends Omit<SaveDeps, "edit"> {
  change(
    draft: Partial<ProfileDraft>
  ): Promise<{ kept: true } | { kept: false; words: string }>
}

/** A change no form holds: the outbox's change, re-read and sent again on
 * top; where there is no profile, a create first (with `createWith`), and
 * where that meets one another device made, the change on top of it.
 * Resolves with the words where it was not kept. */
export async function changeProfile(
  deps: ChangeDeps,
  draft: Partial<ProfileDraft>,
  createWith: Partial<ProfileDraft> = {}
): Promise<string | undefined> {
  const now = await deps.current()
  if (now.state === "failed") return now.words
  if (now.state === "absent") {
    const created = await createProfile(deps.client, {
      ...createWith,
      ...draft,
    })
    if (created.state === "failed") return created.words
    await deps.laid()
    if (created.state === "created") return undefined
  }
  const changed = await deps.change(draft)
  return changed.kept ? undefined : changed.words
}
