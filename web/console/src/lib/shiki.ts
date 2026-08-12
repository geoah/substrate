/** The YAML tinting engine, isolated so the shiki bundle loads lazily: the
 * record page imports this module with `import()` and nothing else pulls it
 * into the main chunk. The theme is shiki's css-variables theme, so every
 * color rides `--shiki-*` variables defined next to the app tokens in
 * `index.css` — the tint follows light/dark for free (rule 10). */

// The fine-grained core: only the YAML grammar and the lightweight JS regex
// engine ship — the full `shiki` entry would bundle every language.
import {
  createCssVariablesTheme,
  createHighlighterCore,
  type HighlighterCore,
  type ThemedToken,
} from "shiki/core"
import { createJavaScriptRegexEngine } from "shiki/engine/javascript"
import yaml from "shiki/langs/yaml.mjs"

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
    langs: [yaml],
    engine: createJavaScriptRegexEngine(),
  }))
}

export interface YamlToken {
  content: string
  color?: string
  italic: boolean
}

export async function tokenizeYAML(source: string): Promise<YamlToken[][]> {
  const hl = await highlighter()
  const { tokens } = hl.codeToTokens(source, {
    lang: "yaml",
    theme: "css-variables",
  })
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
