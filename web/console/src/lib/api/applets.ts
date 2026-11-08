/** Applets (docs/plans/applets.md): records of the seeded
 * `substrate.reamde.dev/applets/applet` kind, whose screens the server renders as
 * A2UI v1.0 messages. The console lists them through the records route and
 * runs one through its two routes, `render` and `act`, each answering an
 * `AppletFrame`. The server keeps no session: every call carries the state the
 * renderer holds. */

import { APPLETS_PACKAGE_NAME, CORE_AUTHORITY } from "@/lib/core-kinds"

import { collectionPath, request, seg } from "./http"
import { recordsQueryOptions } from "./records"
import type { AppletActInput, AppletFrame, AppletRenderInput } from "./types"

/** How many applets the list reads. */
const APPS_WINDOW = 200

/** Every applet the repository holds, newest first. */
export function appletsQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: APPLETS_PACKAGE_NAME,
    name: "applet",
    first: APPS_WINDOW,
    orderBy: "createdAt:desc",
  })
}

function appletPath(id: string, verb: "render" | "act"): string {
  return `${collectionPath(CORE_AUTHORITY, APPLETS_PACKAGE_NAME, "applet")}/${seg(id)}/${verb}`
}

/** Renders the applet's current screen; no state is its start. */
export function renderApplet(
  id: string,
  input: AppletRenderInput,
  signal?: AbortSignal
): Promise<AppletFrame> {
  return request<AppletFrame>("POST", appletPath(id, "render"), input, {
    signal,
  })
}

/** Runs one of the applet's actions. A refused write answers the error
 * envelope (422 with problems), and the renderer keeps what it holds. */
export function actApplet(
  id: string,
  input: AppletActInput
): Promise<AppletFrame> {
  return request<AppletFrame>("POST", appletPath(id, "act"), input)
}
