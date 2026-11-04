import { useEffect } from "react"

import { brandedTitle, onSite } from "@/lib/site"

/** Names a page outside the shell (the sign-in door) in the browser tab; the
 * shell names every page inside it from its crumbs. A site's name follows the
 * page's, and the title is written again if the site arrives after the page. */
export function useDocumentTitle(title: string): void {
  useEffect(() => {
    document.title = brandedTitle(title)
    return onSite(() => {
      document.title = brandedTitle(title)
    })
  }, [title])
}
