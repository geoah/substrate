/** The connect half of a provider account, as a hook the Connections page and
 * its detail share: open the consent tab synchronously from the click (a
 * tab opened after the round-trip is what popup blockers kill), mint the
 * URL through `oauth/start`, navigate the tab, then listen for the callback
 * page's postMessage and re-read the account when it lands. */

import { useEffect, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import { toast } from "@/components/ui/toast"
import { parseSubstrateOAuthMessage, startOAuth } from "@/lib/api/bundles"

export function useOAuthConnect(accountId: string, label: string) {
  const queryClient = useQueryClient()
  const [awaitingReturn, setAwaitingReturn] = useState(false)

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
    void queryClient.invalidateQueries({ queryKey: ["sync"] })
    void queryClient.invalidateQueries({ queryKey: ["record"] })
  }

  const connect = useMutation({
    mutationFn: async () => {
      const tab = window.open("about:blank", "_blank")
      try {
        const { url } = await startOAuth(accountId)
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
        return { opened: Boolean(tab) }
      } catch (error) {
        tab?.close()
        throw error
      }
    },
    onSuccess: ({ opened }) => {
      if (opened) {
        setAwaitingReturn(true)
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
    if (!awaitingReturn) return
    function onMessage(event: MessageEvent) {
      // Origin first: the callback page is served by the substrate that
      // serves this console, so anything else is a stranger's window.
      if (event.origin !== window.location.origin) return
      const msg = parseSubstrateOAuthMessage(event.data)
      if (!msg) return
      if (msg.ok) {
        if (msg.record && msg.record !== accountId) return
        setAwaitingReturn(false)
        toast.add({
          type: "success",
          title: "Account connected",
          description: label,
        })
        refresh()
      } else {
        setAwaitingReturn(false)
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
  }, [awaitingReturn, accountId, label])

  return connect
}
