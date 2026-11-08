// Names the file each relative import in a package's emitted declarations
// points at, run from the package's folder after tsc (`pnpm build`). The
// sources import each other without extensions, which a bundler and
// TypeScript's "bundler" resolution accept, and tsc keeps them so; under
// "node16" or "nodenext" an ES module's relative import must name its file,
// so `./client` becomes `./client.js`, which resolves to client.d.ts.
import { readdirSync, readFileSync, writeFileSync } from "node:fs"
import { join } from "node:path"

const dist = join(process.cwd(), "dist")
const relative = /((?:from|import\()\s*["'])(\.\.?\/[^"'\n]+)(["'])/g

for (const file of readdirSync(dist)) {
  if (!file.endsWith(".d.ts")) continue
  const path = join(dist, file)
  const text = readFileSync(path, "utf8")
  const named = text.replace(relative, (whole, head, spec, tail) =>
    /\.[cm]?js$/.test(spec) ? whole : `${head}${spec}.js${tail}`
  )
  if (named !== text) writeFileSync(path, named)
}
