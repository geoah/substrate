import { defineKind, stringProp } from "@substrate/react"

/** An item as the screens read it. */
export interface Item {
  id: string
  /** The version the owner sees: what an edit holds to. */
  version: number
  name: string
}

/** What a write of an item names: a create every field, a change or an
 * edit the fields it sets. */
export interface ItemDraft {
  name: string
}

/** The template's one kind, declared once. The package is published under
 * `substrate.reamde.dev`, so the name is the same in every repository
 * (../package/README.md). */
export const Item = defineKind("substrate.reamde.dev/template/item", {
  read: (r): Item => ({
    id: r.id,
    version: r.version,
    name: stringProp(r, "name") ?? "",
  }),
  write: (draft: Partial<ItemDraft>) =>
    draft.name === undefined ? {} : { name: draft.name.trim() },
})

/** An item's name as a screen shows it. */
export function itemTitle(item: Item): string {
  return item.name || "Untitled item"
}
