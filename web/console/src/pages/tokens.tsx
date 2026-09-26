/** Tokens, which are also the sessions: Settings' "Signed in" rows and, for
 * developers, the full token table with the mint form.
 *
 * A session IS a token record: signing in mints one, and there is no session
 * table beside it. So this one list is every way into the repository (this
 * browser, a script, a phone), and signing one out deletes the record, the
 * same write `substratectl` performs. The token whose id matches
 * getTokenId() is THIS browser; signing it out signs this browser out.
 *
 * A minted secret is shown ONCE. Nothing can show it again, which is why the
 * panel that carries it stays until it is dismissed. */

import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { CopyIcon, XIcon } from "lucide-react"

import { Pill } from "@/components/identity/pill"

import { SettingRow } from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, SignOutDialog } from "@/components/ui/confirm-dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { toast } from "@/components/ui/toast"
import { listTokens, mintToken, revokeToken } from "@/lib/api/auth"
import { clearSession, getTokenId } from "@/lib/api/session"
import { ApiError, type MintedToken, type TokenInfo } from "@/lib/api/types"
import { relativeTime, shortDateTime } from "@/lib/format"

const TOKENS_KEY = ["tokens"] as const

function useTokens() {
  return useQuery({ queryKey: TOKENS_KEY, queryFn: listTokens })
}

/** A token as a person knows it: this browser, the command line, a browser,
 * or the label it was minted with. */
// eslint-disable-next-line react-refresh/only-export-components -- pure words, exported for their test
export function tokenWords(
  t: TokenInfo,
  current: boolean,
  now = Date.now()
): { title: string; description: string } {
  const since = `Signed in ${relativeTime(t.createdAt, now)}`
  const expires = t.expiresAt
    ? ` · stops working ${relativeTime(t.expiresAt, now)}`
    : ""
  if (current) {
    return { title: "This browser", description: `${since}${expires}` }
  }
  if (t.label.startsWith("substratectl")) {
    const host = t.label.split("@")[1]
    return {
      title: "Command line",
      description: `substratectl${host ? ` on ${host}` : ""} · ${since.toLowerCase()}${expires}`,
    }
  }
  if (t.label === "console") {
    return { title: "Another browser", description: `${since}${expires}` }
  }
  return { title: t.label, description: `${since}${expires}` }
}

/** A token as a sentence names it: "command line", "another browser", or
 * the label it was minted with, as written. */
function signedInAs(t: TokenInfo): string {
  const title = tokenWords(t, false).title
  return title === t.label ? title : title.toLowerCase()
}

/** Signing a token out, confirmed first: it cannot be undone, and signing
 * out THIS browser ends the session the console is running on. */
function useSignOut() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const currentId = getTokenId()
  const [pending, setPending] = useState<TokenInfo | null>(null)
  const revoke = useMutation({
    mutationFn: (t: TokenInfo) => revokeToken(t.id),
    onSuccess: (_res, t) => {
      setPending(null)
      if (t.id === currentId) {
        // Revoking THIS session ends it: drop the local copy and return to the
        // door rather than let the next call 401 into a broken console.
        clearSession()
        void navigate({ to: "/login", replace: true })
        return
      }
      toast.add({
        type: "success",
        title: `Signed out ${signedInAs(t)}.`,
      })
      void queryClient.invalidateQueries({ queryKey: TOKENS_KEY })
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: "Signing out didn’t work",
        description: error.message,
      })
    },
  })
  const dialog =
    pending &&
    (pending.id === currentId ? (
      <SignOutDialog
        pending={revoke.isPending}
        onConfirm={() => revoke.mutate(pending)}
        onClose={() => setPending(null)}
      />
    ) : (
      <ConfirmDialog
        title={`Sign out ${signedInAs(pending)}?`}
        consequence="It stops working straight away and has to sign in again."
        confirm="Sign out"
        destructive
        pending={revoke.isPending}
        onConfirm={() => revoke.mutate(pending)}
        onClose={() => setPending(null)}
      />
    ))
  return { ask: setPending, busy: revoke.isPending, dialog, currentId }
}

function TokensError({ error, retry }: { error: Error; retry: () => void }) {
  return (
    <SettingRow
      title="Your sign-ins didn’t load"
      description={
        error instanceof ApiError ? error.message : "Something went wrong."
      }
      control={
        <Button variant="outline" size="sm" onClick={retry}>
          Try again
        </Button>
      }
    />
  )
}

/** Settings' "Signed in" rows: this browser first, then every other token,
 * newest first, each one "Sign out…" away. */
export function SignedInRows() {
  const tokens = useTokens()
  const { ask, busy, dialog, currentId } = useSignOut()
  if (tokens.isPending) {
    return (
      <div className="flex flex-col gap-2 p-4">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
      </div>
    )
  }
  if (tokens.isError) {
    return (
      <TokensError error={tokens.error} retry={() => void tokens.refetch()} />
    )
  }
  const rows = [...tokens.data].sort(
    (a, b) =>
      Number(b.id === currentId) - Number(a.id === currentId) ||
      b.createdAt.localeCompare(a.createdAt)
  )
  return (
    <>
      {rows.map((t) => {
        const current = t.id === currentId
        const words = tokenWords(t, current)
        return (
          <SettingRow
            key={t.id}
            title={words.title}
            description={<span title={t.createdAt}>{words.description}</span>}
            control={
              <>
                {current && (
                  <Pill tone="ok" dot={false}>
                    You are here
                  </Pill>
                )}
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  aria-label={`Sign out ${current ? "this browser" : t.label}`}
                  onClick={() => ask(t)}
                >
                  Sign out…
                </Button>
              </>
            }
          />
        )
      })}
      {dialog}
    </>
  )
}

/** A calendar day the browser can turn into an instant. The wire wants RFC
 * 3339; a person writing an expiry means a day, so the console takes the day
 * and ends it at the last second, UTC: the reading that never expires a token
 * early. */
function isCalendarDay(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const parsed = new Date(`${value}T00:00:00Z`)
  return (
    !Number.isNaN(parsed.getTime()) &&
    parsed.toISOString().slice(0, 10) === value
  )
}

async function copy(secret: string) {
  try {
    await navigator.clipboard?.writeText(secret)
    toast.add({ type: "success", title: "Secret copied." })
  } catch {
    toast.add({
      type: "error",
      title: "Copying failed. Select the secret and copy it.",
    })
  }
}

function MintedPanel({
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
          <p className="font-medium">
            “{minted.token.label}” is live. Copy the secret now.
          </p>
          <p className="text-[12.5px] text-muted-foreground">
            This is the only time the secret is shown. If you lose it, mint
            another token.
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
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-md bg-background px-2.5 py-1.5 font-mono text-xs">
          {minted.secret}
        </code>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void copy(minted.secret)}
        >
          <CopyIcon />
          Copy
        </Button>
      </div>
    </div>
  )
}

function MintForm({ onMinted }: { onMinted: (m: MintedToken) => void }) {
  const queryClient = useQueryClient()
  const [label, setLabel] = useState("")
  const [expiresAt, setExpiresAt] = useState("")

  const expiryInvalid = expiresAt !== "" && !isCalendarDay(expiresAt)
  const canMint = label.trim().length > 0 && !expiryInvalid

  const mint = useMutation({
    mutationFn: () => {
      const expiry = expiresAt
        ? new Date(`${expiresAt}T23:59:59Z`).toISOString()
        : undefined
      return mintToken(label.trim(), expiry)
    },
    onSuccess: (result) => {
      onMinted(result)
      setLabel("")
      setExpiresAt("")
      void queryClient.invalidateQueries({ queryKey: TOKENS_KEY })
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: "Minting didn’t work",
        description: error.message,
      })
    },
  })

  return (
    <form
      aria-label="Mint a token"
      className="flex flex-wrap items-start gap-3"
      onSubmit={(e) => {
        e.preventDefault()
        if (canMint) mint.mutate()
      }}
    >
      <Field className="flex-1 basis-48">
        <FieldLabel htmlFor="label">Label</FieldLabel>
        <Input
          id="label"
          placeholder="laptop cli"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
        />
      </Field>
      <Field
        className="flex-1 basis-44"
        data-invalid={expiryInvalid || undefined}
      >
        <FieldLabel htmlFor="expires">Expires</FieldLabel>
        <Input
          id="expires"
          className="font-mono"
          placeholder="2027-01-31"
          aria-invalid={expiryInvalid}
          value={expiresAt}
          onChange={(e) => setExpiresAt(e.target.value)}
        />
        {expiryInvalid ? (
          <FieldError errors={[{ message: "Write the day as YYYY-MM-DD." }]} />
        ) : (
          <FieldDescription>
            Optional. Without one it lasts until revoked.
          </FieldDescription>
        )}
      </Field>
      <Button
        type="submit"
        className="mt-6"
        disabled={!canMint || mint.isPending}
      >
        {mint.isPending && <Spinner />}
        Mint
      </Button>
    </form>
  )
}

/** Developer: every token record, raw, and the form that mints one for a
 * script or a device. Every token has full access. */
export function ApiTokens() {
  const [minted, setMinted] = useState<MintedToken | null>(null)
  const tokens = useTokens()
  const { ask, busy, dialog, currentId } = useSignOut()
  const rows = tokens.data ?? []
  return (
    <div className="flex flex-col gap-4">
      {minted && (
        <MintedPanel minted={minted} onDismiss={() => setMinted(null)} />
      )}
      <MintForm onMinted={setMinted} />
      {tokens.isPending ? (
        <Skeleton className="h-20 w-full" />
      ) : tokens.isError ? (
        <p className="text-muted-foreground">
          The tokens didn’t load: {tokens.error.message}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Label</TableHead>
                <TableHead>Id</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="text-right">Revoke</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((t) => (
                <TableRow key={t.id}>
                  <TableCell>
                    <span className="font-medium">{t.label}</span>
                    {t.id === currentId && (
                      <span className="ml-2 rounded-full bg-hover px-1.5 py-px text-xs text-muted-foreground">
                        this session
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {t.id}
                  </TableCell>
                  <TableCell
                    className="text-muted-foreground"
                    title={t.createdAt}
                  >
                    {relativeTime(t.createdAt)}
                  </TableCell>
                  <TableCell
                    className="text-muted-foreground"
                    title={t.expiresAt ? shortDateTime(t.expiresAt) : undefined}
                  >
                    {t.expiresAt ? relativeTime(t.expiresAt) : "never"}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="destructive"
                      size="sm"
                      disabled={busy}
                      onClick={() => ask(t)}
                    >
                      {t.id === currentId ? "Revoke (signs out)" : "Revoke"}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {dialog}
    </div>
  )
}
