/** Settings → AI assistants (`/settings/assistants`, decision 0230): the
 * address an assistant connects to, how each one connects, what an assistant
 * can and cannot do, and the ones connected, each "Revoke…" away.
 *
 * An assistant's token is one whose stored scopes are exactly `["mcp"]`. One
 * that signs in gets it from the approval page; Cursor, which cannot sign in
 * here yet, and any other client that takes a pasted token get one made on
 * this page, whose secret is shown ONCE. */

import { useState, type ReactNode } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CheckIcon, CopyIcon, XIcon } from "lucide-react"

import { CopyButton } from "@/components/identity/copy-button"
import { PageHeader } from "@/components/identity/page-header"
import { DocPage } from "@/components/identity/page-layout"
import { SectionHead } from "@/components/identity/section-head"
import {
  SettingRow,
  SettingsSection,
} from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toast } from "@/components/ui/toast"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  mintAssistantToken,
  revokeToken,
  tokensQueryOptions,
} from "@/lib/api/auth"
import { ApiError, type MintedToken, type TokenInfo } from "@/lib/api/types"
import {
  ASSISTANT_CAN,
  ASSISTANT_CANNOT,
  ASSISTANT_CODE_SENTENCE,
  ASSISTANT_HISTORY_SENTENCE,
} from "@/lib/app-scopes"
import {
  assistantAddress,
  cursorConfig,
  CURSOR_TOKEN_VARIABLE,
  isAssistantToken,
} from "@/lib/assistants"
import { relativeTime, shortDateTime } from "@/lib/format"
import { appActor } from "@/pages/tokens"

async function copy(value: string, what: string) {
  try {
    await navigator.clipboard?.writeText(value)
    toast.add({ type: "success", title: `${what} copied.` })
  } catch {
    toast.add({
      type: "error",
      title: `Copying failed. Select the ${what.toLowerCase()} and copy it.`,
    })
  }
}

/** Something to paste: a command, a config, the address. It wraps rather
 * than scrolls, and its button copies it exactly. */
function Code({ value, label }: { value: string; label: string }) {
  return (
    <div className="relative">
      <pre className="rounded-lg border border-border bg-muted/40 p-3 pr-9 font-mono text-xs [overflow-wrap:anywhere] whitespace-pre-wrap">
        {value}
      </pre>
      <CopyButton
        value={value}
        label={label}
        className="absolute top-2 right-2"
      />
    </div>
  )
}

/** What the reader types into the assistant: a command, a name, a path. */
function Typed({ children }: { children: ReactNode }) {
  return (
    <code className="rounded-sm bg-muted px-1 font-mono text-[0.86em]">
      {children}
    </code>
  )
}

/** A button or a setting the reader finds in the assistant's own window. */
function Control({ children }: { children: ReactNode }) {
  return <span className="font-medium">{children}</span>
}

function Steps({ children }: { children: ReactNode }) {
  return (
    <ol className="flex list-decimal flex-col gap-3 pl-5 marker:text-faint">
      {children}
    </ol>
  )
}

function Step({ children }: { children: ReactNode }) {
  return <li className="flex flex-col gap-1.5 pl-1">{children}</li>
}

function Note({ children }: { children: ReactNode }) {
  return <p className="text-[12.5px] text-muted-foreground">{children}</p>
}

function AddressRow({ address }: { address: string }) {
  return (
    <SettingRow
      title={
        <span className="font-mono text-[13px] font-normal">{address}</span>
      }
      description="Give this address to your assistant. It works with this server alone."
      control={
        <Button
          variant="outline"
          size="sm"
          aria-label="Copy the address"
          onClick={() => void copy(address, "Address")}
        >
          <CopyIcon />
          Copy
        </Button>
      }
    />
  )
}

/** The once-only token, with the line that hands it to the assistant. */
function TokenOnce({
  minted,
  onDismiss,
}: {
  minted: MintedToken
  onDismiss: () => void
}) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-primary/40 bg-primary-soft p-3">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="font-medium [overflow-wrap:anywhere]">
            The token for “{minted.token.label}” is ready. Copy it now.
          </p>
          <p className="text-[12.5px] text-muted-foreground">
            This is the only time it is shown. If you lose it, revoke it below
            and make another.
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Dismiss"
          onClick={onDismiss}
        >
          <XIcon />
        </Button>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <code className="min-w-0 flex-1 basis-48 rounded-md bg-background px-2.5 py-1.5 font-mono text-xs [overflow-wrap:anywhere]">
          {minted.secret}
        </code>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void copy(minted.secret, "Token")}
        >
          <CopyIcon />
          Copy
        </Button>
      </div>
    </div>
  )
}

/** Makes an assistant's token: named for the client where the tab knows it
 * (Cursor), else by what the owner types. */
function MakeToken({
  name,
  minted,
  onMinted,
}: {
  /** The client the token is for; absent asks for a name. */
  name?: string
  minted: MintedToken | undefined
  onMinted: (minted: MintedToken | undefined) => void
}) {
  const queryClient = useQueryClient()
  const [typed, setTyped] = useState("")
  const label = (name ?? typed).trim()
  const make = useMutation({
    mutationFn: async (label: string) => {
      const made = await mintAssistantToken(label)
      // A server that answered with any other token made more than this
      // page offered: it goes before anyone can copy it.
      if (!isAssistantToken(made.token)) {
        await revokeToken(made.token.id)
        throw new Error(
          "This server can’t make an assistant’s token yet. What it made instead was revoked straight away."
        )
      }
      return made
    },
    onSuccess: (made) => {
      onMinted(made)
      setTyped("")
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: "Making the token didn’t work",
        description: error.message,
      })
    },
    onSettled: () =>
      void queryClient.invalidateQueries({
        queryKey: tokensQueryOptions.queryKey,
      }),
  })
  if (minted)
    return <TokenOnce minted={minted} onDismiss={() => onMinted(undefined)} />
  return (
    <form
      aria-label={name ? `Make a token for ${name}` : "Make a token"}
      className="flex flex-wrap items-end gap-3"
      onSubmit={(e) => {
        e.preventDefault()
        if (label) make.mutate(label)
      }}
    >
      {!name && (
        <Field className="flex-1 basis-48">
          <FieldLabel htmlFor="assistant-name">Name</FieldLabel>
          <Input
            id="assistant-name"
            placeholder="The assistant’s name"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
          />
        </Field>
      )}
      <Button type="submit" disabled={!label || make.isPending}>
        {make.isPending && <Spinner />}
        {name ? `Make a token for ${name}` : "Make a token"}
      </Button>
    </form>
  )
}

const CLIENTS = [
  { value: "claude-code", label: "Claude Code" },
  { value: "codex", label: "Codex" },
  { value: "claude", label: "Claude" },
  { value: "cursor", label: "Cursor" },
  { value: "other", label: "Other" },
] as const

type Client = (typeof CLIENTS)[number]["value"]

/** One tab per client, each with its exact steps. A token made on a tab
 * stays with it while the reader moves between tabs, since it is never
 * shown again. */
function ClientTabs({ address }: { address: string }) {
  const [minted, setMinted] = useState<
    Partial<Record<Client, MintedToken | undefined>>
  >({})
  const mintedFor = (client: Client) => ({
    minted: minted[client],
    onMinted: (m: MintedToken | undefined) =>
      setMinted((old) => ({ ...old, [client]: m })),
  })
  const cursorSecret = minted.cursor?.secret
  return (
    <Tabs defaultValue="claude-code" className="gap-3">
      <TabsList
        variant="line"
        aria-label="Assistant"
        className="max-w-full justify-start overflow-x-auto"
      >
        {CLIENTS.map((c) => (
          <TabsTrigger key={c.value} value={c.value} className="flex-none">
            {c.label}
          </TabsTrigger>
        ))}
      </TabsList>
      <div className="rounded-[10px] border border-border p-4">
        <TabsContent value="claude-code" className="flex flex-col gap-3">
          <Steps>
            <Step>
              <p>
                In a terminal, add substrate to Claude Code for all your
                projects:
              </p>
              <Code
                value={`claude mcp add --transport http --scope user substrate ${address}`}
                label="Copy the Claude Code command"
              />
            </Step>
            <Step>
              <p>
                In Claude Code, type <Typed>/mcp</Typed>, choose{" "}
                <Control>substrate</Control>, then{" "}
                <Control>Authenticate</Control>.
              </p>
            </Step>
            <Step>
              <p>
                Your browser opens this console’s approval page. Approve it.
              </p>
            </Step>
          </Steps>
        </TabsContent>
        <TabsContent value="codex" className="flex flex-col gap-3">
          <Steps>
            <Step>
              <p>In a terminal, add substrate to Codex:</p>
              <Code
                value={`codex mcp add substrate --url ${address}`}
                label="Copy the Codex command"
              />
            </Step>
            <Step>
              <p>Sign it in:</p>
              <Code
                value="codex mcp login substrate"
                label="Copy the Codex sign-in command"
              />
              <p>
                Your browser opens this console’s approval page. Approve it.
              </p>
            </Step>
          </Steps>
          <Note>
            After you revoke it, or when the access you approved runs out, run{" "}
            <Typed>codex mcp login substrate</Typed> again.
          </Note>
        </TabsContent>
        <TabsContent value="claude" className="flex flex-col gap-3">
          <p>For Claude on the web and Claude Desktop.</p>
          <Steps>
            <Step>
              <p>
                In Claude, open <Control>Settings</Control>, then{" "}
                <Control>Connectors</Control>, and choose{" "}
                <Control>Add custom connector</Control>.
              </p>
            </Step>
            <Step>
              <p>
                Name it <Typed>substrate</Typed>, and paste this address as its
                URL. Keep <Control>Use Claude’s published identity</Control> as
                it is.
              </p>
              <Code value={address} label="Copy the address" />
            </Step>
            <Step>
              <p>
                Add it and connect it. This console’s approval page opens.
                Approve it only if you just started this yourself.
              </p>
            </Step>
          </Steps>
          <Note>
            Claude connects from Anthropic’s servers, not from your computer, so
            this works only where this server can be reached from the internet.
          </Note>
        </TabsContent>
        <TabsContent value="cursor" className="flex flex-col gap-3">
          <p>
            Cursor can’t sign in here yet, so it uses a token you make here
            instead.
          </p>
          <Steps>
            <Step>
              <p>Make a token for Cursor.</p>
              <MakeToken name="Cursor" {...mintedFor("cursor")} />
            </Step>
            <Step>
              <p>
                Give it to Cursor as <Typed>{CURSOR_TOKEN_VARIABLE}</Typed>, for
                example in your shell profile:
              </p>
              <Code
                value={`export ${CURSOR_TOKEN_VARIABLE}=${cursorSecret ?? "<the token>"}`}
                label="Copy the line that sets the token"
              />
            </Step>
            <Step>
              <p>
                Add substrate to <Typed>~/.cursor/mcp.json</Typed>, then restart
                Cursor:
              </p>
              <Code
                value={cursorConfig(address)}
                label="Copy Cursor’s settings"
              />
            </Step>
          </Steps>
          <Note>
            Whoever holds the token can do what an assistant can, at this
            address alone. Revoke it below once you stop using it.
          </Note>
        </TabsContent>
        <TabsContent value="other" className="flex flex-col gap-3">
          <p>Any assistant that speaks MCP over Streamable HTTP can connect.</p>
          <dl className="flex flex-col gap-3">
            <div className="flex flex-col gap-1">
              <dt className="text-[12.5px] text-faint">Address</dt>
              <dd>
                <Code value={address} label="Copy the address" />
              </dd>
            </div>
            <div className="flex flex-col gap-0.5">
              <dt className="text-[12.5px] text-faint">Signing in</dt>
              <dd>
                OAuth, through this console’s approval page. The assistant’s
                client id is the web address of its client metadata document:
                this server has no registration step.
              </dd>
            </div>
            <div className="flex flex-col gap-1.5">
              <dt className="text-[12.5px] text-faint">Or a token</dt>
              <dd className="flex flex-col gap-2">
                <p>
                  Make one here, and have the assistant send it as{" "}
                  <Typed>Authorization: Bearer &lt;the token&gt;</Typed>.
                </p>
                <MakeToken {...mintedFor("other")} />
              </dd>
            </div>
          </dl>
        </TabsContent>
      </div>
    </Tabs>
  )
}

/** What an assistant can and cannot do: the same lines the approval page
 * shows before it is let in. */
function Reach() {
  return (
    <section aria-label="What an assistant can do">
      <SectionHead title="What an assistant can do" />
      <div className="flex flex-col gap-4 rounded-[10px] border border-border p-4">
        <div className="flex flex-col gap-1.5">
          <h3 className="font-medium">It can</h3>
          <ul aria-label="It can" className="flex flex-col gap-1">
            {ASSISTANT_CAN.map((line) => (
              <li key={line} className="flex gap-2">
                <CheckIcon
                  aria-hidden
                  className="mt-0.5 size-4 shrink-0 text-ok"
                />
                <span>{line}.</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="flex flex-col gap-1.5">
          <h3 className="font-medium">It can’t</h3>
          <ul aria-label="It can’t" className="flex flex-col gap-1">
            {ASSISTANT_CANNOT.map((line) => (
              <li key={line} className="flex gap-2">
                <XIcon
                  aria-hidden
                  className="mt-0.5 size-4 shrink-0 text-destructive"
                />
                <span>{line}.</span>
              </li>
            ))}
          </ul>
        </div>
        <p>{ASSISTANT_CODE_SENTENCE}.</p>
        <p>{ASSISTANT_HISTORY_SENTENCE}.</p>
      </div>
    </section>
  )
}

/** An assistant as its row heads it: the name it signed in with, else the
 * label its token was made under. */
function assistantName(t: TokenInfo): string {
  return t.clientName || t.label
}

/** The app behind an assistant: the host its client id names, the one fact
 * the id proves; a token made here names none. */
function assistantApp(t: TokenInfo): string | undefined {
  if (!t.clientId) return undefined
  try {
    return new URL(t.clientId).host || t.clientId
  } catch {
    return t.clientId
  }
}

/** One labelled fact of a row, its label above its value so neither side's
 * length moves the other. */
function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-faint">{label}</dt>
      <dd className="flex flex-col [overflow-wrap:anywhere]">{children}</dd>
    </div>
  )
}

/** The token has lapsed; the server may still list it until it is
 * revoked. */
function hasEnded(t: TokenInfo, now = Date.now()): boolean {
  return t.expiresAt !== undefined && Date.parse(t.expiresAt) <= now
}

function AssistantFacts({
  t,
  technical,
}: {
  t: TokenInfo
  technical: boolean
}) {
  const app = assistantApp(t)
  const ended = hasEnded(t)
  return (
    <dl
      aria-label={`About ${assistantName(t)}`}
      className="flex flex-wrap gap-x-8 gap-y-3 text-[12.5px]"
    >
      <Detail label="App">
        <span>{app ?? "A token you made"}</span>
        {technical && t.clientId && (
          <span className="font-mono text-muted-foreground">{t.clientId}</span>
        )}
      </Detail>
      <Detail label="Runs">
        <span>
          {t.appLocal
            ? "On your own device"
            : app
              ? `On ${app}`
              : "Wherever you put the token"}
        </span>
      </Detail>
      <Detail label={app ? "Approved" : "Made"}>
        <span title={shortDateTime(t.createdAt)}>
          {relativeTime(t.createdAt)}
        </span>
      </Detail>
      <Detail label={ended ? "Stopped working" : "Stops working"}>
        {t.expiresAt ? (
          <span title={shortDateTime(t.expiresAt)}>
            {relativeTime(t.expiresAt)}
          </span>
        ) : (
          <span>When you revoke it</span>
        )}
      </Detail>
      {technical && (
        <Detail label="Writes as">
          <span className="font-mono">{app ? appActor(t) : "api"}</span>
        </Detail>
      )}
    </dl>
  )
}

/** Every assistant's token, newest first, each "Revoke…" away. */
function ConnectedRows() {
  const tokens = useQuery(tokensQueryOptions)
  const queryClient = useQueryClient()
  const [technical] = useTechnicalDetails()
  const [pending, setPending] = useState<TokenInfo | null>(null)
  const revoke = useMutation({
    mutationFn: (t: TokenInfo) => revokeToken(t.id),
    onSuccess: (_res, t) => {
      setPending(null)
      toast.add({ type: "success", title: `Revoked ${assistantName(t)}.` })
      void queryClient.invalidateQueries({
        queryKey: tokensQueryOptions.queryKey,
      })
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: "Revoking didn’t work",
        description: error.message,
      })
    },
  })
  if (tokens.isPending) {
    return (
      <div className="flex flex-col gap-2 p-4">
        <Skeleton className="h-9 w-full" />
      </div>
    )
  }
  if (tokens.isError) {
    return (
      <SettingRow
        title="Your assistants didn’t load"
        description={
          tokens.error instanceof ApiError
            ? tokens.error.message
            : "Something went wrong."
        }
        control={
          <Button
            variant="outline"
            size="sm"
            onClick={() => void tokens.refetch()}
          >
            Try again
          </Button>
        }
      />
    )
  }
  const assistants = tokens.data
    .filter(isAssistantToken)
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
  if (assistants.length === 0) {
    return (
      <SettingRow
        title="None yet"
        description="An assistant you connect is listed here, with when it stops working."
      />
    )
  }
  return (
    <>
      {assistants.map((t) => {
        const name = assistantName(t)
        return (
          <SettingRow
            key={t.id}
            title={name}
            control={
              <Button
                variant="outline"
                size="sm"
                disabled={revoke.isPending}
                aria-label={`Revoke ${name}`}
                onClick={() => setPending(t)}
              >
                Revoke…
              </Button>
            }
          >
            <AssistantFacts t={t} technical={technical} />
          </SettingRow>
        )
      })}
      {pending && (
        <ConfirmDialog
          title={`Revoke ${assistantName(pending)}?`}
          consequence={
            pending.clientId
              ? `${assistantName(pending)} stops reaching your data straight away. What it already changed stays. To use it again, connect it again from the assistant.`
              : "Whatever uses this token stops reaching your data straight away. What it already changed stays. To connect again, make a new token."
          }
          confirm="Revoke"
          destructive
          pending={revoke.isPending}
          onConfirm={() => revoke.mutate(pending)}
          onClose={() => setPending(null)}
        />
      )}
    </>
  )
}

export function AssistantsPage() {
  const address = assistantAddress()
  return (
    <DocPage className="pb-20">
      <PageHeader
        title="AI assistants"
        description="Let Claude Code, Codex, Claude, Cursor or another AI assistant work with your data, through the address below."
      />
      <SettingsSection title="Address">
        <AddressRow address={address} />
      </SettingsSection>
      <section aria-label="Connect an assistant">
        <SectionHead title="Connect an assistant" />
        <ClientTabs address={address} />
      </section>
      <Reach />
      <SettingsSection title="Connected">
        <ConnectedRows />
      </SettingsSection>
    </DocPage>
  )
}
