/** Tokens — which is also the sessions surface.
 *
 * A session IS a token record: logging in mints one, and there
 * is no session table beside it. So this one list is every way into the
 * repository — this browser, a script, a phone — and revoking a row deletes
 * the record, the same write `substratectl` performs. The token whose id matches
 * getTokenId() is THIS session; revoking it signs this browser out.
 *
 * The secret is shown ONCE, at mint. Nothing can show it again, which is why
 * the panel that carries it stays until it is dismissed. */

import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { CopyIcon, KeyRoundIcon, SearchXIcon, XIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
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

/** A calendar day the browser can turn into an instant. The wire wants RFC
 * 3339; a person writing an expiry means a day, so the console takes the day
 * and ends it at the last second, UTC — the reading that never expires a token
 * early. */
function isCalendarDay(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const parsed = new Date(`${value}T00:00:00Z`)
  return !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value
}

async function copy(secret: string) {
  try {
    await navigator.clipboard?.writeText(secret)
    toast.add({ type: "success", title: "Secret copied." })
  } catch {
    toast.add({ type: "error", title: "Could not reach the clipboard — select and copy." })
  }
}

/** The one time the secret is shown. Stays until dismissed. */
function MintedPanel({
  minted,
  onDismiss,
}: {
  minted: MintedToken
  onDismiss: () => void
}) {
  return (
    <Card className="ring-primary/30">
      <CardHeader>
        <CardTitle>“{minted.token.label}” is live — copy the secret now</CardTitle>
        <CardDescription>
          This is the only time the secret is shown. The substrate stores its
          SHA-256 and nothing else — lose it and the only remedy is minting
          another.
        </CardDescription>
        <CardAction>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Dismiss"
            onClick={onDismiss}
          >
            <XIcon />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="flex items-center gap-2">
        <code className="data min-w-0 flex-1 truncate rounded-lg bg-muted px-2.5 py-1.5 text-xs">
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
      </CardContent>
    </Card>
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
      toast.add({ type: "error", title: "Could not mint", description: error.message })
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Mint a token</CardTitle>
        <CardDescription>
          For a script, a device, or another browser. The label is how you will
          recognise it here.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
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
              className="data"
              placeholder="2027-01-31"
              aria-invalid={expiryInvalid}
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
            />
            {expiryInvalid ? (
              <FieldError
                errors={[{ message: "Write the day as YYYY-MM-DD." }]}
              />
            ) : (
              <FieldDescription>
                Optional. A token without an expiry lives until revoked.
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
      </CardContent>
    </Card>
  )
}

function TokenRows({
  tokens,
  onMintedRevoked,
}: {
  tokens: TokenInfo[]
  onMintedRevoked: (id: string) => void
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const currentId = getTokenId()

  const revoke = useMutation({
    mutationFn: (t: TokenInfo) => revokeToken(t.id),
    onSuccess: (_res, t) => {
      onMintedRevoked(t.id)
      toast.add({ type: "success", title: `Revoked ${t.label}.` })
      if (t.id === currentId) {
        // Revoking THIS session ends it: drop the local copy and return to the
        // door rather than let the next call 401 into a broken console.
        clearSession()
        void navigate({ to: "/login", replace: true })
        return
      }
      void queryClient.invalidateQueries({ queryKey: TOKENS_KEY })
    },
    onError: (error) => {
      toast.add({ type: "error", title: "Could not revoke", description: error.message })
    },
  })

  return (
    <>
      {tokens.map((t) => (
        <TableRow key={t.id}>
          <TableCell>
            <span className="font-medium">{t.label}</span>
            {t.id === currentId && (
              <Badge variant="secondary" className="ml-2">
                this session
              </Badge>
            )}
          </TableCell>
          <TableCell className="data text-muted-foreground" title={t.createdAt}>
            {relativeTime(t.createdAt)}
          </TableCell>
          <TableCell
            className="data text-muted-foreground"
            title={t.expiresAt ? shortDateTime(t.expiresAt) : undefined}
          >
            {t.expiresAt ? relativeTime(t.expiresAt) : "never"}
          </TableCell>
          <TableCell
            className="data text-muted-foreground"
            title={t.lastUsedAt ? shortDateTime(t.lastUsedAt) : undefined}
          >
            {t.lastUsedAt ? relativeTime(t.lastUsedAt) : "never"}
          </TableCell>
          <TableCell className="text-right">
            <Button
              variant="destructive"
              size="sm"
              disabled={revoke.isPending}
              onClick={() => revoke.mutate(t)}
            >
              {t.id === currentId ? "Revoke (signs out)" : "Revoke"}
            </Button>
          </TableCell>
        </TableRow>
      ))}
    </>
  )
}

export function TokensPage() {
  const [minted, setMinted] = useState<MintedToken | null>(null)
  const tokens = useQuery({ queryKey: TOKENS_KEY, queryFn: listTokens })

  const rows = tokens.data ?? []

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-end justify-between gap-3 px-6 pt-5 pb-2">
        <div>
          <h1 className="text-lg font-semibold">Tokens</h1>
          <p className="text-xs text-muted-foreground">
            Every way into this repository — a session is a token record, so
            this is also every browser you are signed in on. A token has full
            access; there are no scopes.
          </p>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="flex flex-col gap-6 px-6 py-4">
          {minted && (
            <MintedPanel minted={minted} onDismiss={() => setMinted(null)} />
          )}
          <MintForm onMinted={setMinted} />

          <div className="flex flex-col gap-2">
            <h2 className="text-sm font-medium">
              Live tokens
              {rows.length > 0 && (
                <span className="data ml-2 text-xs font-normal text-muted-foreground">
                  {rows.length}
                </span>
              )}
            </h2>
            {tokens.isPending ? (
              <div className="flex flex-col gap-2">
                <Skeleton className="h-9 w-full" />
                <Skeleton className="h-9 w-full" />
              </div>
            ) : tokens.isError ? (
              <Empty className="py-10">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <SearchXIcon />
                  </EmptyMedia>
                  <EmptyTitle>Could not load your tokens</EmptyTitle>
                  <EmptyDescription>
                    {tokens.error instanceof ApiError
                      ? tokens.error.message
                      : "Something went wrong."}
                  </EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void tokens.refetch()}
                  >
                    Retry
                  </Button>
                </EmptyContent>
              </Empty>
            ) : rows.length === 0 ? (
              <Empty className="py-10">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <KeyRoundIcon />
                  </EmptyMedia>
                  <EmptyTitle>No tokens</EmptyTitle>
                  <EmptyDescription>
                    Signing in mints one, so this list is never empty for long.
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className="rounded-xl border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Label</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead>Expires</TableHead>
                      <TableHead>Last used</TableHead>
                      <TableHead className="text-right">Revoke</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TokenRows
                      tokens={rows}
                      onMintedRevoked={(id) =>
                        setMinted((m) => (m?.token.id === id ? null : m))
                      }
                    />
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
