/** AI assistants (decision 0230): Claude Code, Codex, Claude, Cursor and
 * others reach a substrate through its MCP address, with a token whose
 * stored scopes are exactly `["mcp"]` and which works there and nowhere
 * else. Settings lists those tokens on their own page, never under Apps or
 * Signed in. */

import type { TokenInfo } from "@/lib/api/types"
import { substrateOrigin } from "@/lib/shell"

/** The scope word an assistant's token stores, alone. */
export const ASSISTANT_SCOPE = "mcp"

/** Where an assistant connects: this server's MCP address. In the native
 * apps the page's own origin is the app's, so the server's comes from the
 * shell. */
export function assistantAddress(origin: string = substrateOrigin()): string {
  return `${origin}/mcp`
}

/** An assistant's token: its stored scopes are exactly `["mcp"]`. A `full`
 * token stores none, so the two never meet. */
export function isAssistantToken(t: TokenInfo): boolean {
  return t.scopes?.length === 1 && t.scopes[0] === ASSISTANT_SCOPE
}

/** The environment variable Cursor's config reads the token from. */
export const CURSOR_TOKEN_VARIABLE = "SUBSTRATE_TOKEN"

/** Cursor's `~/.cursor/mcp.json`: Cursor signs in by dynamic registration
 * alone, which this server does not offer, so it sends a token the owner
 * made, read from the environment rather than written into the file. */
export function cursorConfig(address: string): string {
  return JSON.stringify(
    {
      mcpServers: {
        substrate: {
          url: address,
          headers: {
            Authorization: `Bearer \${env:${CURSOR_TOKEN_VARIABLE}}`,
          },
        },
      },
    },
    null,
    2
  )
}
