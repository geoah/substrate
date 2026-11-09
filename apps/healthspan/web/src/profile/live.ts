// The owner and the profile on the owner's substrate: the setting naming
// their contact and the contact (read with the client, never written: no
// scope writes `core`), the profile the kit links, and the patient
// registrations, which the repository holds only where a provider
// installed `clinical`.

import { SubstrateError, type SubstrateClient } from "@substrate/client"
import {
  useInvalidate,
  useKindWrites,
  useRead,
  useRecord,
  useSubstrate,
  useToast,
} from "@substrate/react"
import { useCallback, useMemo } from "react"
import type { Owner, ProfileWrites } from "../data/profile"
import type { Read } from "../data/read"
import {
  KIND,
  OWNER_CONTACT_SETTING,
  PROFILE_ID,
  Profile,
  type ProfileDraft,
} from "../kinds"
import {
  contactOf,
  ownerContactId,
  patientOf,
  resolveOwner,
  type OwnerContact,
  type PatientRegistration,
} from "./owner"
import { changeProfile, saveProfile, type Current } from "./save"

export function useLiveGate(): { ready: boolean; needsStart: boolean } {
  const read = useRecord(Profile, PROFILE_ID)
  if (read.state === "loading") return { ready: false, needsStart: false }
  // A read that failed opens the app as it is rather than trapping the
  // owner on getting started; it is read again by itself.
  if (read.state === "failed") return { ready: true, needsStart: false }
  return {
    ready: true,
    needsStart: read.state !== "present" || !read.value.startedAt,
  }
}

/** The owner's contact, or null where the setting names none (unset,
 * empty, or a contact since deleted). A tombstone reads as absent. */
async function readOwnerContact(
  client: SubstrateClient,
  signal: AbortSignal
): Promise<OwnerContact | null> {
  const setting = await client.record(
    KIND.setting,
    OWNER_CONTACT_SETTING,
    signal
  )
  const id = ownerContactId(setting)
  if (!id) return null
  const contact = await client.record(KIND.contact, id, signal)
  return contact ? contactOf(contact) : null
}

/** A refusal of the request itself (the kind not installed, a scope the
 * approval lacks), which reads as none; a signed-out or busy server is not
 * one. */
function refused(e: unknown): boolean {
  return (
    e instanceof SubstrateError &&
    e.status >= 400 &&
    e.status < 500 &&
    e.status !== 401 &&
    e.status !== 429
  )
}

async function readPatients(
  client: SubstrateClient,
  signal: AbortSignal
): Promise<PatientRegistration[]> {
  try {
    const page = await client.listAll({ kinds: [KIND.patient] }, 500, signal)
    return page.records.filter((r) => !r.deletedAt).map(patientOf)
  } catch (e) {
    if (refused(e)) return []
    throw e
  }
}

const CONTACT_KEY = "healthspan/owner-contact"
const PATIENTS_KEY = "healthspan/patients"

function useOwnerContact() {
  return useRead(CONTACT_KEY, readOwnerContact, {
    kinds: [KIND.setting, KIND.contact],
  })
}

export function useLiveOwner(): Read<Owner> {
  const profile = useRecord(Profile, PROFILE_ID)
  const contact = useOwnerContact()
  const patients = useRead(PATIENTS_KEY, readPatients, {
    kinds: [KIND.patient],
  })
  const profileKnown = profile.state !== "loading"
  const contactKnown = contact.data !== undefined || !!contact.error
  const patientsKnown = patients.data !== undefined || !!patients.error
  const data = useMemo(
    () =>
      profileKnown && contactKnown && patientsKnown
        ? resolveOwner({
            contact: contact.data ?? undefined,
            profile: profile.value,
            patients: patients.data ?? [],
          })
        : undefined,
    [
      profileKnown,
      contactKnown,
      patientsKnown,
      contact.data,
      profile.value,
      patients.data,
    ]
  )
  const contactRefresh = contact.refresh
  const patientsRefresh = patients.refresh
  const retryProfile = profile.state === "failed" ? profile.retry : undefined
  const refresh = useCallback(() => {
    contactRefresh()
    patientsRefresh()
    retryProfile?.()
  }, [contactRefresh, patientsRefresh, retryProfile])
  return {
    data,
    // The contact's read failing is said; a patient read failing only
    // leaves no suggestion.
    error:
      contact.error ??
      (profile.state === "failed" ? new Error(profile.words) : undefined),
    loading: !data,
    refreshing: contact.refreshing || patients.refreshing,
    refresh,
  }
}

/** The profile once its read has answered: what a save waits for. */
function currentProfile(
  linked: ReturnType<typeof useSubstrate>["linked"]
): Promise<Current> {
  const now = (): Current | undefined => {
    const r = linked.recordSnapshot(KIND.profile, PROFILE_ID)
    if (r) return { state: "present", value: Profile.read(r) }
    if (linked.isAbsent(KIND.profile, PROFILE_ID)) return { state: "absent" }
    const failed = linked.readError(KIND.profile, PROFILE_ID)
    if (failed) return { state: "failed", words: failed }
    return undefined
  }
  const first = now()
  if (first) return Promise.resolve(first)
  linked.resolve(KIND.profile, PROFILE_ID)
  return new Promise((resolve) => {
    const stop = linked.subscribe(() => {
      const known = now()
      if (!known) return
      stop()
      resolve(known)
    })
  })
}

export function useLiveProfileWrites(): ProfileWrites {
  const { client, linked } = useSubstrate()
  const writes = useKindWrites(Profile)
  const invalidate = useInvalidate()
  const toast = useToast()
  const contact = useOwnerContact()
  const contactId = contact.data?.id
  return useMemo<ProfileWrites>(() => {
    const quiet = { onNotKept: () => {} }
    const kept = (r: { kept: true } | { kept: false; words: string }) =>
      r.kept ? { kept: true as const } : r
    const base = {
      current: () => currentProfile(linked),
      client,
      laid: async () => {
        await linked.refresh(KIND.profile, PROFILE_ID)
        invalidate([KIND.profile])
      },
    }
    const createWith: Partial<ProfileDraft> = contactId
      ? { contact: contactId }
      : {}
    return {
      save: (draft, opts) =>
        saveProfile(
          {
            ...base,
            edit: async (d, version) =>
              kept(await writes.edit(PROFILE_ID, d, version, quiet)),
          },
          draft,
          opts?.seenVersion,
          createWith
        ),
      change: async (draft) => {
        const words = await changeProfile(
          {
            ...base,
            change: async (d) =>
              kept(await writes.change(PROFILE_ID, d, quiet)),
          },
          draft,
          createWith
        )
        if (words) toast(words, "error")
      },
    }
  }, [client, linked, writes, invalidate, toast, contactId])
}

export function useLiveConsoleLink(): (path: string) => string {
  const { session } = useSubstrate()
  const origin = new URL(session.server).origin
  return useCallback((path: string) => `${origin}${path}`, [origin])
}
