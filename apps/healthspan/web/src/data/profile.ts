// The owner's details and the profile record (docs/plans/healthspan.md,
// The owner's details; The package, profile). The contract every domain
// reads the owner through; the profile workstream implements it, live and
// demo. Until then: the gate alone, and nothing known.

import { useRecord } from "@substrate/react"
import { PROFILE_ID, Profile, type ProfileDraft, type Sex } from "../kinds"
import { pending, settled, type Read } from "./read"

/** Where a value came from: the owner's contact, the profile this app
 * keeps, or a patient record (offered, and kept only once confirmed, so a
 * patient value is never an origin of a value in use). */
export type Origin = "contact" | "profile"

export interface Resolved<T> {
  value: T
  origin: Origin
}

/** The owner as every screen reads them. */
export interface Owner {
  /** The owner's own contact, where the setting names a live one. */
  contact?: { id: string; name?: string }
  birthDate?: Resolved<string>
  /** Only ever the profile's: the reference basis is the owner's choice. */
  sex?: Resolved<Sex>
  /** What a patient record about the owner says, offered on getting
   * started and in Settings, never used until saved to the profile. */
  suggestions: { birthDate?: string; sex?: Sex }
  /** The profile record, where one exists. */
  profile?: Profile
}

/** How a save of the profile ended. */
export type ProfileSave =
  | { state: "saved" }
  /** Another device saved the profile first with other values: both are
   * shown and the owner picks, then `save` again. */
  | { state: "conflict"; theirs: Profile }
  | { state: "failed"; words: string }

export interface ProfileWrites {
  /** A form's save: creates the profile where absent (a PUT at
   * `ifVersion: 0` through the client, compared on 409), else an edit on
   * the version shown. */
  save(draft: Partial<ProfileDraft>): Promise<ProfileSave>
  /** A change no form holds (the night and workout orders, recordSeenAt):
   * a patch re-read and sent again on top. Creates the profile first where
   * there is none. */
  change(draft: Partial<ProfileDraft>): Promise<void>
}

export interface ProfileData {
  /** Whether getting started should open: known once the profile's read
   * has answered, needed while no profile records a start. */
  useGate(): { ready: boolean; needsStart: boolean }
  useOwner(): Read<Owner>
  useProfileWrites(): ProfileWrites
}

const noWrites: ProfileWrites = {
  save: () => Promise.resolve({ state: "failed", words: "Not built yet." }),
  change: () => Promise.resolve(),
}

export const liveProfile: ProfileData = {
  useGate() {
    const read = useRecord(Profile, PROFILE_ID)
    if (read.state === "loading") return { ready: false, needsStart: false }
    return {
      ready: true,
      needsStart: read.state !== "present" || !read.value.startedAt,
    }
  },
  useOwner: () => pending(),
  useProfileWrites: () => noWrites,
}

export const demoProfile: ProfileData = {
  useGate: () => ({ ready: true, needsStart: false }),
  useOwner: () => settled<Owner>({ suggestions: {} }),
  useProfileWrites: () => noWrites,
}
