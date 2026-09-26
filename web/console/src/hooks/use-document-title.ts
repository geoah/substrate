import { useEffect } from "react"

/** Names a page outside the shell (the sign-in door) in the browser tab; the
 * shell names every page inside it from its crumbs. */
export function useDocumentTitle(title: string): void {
  useEffect(() => {
    document.title = title
  }, [title])
}
