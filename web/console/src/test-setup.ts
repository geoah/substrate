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

/** base-ui's popover positioning measures text ranges through the DOM Range
 * API. jsdom implements Range but not its geometry, so both readers return
 * empty boxes; positioning needs them only to exist. */
if (!Range.prototype.getClientRects) {
  Range.prototype.getClientRects = function getClientRects() {
    return {
      length: 0,
      item: () => null,
      [Symbol.iterator]: [][Symbol.iterator],
    } as unknown as DOMRectList
  }
}
if (!Range.prototype.getBoundingClientRect) {
  Range.prototype.getBoundingClientRect = function getBoundingClientRect() {
    return new DOMRect(0, 0, 0, 0)
  }
}

/** base-ui's scroll area asks the viewport for its running animations before
 * hiding a scrollbar. jsdom animates nothing, so the honest answer is none. */
if (!Element.prototype.getAnimations) {
  Element.prototype.getAnimations = function getAnimations() {
    return []
  }
}
