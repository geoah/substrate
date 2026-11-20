// Every importer the import screen offers, each loaded with the screen
// that needs it, so a provider's parsers (and a PDF reader) load only
// when they are used.

import type { BuiltinImporterId, Importer } from "./types"

export type { BuiltinImporterId, Importer, ImporterId } from "./types"

export const IMPORTERS: Readonly<
  Record<Exclude<BuiltinImporterId, "csv" | "manual">, () => Promise<Importer>>
> = {
  numan: () => import("./numan").then((m) => m.numan),
  randox: () => import("./randox").then((m) => m.randox),
  dxa: () => import("./dxa").then((m) => m.dxa),
  nhs: () => import("./nhs").then((m) => m.nhs),
  mychart: () => import("./mychart").then((m) => m.mychart),
}
