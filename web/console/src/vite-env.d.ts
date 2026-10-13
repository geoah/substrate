/** The build-time settings the console reads from `import.meta.env`. Vite
 * types every other key as `any`, so a setting read without a line here is
 * unchecked wherever it flows. */
interface ImportMetaEnv {
  /** The substrate's own origin, baked in at build time (lib/api/catalog.ts). */
  readonly VITE_SUBSTRATE_ORIGIN?: string
}
