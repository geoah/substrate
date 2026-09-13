/** TSX to JavaScript, in the guest, per mount. Sucrase is a parser and a
 * printer with no evaluator, small enough to lazy-load and LINE-PRESERVING,
 * so a runtime error's line is the author's line with no source map. It is
 * not a checker: a type error is not reported. Imports are kept as written
 * (`disableESTransforms`), so the import map resolves them; an import used
 * only as a type is elided, as `tsc` elides it. */

import { transform } from "sucrase"

import { SOURCE_MODULE } from "@/lib/apps/spec"

export interface TransformProblem {
  module: string
  message: string
  line?: number
  column?: number
}

export type Transformed = { js: string } | { error: TransformProblem }

export function transformModule(name: string, text: string): Transformed {
  try {
    const { code } = transform(text, {
      transforms: ["typescript", "jsx"],
      jsxRuntime: "automatic",
      jsxImportSource: "react",
      production: true,
      disableESTransforms: true,
      filePath: name === SOURCE_MODULE ? `${name}.tsx` : `#${name}.tsx`,
    })
    return { js: code }
  } catch (e) {
    const err = e as Error & { loc?: { line: number; column: number } }
    return {
      error: {
        module: name,
        // Sucrase writes the file and the position into the message too;
        // the fields carry both, so the text says only what went wrong.
        message: err.message
          .replace(/^Error transforming [^:]+: /, "")
          .replace(/\s*\(\d+:\d+\)\s*$/, ""),
        line: err.loc?.line,
        column: err.loc?.column,
      },
    }
  }
}
