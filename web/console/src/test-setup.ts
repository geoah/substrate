/** What jsdom does not implement and the components under test need.
 *
 * NOT a place for shared fixtures or helpers: each suite builds its own, so a
 * test reads as one file. Only the browser APIs jsdom leaves out belong here,
 * and each one says which component asks for it. */

/** cmdk measures its list to keep the selected row in view (`Command`, and so
 * every combobox built on it). jsdom has no layout, so the observer never has
 * anything to report; it only has to exist. */
if (!("ResizeObserver" in globalThis)) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

/** cmdk scrolls the selected row into view on every keyboard move. */
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {}
}
