/** The two core collections a bundle ships its configuration in: `setting`
 * and `secret` (decision record 0076). They are ordinary records of ordinary
 * core kinds, so every read and write here rides the record API; nothing new
 * is addressed.
 *
 * A record's id is `<bundle id>/<name>`, and the bundle id is
 * `<authority>/<package>`, so ownership is the id prefix and the console never
 * asks the server which bundle a setting belongs to. */

import { queryOptions } from "@tanstack/react-query"

import {
  CORE_AUTHORITY,
  CORE_PACKAGE,
  CORE_PACKAGE_NAME,
  request,
} from "./http"
import { listPath, patchRecord } from "./records"
import type { Page, SubstrateRecord } from "./types"

export const SETTING_KIND = `${CORE_PACKAGE}/setting`
export const SECRET_KIND = `${CORE_PACKAGE}/secret`

/** The core kind names, as a collection path spells them. */
export const SETTING_NAME = "setting"
export const SECRET_NAME = "secret"

/** The bounded read behind the settings surfaces. A repository's settings are
 * a handful per bundle, so one page of each collection is the whole set; a
 * repository that outgrows the cap is a paging problem nobody has yet. */
export const SETTINGS_CAP = 500

/** Both kinds in ONE read: the records route lists several kinds at once
 * (`filter.kinds`), so the settings surface is one request, not two joined. */
async function fetchBoth(signal?: AbortSignal): Promise<SubstrateRecord[]> {
  const page = await request<Page>(
    "GET",
    listPath({ kinds: [SETTING_KIND, SECRET_KIND], first: SETTINGS_CAP }),
    undefined,
    { signal }
  )
  return page.records ?? []
}

/** Every `setting` and `secret` record in the repository, one list. The kind
 * on each record is what tells the two apart. */
export const settingRecordsQueryOptions = queryOptions({
  queryKey: ["settings", "records"],
  queryFn: ({ signal }) => fetchBoth(signal),
  staleTime: 30_000,
})

/** Write one setting's value. A PATCH, never a PUT: the bundle owns every
 * other property on the record (its label, its description, its type) and a
 * full-document write from this form would drop them. A secret left blank is
 * the caller's to skip — sending one would seal an empty string over the
 * stored value. */
export function saveSettingValue(
  record: SubstrateRecord,
  value: string
): Promise<SubstrateRecord> {
  const name = record.kind === SECRET_KIND ? SECRET_NAME : SETTING_NAME
  return patchRecord(CORE_AUTHORITY, CORE_PACKAGE_NAME, name, record.id, {
    properties: { value },
  })
}
