import { defineKind, stringProp } from "@substrate/client"

/** An item as the screens read it. */
export interface Item {
  id: string
  /** The version the owner sees: what an edit holds to. */
  version: number
  name: string
}

/** Every kind the app reads or writes, declared once. The package is
 * published under `substrate.reamde.dev`, so the names are the same in
 * every repository (../package/README.md). */
export const Item = defineKind("substrate.reamde.dev/template/item", {
  read: (r): Item => ({
    id: r.id,
    version: r.version,
    name: stringProp(r, "name") ?? "",
  }),
  write: (draft: { name: string }) => ({ name: draft.name.trim() }),
})

/** An item's name as a screen shows it. */
export function itemTitle(item: Item): string {
  return item.name || "Untitled item"
}
