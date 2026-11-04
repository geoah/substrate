/** The operator's landing fragment, prepared for the console's page.
 *
 * The fragment is the deployment's own file, served by the same server as the
 * console, but it is still mounted as markup and nothing more: what a template
 * parses never runs, and the elements and attributes that would run code or
 * load another document once mounted are removed here. */

const DROPPED = "script, iframe, frame, object, embed, base, meta, link, form"

const URL_ATTRS = ["href", "src", "xlink:href", "action", "formaction"]

/** Parse `html` into an inert fragment with no scripts, no event handlers and
 * no `javascript:` URLs. */
export function landingFragment(
  html: string,
  doc: Document = document
): DocumentFragment {
  const template = doc.createElement("template")
  template.innerHTML = html
  const content = template.content
  for (const el of content.querySelectorAll(DROPPED)) el.remove()
  for (const el of content.querySelectorAll("*")) {
    for (const attr of [...el.attributes]) {
      const name = attr.name.toLowerCase()
      if (name.startsWith("on")) {
        el.removeAttribute(attr.name)
        continue
      }
      if (
        URL_ATTRS.includes(name) &&
        /^\s*(javascript|vbscript):/i.test(attr.value)
      ) {
        el.removeAttribute(attr.name)
      }
    }
  }
  return content
}

/** The console path a click on the landing should open inside the app, or
 * null for a link the browser follows itself. Only the doors are taken over:
 * every other link is the operator's to point anywhere. */
export function doorPath(href: string, origin: string): string | null {
  let url: URL
  try {
    url = new URL(href, origin)
  } catch {
    return null
  }
  if (url.origin !== origin) return null
  if (url.pathname !== "/login" && url.pathname !== "/register") return null
  return url.pathname + url.search
}
