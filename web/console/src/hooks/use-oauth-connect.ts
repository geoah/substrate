/** The connect half of a provider account, as a hook the Connections page,
 * its detail and the Add account dialog share: open the consent tab
 * synchronously from the click (a tab opened after the round-trip is what
 * popup blockers kill), mint the URL through `oauth/start`, navigate the tab,
 * then listen for the callback page's postMessage and re-read the account
 * when it lands.
 *
 * The account is named by its RECORD PATH, `<authority>/<package>/<kind>/<id>`:
 * `oauth/start` refuses a bare id (decision record 0102), and the callback's
 * message names the connected account the same way. It is named at hook time
 * (a row that already exists) OR at mutate time (a record the dialog has just
 * created), and a caller whose connect follows an async step hands in the tab
 * it opened from its own click, so the create-then-connect press is still one
 * synchronous open. */

import { useEffect, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import { toast } from "@/components/ui/toast"
import { parseSubstrateOAuthMessage, startOAuth } from "@/lib/api/bundles"

export interface ConnectTarget {
  /** The account's record path, `recordPath(kind, id)`. */
  record: string
  /** What the toasts call it. */
  label: string
  /** A tab the caller opened synchronously from its click. `null` is a tab
   * the browser refused (window.open's own answer), and reads as blocked. */
  tab?: Window | null
}

export function useOAuthConnect(record?: string, label?: string) {
  const queryClient = useQueryClient()
  // The flow whose return the listener below awaits; unset while none is.
  const [awaiting, setAwaiting] = useState<ConnectTarget | undefined>()

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
    void queryClient.invalidateQueries({ queryKey: ["sync"] })
    void queryClient.invalidateQueries({ queryKey: ["record"] })
  }

  const connect = useMutation({
    mutationFn: async (target: ConnectTarget | void) => {
      const chosen = (target ?? undefined) as ConnectTarget | undefined
      const path = chosen?.record ?? record
      if (!path) throw new Error("There is no account to connect.")
      const name = chosen?.label ?? label ?? path
      const tab =
        chosen?.tab === undefined
          ? window.open("about:blank", "_blank")
          : chosen.tab
      try {
        const { url } = await startOAuth(path)
        let target: URL
        try {
          target = new URL(url)
        } catch {
          throw new Error(
            "The provider returned an address the console cannot open."
          )
        }
        if (target.protocol !== "https:") {
          throw new Error(
            "The provider returned an address that is not HTTPS, so the console will not open it."
          )
        }
        if (tab) tab.location.href = url
        return { opened: Boolean(tab), record: path, label: name }
      } catch (error) {
        tab?.close()
        throw error
      }
    },
    onSuccess: ({ opened, record, label }) => {
      if (opened) {
        setAwaiting({ record, label })
        toast.add({
          type: "success",
          title: "The provider opened in a new tab",
          description: "Approve there. This page updates when you come back.",
        })
      } else {
        toast.add({
          type: "error",
          title: "Your browser blocked the new tab",
          description: "Allow pop-ups for this site, then press Connect again.",
        })
      }
      refresh()
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Connecting failed",
        description: error.message,
      }),
  })

  useEffect(() => {
    if (!awaiting) return
    const { record, label } = awaiting
    function onMessage(event: MessageEvent) {
      // Origin first: the callback page is served by the substrate that
      // serves this console, so anything else is a stranger's window.
      if (event.origin !== window.location.origin) return
      const msg = parseSubstrateOAuthMessage(event.data)
      if (!msg) return
      if (msg.ok) {
        // A success names its record path; a return meant for another row is
        // not this flow's.
        if (msg.record !== record) return
        setAwaiting(undefined)
        toast.add({
          type: "success",
          title: "Account connected",
          description: `${label} is approved and its first sync is starting.`,
        })
        refresh()
      } else {
        setAwaiting(undefined)
        toast.add({
          type: "error",
          title: "Connecting failed",
          description: msg.correlation
            ? `The provider refused. Reference ${msg.correlation}.`
            : "The provider refused.",
        })
      }
    }
    window.addEventListener("message", onMessage)
    return () => window.removeEventListener("message", onMessage)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [awaiting])

  return connect
}

export type OAuthConnect = ReturnType<typeof useOAuthConnect>
