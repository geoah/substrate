/** Finding a substrate from what a person typed: a server's address, or a
 * repository name whose `_substrate` TXT record names its server (the kit's
 * ServerLocator). A browser has no DNS, so the record is read over DNS over
 * HTTPS. */

export interface AuthorizationServerMetadata {
  issuer: string
  authorization_endpoint: string
  token_endpoint: string
  revocation_endpoint?: string
  scopes_supported?: string[]
  authorization_response_iss_parameter_supported?: boolean
  authorization_details_types_supported?: string[]
  client_id_metadata_document_supported?: boolean
}

export interface ServerDocument {
  server?: { version?: string; build?: string }
  features?: { name: string; stability: string }[]
}

export interface LocatedServer {
  /** The server's origin, `https://host`. */
  server: string
  /** The repository name the server was found from, when one was typed. */
  repository?: string
  metadata: AuthorizationServerMetadata
  document: ServerDocument
}

export class NotASubstrate extends Error {
  constructor(message: string) {
    super(message)
    this.name = "NotASubstrate"
  }
}

const dohEndpoint = "https://cloudflare-dns.com/dns-query"

/** An https origin from typed text: a bare host is completed with https. */
export function originOf(typed: string): string | undefined {
  const text = typed.trim()
  if (!text) return undefined
  try {
    const u = new URL(
      /^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? text : `https://${text}`
    )
    if (
      u.protocol !== "https:" &&
      !(u.protocol === "http:" && isLoopback(u.hostname))
    )
      return undefined
    if (!u.hostname.includes(".") && !isLoopback(u.hostname)) return undefined
    return u.origin
  } catch {
    return undefined
  }
}

export function isLoopback(host: string): boolean {
  return (
    host === "localhost" ||
    host === "127.0.0.1" ||
    host === "[::1]" ||
    host === "::1"
  )
}

/** A bare name that could be a repository: a host with no path or port. */
function repositoryName(typed: string): string | undefined {
  const text = typed.trim().toLowerCase().replace(/\.$/, "")
  if (!/^[a-z0-9-]+(\.[a-z0-9-]+)+$/.test(text)) return undefined
  return text
}

async function getJSON<T>(
  url: string,
  signal?: AbortSignal
): Promise<T | undefined> {
  try {
    const res = await fetch(url, {
      headers: { Accept: "application/json" },
      signal,
      redirect: "error",
    })
    if (!res.ok) return undefined
    return (await res.json()) as T
  } catch (e) {
    if (signal?.aborted) throw e
    return undefined
  }
}

/** Probes one origin: its OAuth metadata, on its own origin, and its server
 * document. */
export async function probe(
  origin: string,
  signal?: AbortSignal
): Promise<LocatedServer> {
  const [metadata, document] = await Promise.all([
    getJSON<AuthorizationServerMetadata>(
      `${origin}/.well-known/oauth-authorization-server`,
      signal
    ),
    getJSON<ServerDocument>(
      `${origin}/.well-known/substrate/server.json`,
      signal
    ),
  ])
  if (
    !metadata ||
    !document ||
    !metadata.authorization_endpoint ||
    !metadata.token_endpoint
  ) {
    throw new NotASubstrate(
      `${new URL(origin).host} is not a Substrate server.`
    )
  }
  for (const endpoint of [
    metadata.issuer,
    metadata.authorization_endpoint,
    metadata.token_endpoint,
  ]) {
    if (new URL(endpoint).origin !== origin) {
      throw new NotASubstrate(
        `${new URL(origin).host} names its sign-in on another server (${endpoint}).`
      )
    }
  }
  if (!document.features?.some((f) => f.name === "apps")) {
    throw new NotASubstrate(
      `${new URL(origin).host} is a Substrate server too old for apps: it needs an update first.`
    )
  }
  return { server: origin, metadata, document }
}

/** The server a repository's `_substrate` TXT record names. */
export async function recordServer(
  name: string,
  signal?: AbortSignal
): Promise<string | undefined> {
  const url = `${dohEndpoint}?name=${encodeURIComponent(`_substrate.${name}`)}&type=TXT`
  const res = await fetch(url, {
    headers: { Accept: "application/dns-json" },
    signal,
  })
  if (!res.ok)
    throw new Error(
      `The DNS lookup for _substrate.${name} failed. Check your connection and try again.`
    )
  const body = (await res.json()) as {
    Answer?: { type: number; data: string }[]
  }
  for (const answer of body.Answer ?? []) {
    if (answer.type !== 16) continue
    // TXT data arrives quoted, possibly as several strings: join them.
    const text = answer.data.replace(/^"|"$/g, "").replace(/"\s*"/g, "")
    const fields = text.split(/\s+/)
    if (fields[0] !== "v=substrate1") continue
    const server = fields
      .find((f) => f.startsWith("server="))
      ?.slice("server=".length)
    if (server) return originOf(server)
  }
  return undefined
}

/** The server for what a person typed: the address itself when it is a
 * substrate, else the repository's record, else its document, else its
 * parent domain. */
export async function locate(
  typed: string,
  signal?: AbortSignal
): Promise<LocatedServer> {
  const origin = originOf(typed)
  if (!origin)
    throw new NotASubstrate(
      "That is not a server address or a repository name."
    )
  try {
    return await probe(origin, signal)
  } catch (e) {
    if (!(e instanceof NotASubstrate)) throw e
    const name = repositoryName(typed)
    if (!name) throw e
    const fromRecord = await recordServer(name, signal)
    if (fromRecord)
      return { ...(await probe(fromRecord, signal)), repository: name }
    const doc = await getJSON<{ server?: string }>(
      `https://${name}/.well-known/substrate/repository.json`,
      signal
    )
    const fromDoc = doc?.server ? originOf(doc.server) : undefined
    if (fromDoc) return { ...(await probe(fromDoc, signal)), repository: name }
    const parent = name.split(".").slice(1).join(".")
    if (parent.includes(".")) {
      try {
        return {
          ...(await probe(`https://${parent}`, signal)),
          repository: name,
        }
      } catch (pe) {
        if (!(pe instanceof NotASubstrate)) throw pe
      }
    }
    throw new NotASubstrate(
      `${name} is not a Substrate server, and it does not say where its repository is served: it has no _substrate record and no /.well-known/substrate/repository.json.`
    )
  }
}
