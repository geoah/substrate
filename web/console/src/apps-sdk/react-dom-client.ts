/** `react-dom/client` inside the guest: the shell's bootstrap renders the
 * entry with this `createRoot`, the console's own. Named, for the reason
 * `react.ts` gives; `version` is left out, as its types leave it out. */
export { createRoot, hydrateRoot } from "react-dom/client"
