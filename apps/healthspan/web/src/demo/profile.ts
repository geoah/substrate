// The demo member as the owner (`?demo`): their contact, their profile and
// their patient registration, as records run through the live reads'
// transforms. Writes change this page's memory alone. `?start` opens on
// getting started with no profile yet; `?profile=none` knows nothing of the
// owner (no contact, no profile, no registration).

import type { SubstrateRecord } from "@substrate/react"
import { useMemo, useSyncExternalStore } from "react"
import type { Owner, ProfileData, ProfileWrites } from "../data/profile"
import { useMode } from "../data/mode"
import { settled, type Read } from "../data/read"
import { KIND, PROFILE_ID, Profile, type ProfileDraft } from "../kinds"
import { contactOf, patientOf, resolveOwner } from "../profile/owner"
import { PROFILE_NAME } from "../profile/save"
import { demoPatientRecords } from "./record"
import { DEMO_PERSON, rec, ref } from "./shared"

interface DemoState {
  contact: SubstrateRecord | null
  profile: SubstrateRecord | null
  patients: SubstrateRecord[]
}

const DAY = 86_400_000

function initial(params: URLSearchParams, today: Date): DemoState {
  const none = params.get("profile") === "none"
  const [year, month, day] = DEMO_PERSON.birthDate.split("-").map(Number)
  const contact = none
    ? null
    : rec(KIND.contact, DEMO_PERSON.contactId, {
        name: DEMO_PERSON.name,
        contactType: "person",
        dates: { birthday: { year, month, day } },
      })
  const profile =
    none || params.has("start")
      ? null
      : rec(KIND.profile, PROFILE_ID, {
          name: PROFILE_NAME,
          contact: ref(KIND.contact, DEMO_PERSON.contactId),
          sex: DEMO_PERSON.sex,
          startedAt: new Date(today.getTime() - 30 * DAY).toISOString(),
        })
  return {
    contact,
    profile,
    patients: demoPatientRecords({ profile: params.get("profile") }),
  }
}

let state: DemoState | undefined
const listeners = new Set<() => void>()

function stateFor(params: URLSearchParams, today: Date): DemoState {
  state ??= initial(params, today)
  return state
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/** Forgets the demo's writes: the next reader starts from the query. */
export function resetDemoProfile(): void {
  state = undefined
  for (const fn of listeners) fn()
}

/** A write onto the demo's profile, as a patch merges: null clears. */
export function writeDemoProfile(draft: Partial<ProfileDraft>): void {
  if (!state) return
  const here = state.profile
  const props: Record<string, unknown> = here
    ? { ...here.properties }
    : {
        name: PROFILE_NAME,
        ...(state.contact
          ? { contact: ref(KIND.contact, state.contact.id) }
          : {}),
      }
  for (const [k, v] of Object.entries(Profile.write(draft))) {
    if (v === null) delete props[k]
    else props[k] = v
  }
  const now = new Date().toISOString()
  state = {
    ...state,
    profile: {
      ...rec(KIND.profile, PROFILE_ID, props),
      version: (here?.version ?? 0) + 1,
      createdAt: here?.createdAt ?? now,
      updatedAt: now,
    },
  }
  for (const fn of listeners) fn()
}

function useDemoState(): DemoState {
  const { params, today } = useMode()
  return useSyncExternalStore(subscribe, () => stateFor(params, today))
}

/** The demo's profile, read through its codec. */
export function useDemoProfileRecord(): Profile | undefined {
  const s = useDemoState()
  return useMemo(() => (s.profile ? Profile.read(s.profile) : undefined), [s])
}

const writes: ProfileWrites = {
  save: (draft) => {
    writeDemoProfile(draft)
    return Promise.resolve({ state: "saved" })
  },
  change: (draft) => {
    writeDemoProfile(draft)
    return Promise.resolve()
  },
}

export const demoProfileData: ProfileData = {
  useGate() {
    const { params } = useMode()
    const profile = useDemoProfileRecord()
    return {
      ready: true,
      needsStart: params.has("start") && !profile?.startedAt,
    }
  },
  useOwner(): Read<Owner> {
    const s = useDemoState()
    return useMemo(
      () =>
        settled(
          resolveOwner({
            contact: s.contact ? contactOf(s.contact) : undefined,
            profile: s.profile ? Profile.read(s.profile) : undefined,
            patients: s.patients.map(patientOf),
          })
        ),
      [s]
    )
  },
  useProfileWrites: () => writes,
  useConsoleLink: () => undefined,
}
