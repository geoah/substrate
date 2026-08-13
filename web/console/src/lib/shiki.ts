/** The tinting engine, isolated so the shiki bundle loads lazily: callers
 * import this module with `import()` and nothing else pulls it into the main
 * chunk. The theme is shiki's css-variables theme, so every color rides
 * `--shiki-*` variables defined next to the app tokens in `index.css` — the
 * tint follows light/dark for free (rule 10). */

// The fine-grained core: only the grammars below and the lightweight JS regex
// engine ship — the full `shiki` entry would bundle every language.
import {
  createCssVariablesTheme,
  createHighlighterCore,
  type HighlighterCore,
  type ThemedToken,
} from "shiki/core"
import { createJavaScriptRegexEngine } from "shiki/engine/javascript"
import json from "shiki/langs/json.mjs"
import yaml from "shiki/langs/yaml.mjs"

/** The grammars the console ships: YAML for a manifest, JSON for a tool call's
 * request and response. A language outside this set is a build-time error, not
 * a runtime one. */
export type CodeLang = "yaml" | "json"

let highlighterPromise: Promise<HighlighterCore> | null = null

function highlighter(): Promise<HighlighterCore> {
  return (highlighterPromise ??= createHighlighterCore({
    themes: [
      createCssVariablesTheme({
        name: "css-variables",
        variablePrefix: "--shiki-",
        fontStyle: true,
      }),
    ],
    langs: [yaml, json],
    engine: createJavaScriptRegexEngine(),
  }))
}

export interface CodeToken {
  content: string
  color?: string
  italic: boolean
}

export async function tokenize(
  source: string,
  lang: CodeLang
): Promise<CodeToken[][]> {
  const hl = await highlighter()
  const { tokens } = hl.codeToTokens(source, { lang, theme: "css-variables" })
  return tokens.map((line: ThemedToken[]) =>
    line.map((token) => ({
      content: token.content,
      color: token.color,
      // The css-variables theme carries no comment italics; rule 10 wants
      // them, so the variable name doubles as the signal.
      italic:
        Boolean((token.fontStyle ?? 0) & 1) ||
        (token.color?.includes("token-comment") ?? false),
    }))
  )
}
