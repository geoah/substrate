/** The kit's stylesheet, carried inside the chunk: the guest's `connect-src`
 * is `'none'` and its `style-src` admits inline text only, so a `<link>` could
 * never load it. Appended to `<head>` once per document, after the shell has
 * written the guest's own document. */

import css from "./kit.css?inline"

const STYLE_ID = "substrate-kit"

export function ensureKitStyles(): void {
  if (typeof document === "undefined" || document.getElementById(STYLE_ID)) {
    return
  }
  const style = document.createElement("style")
  style.id = STYLE_ID
  style.textContent = css
  document.head.appendChild(style)
}
