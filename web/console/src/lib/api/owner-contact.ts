/** The owner's own contact (decision record 0231): an ordinary CONTACT_KIND
 * record under a server-minted id, named by the core setting
 * OWNER_CONTACT_SETTING_ID, whose `value` is that id. Nothing seeds either
 * record; the console writes both once the owner gives their name. The
 * setting is read and written the way the spend cap is (settings.ts). */

import { queryOptions } from "@tanstack/react-query"

import { CORE_AUTHORITY, CORE_PACKAGE_NAME, splitKind } from "./http"
import { createRecord, fetchRecord, patchRecord, putRecord } from "./records"
import { SETTING_KIND, SETTING_NAME } from "./settings"
import { ApiError, type SubstrateRecord } from "./types"
import { CONTACT_KIND, OWNER_CONTACT_SETTING_ID } from "@/lib/core-kinds"

/** The contact kind's `substrate` pattern, as kinds/…/core/contact.yaml
 * declares it: a repository name outside it (a single label) is refused on
 * write, so the contact is created without one. */
const CONTACT_SUBSTRATE =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$/

/** The setting and the contact it names, as one read. */
export interface OwnerContact {
  /** The live setting, or null where nothing wrote it. */
  setting: SubstrateRecord | null
  /** The live contact the setting's `value` names, or null where it names
   * none: no setting, an empty value, or a contact since deleted. */
  contact: SubstrateRecord | null
}

/** A read that answers null for a record that is not there or is a
 * tombstone: a single-record read returns a deleted record too. */
async function liveOrNull(
  read: () => Promise<SubstrateRecord>
): Promise<SubstrateRecord | null> {
  try {
    const record = await read()
    return record.deletedAt ? null : record
  } catch (error) {
    if (error instanceof ApiError && error.code === "not_found") return null
    throw error
  }
}

/** The setting's value, the id of the owner's contact; "" where unset. */
export function ownerContactId(setting: SubstrateRecord | null): string {
  const value = setting?.properties.value
  return typeof value === "string" ? value.trim() : ""
}

/** The owner's contact and the setting naming it. The key names both kinds,
 * so a write to any contact or setting (recordWriteReaches) reads it again. */
export const ownerContactQueryOptions = queryOptions({
  queryKey: ["owner-contact", CONTACT_KIND, SETTING_KIND],
  queryFn: async ({ signal }): Promise<OwnerContact> => {
    const setting = await liveOrNull(() =>
      fetchRecord(
        CORE_AUTHORITY,
        CORE_PACKAGE_NAME,
        SETTING_NAME,
        OWNER_CONTACT_SETTING_ID,
        signal
      )
    )
    const id = ownerContactId(setting)
    if (!id) return { setting, contact: null }
    const { authority, pkg, name } = splitKind(CONTACT_KIND)
    // A former id reads as the merge winner, so the setting still reaches
    // the owner's card after it is merged into another.
    const contact = await liveOrNull(() =>
      fetchRecord(authority, pkg, name, id, signal)
    )
    return { setting, contact }
  },
})

/** The properties of the owner's new contact: a person they know, and
 * their own repository's name where the kind's pattern admits it. */
export function ownerContactProperties(
  name: string,
  repository: string
): Record<string, unknown> {
  return {
    name: name.trim(),
    contactType: "person",
    prominence: "known",
    ...(CONTACT_SUBSTRATE.test(repository) ? { substrate: repository } : {}),
  }
}

/** Create the owner's contact under a server-minted id. */
export function createOwnerContact(
  name: string,
  repository: string
): Promise<SubstrateRecord> {
  const { authority, pkg, name: kindName } = splitKind(CONTACT_KIND)
  return createRecord(authority, pkg, kindName, {
    properties: ownerContactProperties(name, repository),
  })
}

/** Write a new name onto a contact that is already the owner's. */
export function renameOwnerContact(
  id: string,
  name: string
): Promise<SubstrateRecord> {
  const { authority, pkg, name: kindName } = splitKind(CONTACT_KIND)
  return patchRecord(authority, pkg, kindName, id, {
    properties: { name: name.trim() },
  })
}

/** Point the setting at the owner's contact. A PUT, because the record may
 * not exist yet; it merges, so a description written by hand stays: the
 * name and the description are written only by the PUT that creates it. */
export function saveOwnerContactSetting({
  contactId,
  exists,
}: {
  contactId: string
  exists: boolean
}): Promise<SubstrateRecord> {
  return putRecord(
    CORE_AUTHORITY,
    CORE_PACKAGE_NAME,
    SETTING_NAME,
    OWNER_CONTACT_SETTING_ID,
    {
      properties: {
        ...(exists
          ? {}
          : {
              displayName: "Your own contact",
              description:
                "The id of the contact that is you. Agents and apps read it to know which contact is the owner.",
            }),
        value: contactId,
      },
    }
  )
}
