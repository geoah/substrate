// The owner's details and the profile record (docs/plans/healthspan.md,
// The owner's details; The package, profile). The contract every domain
// reads the owner through, live (profile/live.ts) and demo
// (demo/profile.ts).

import { demoProfileData } from "../demo/profile"
import type { Profile, ProfileDraft, Sex } from "../kinds"
import {
  useLiveConsoleLink,
  useLiveGate,
  useLiveOwner,
  useLiveProfileWrites,
} from "../profile/live"
import type { ContactDay } from "../profile/owner"
import type { Read } from "./read"

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
  contact?: {
    id: string
    name?: string
    /** The contact's birthday as saved, the year absent for one saved
     * without. */
    birthday?: ContactDay
  }
  birthDate?: Resolved<string>
  /** Only ever the profile's: the reference basis is the owner's choice. */
  sex?: Resolved<Sex>
  /** What a patient record about the owner says, offered on getting
   * started and in Settings, never used until saved to the profile. */
  suggestions: { birthDate?: string; sex?: Sex }
  /** The patient registration the suggestions come from. */
  suggestedBy?: { id: string; service?: string }
  /** The profile record, where one exists. */
  profile?: Profile
}

/** How a save of the profile ended. */
export type ProfileSave =
  | { state: "saved" }
  /** Another device saved the profile first with other values: both are
   * shown and the owner picks, then `save` again on `theirs.version`. */
  | { state: "conflict"; theirs: Profile }
  | { state: "failed"; words: string }

export interface ProfileWrites {
  /** A form's save: creates the profile where absent (a PUT at
   * `ifVersion: 0` through the client, compared on 409), else an edit on
   * the version shown. `seenVersion` is the version the form showed, 0
   * where it showed none (a profile made since is then compared, not
   * edited); left out, the version shown now. Waits until the profile's
   * read has answered. */
  save: (
    draft: Partial<ProfileDraft>,
    opts?: { seenVersion?: number }
  ) => Promise<ProfileSave>
  /** A change no form holds (the night and workout orders, recordSeenAt):
   * a patch re-read and sent again on top. Creates the profile first where
   * there is none. A change not kept is said in a toast. */
  change: (draft: Partial<ProfileDraft>) => Promise<void>
}

export interface ProfileData {
  /** Whether getting started should open: known once the profile's read
   * has answered, needed while no profile records a start. */
  useGate: () => { ready: boolean; needsStart: boolean }
  useOwner: () => Read<Owner>
  useProfileWrites: () => ProfileWrites
  /** A page of the owner's console (`/settings/name`), at the server the
   * session is with; none in the demo, which has no console. */
  useConsoleLink: () => ((path: string) => string) | undefined
}

export const liveProfile: ProfileData = {
  useGate: useLiveGate,
  useOwner: useLiveOwner,
  useProfileWrites: useLiveProfileWrites,
  useConsoleLink: useLiveConsoleLink,
}

export const demoProfile: ProfileData = demoProfileData
