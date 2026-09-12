/** The account: the two credential changes, both under the password-factor
 * rule.
 *
 * These endpoints do not accept a bearer token at all — the current password
 * AND code travel in the body, and a request that brings only the browser's
 * token is refused. That is the whole point: a leaked token's blast radius is
 * the data, never the account. So both forms ask for the password even though
 * you are plainly signed in.
 *
 * Live tokens SURVIVE a password change: a token is data access, the credential
 * is the account. Revoking is the tokens page's job. */

import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { CopyIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  CODE_DIGITS,
  changePassword,
  normalizeCode,
  totpChange,
  totpEnroll,
} from "@/lib/api/auth"
import { useAuthPolicy } from "@/lib/api/discovery"
import { getRepository } from "@/lib/api/session"
import { ApiError, type TOTPEnrollment } from "@/lib/api/types"

const MIN_PASSWORD = 8

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "forbidden") {
      return "Being signed in is not enough. Fill in your current password and code."
    }
    if (err.code === "auth") {
      return "The password or the code is wrong. Try a fresh code."
    }
    if (err.code === "rate_limited") {
      return `Too many tries. Wait ${err.retryAfter ?? 5}s and try again.`
    }
    return err.message
  }
  return "Something went wrong. Try again."
}

async function copy(value: string) {
  try {
    await navigator.clipboard?.writeText(value)
    toast.add({ type: "success", title: "Secret copied." })
  } catch {
    toast.add({
      type: "error",
      title: "Copying failed. Select the secret and copy it.",
    })
  }
}

export function AccountPage() {
  const repository = getRepository() ?? ""
  const { totpRequired } = useAuthPolicy()

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-end justify-between gap-3 px-6 pt-5 pb-2">
        <div>
          <h1 className="text-lg font-semibold">Account</h1>
          <p className="text-xs text-muted-foreground">
            Changing your password needs your current password
            {totpRequired && " and code"}. Being signed in is not enough.
          </p>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="flex flex-col gap-6 px-6 py-4">
          <Card>
            <CardHeader>
              <CardTitle>You</CardTitle>
              <CardDescription>
                Every signed-in browser and script holds a token. Open{" "}
                <Link
                  to="/account/tokens"
                  className="underline underline-offset-4 hover:text-foreground"
                >
                  Tokens
                </Link>{" "}
                to revoke one.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="flex items-baseline gap-2 text-sm">
                <span className="text-muted-foreground">Repository</span>
                <span className="data">{repository || "unknown"}</span>
              </div>
            </CardContent>
          </Card>

          <PasswordCard repository={repository} totpRequired={totpRequired} />
          {totpRequired ? (
            <TotpCard repository={repository} />
          ) : (
            <TotpOffCard />
          )}
        </div>
      </div>
    </div>
  )
}

function PasswordCard({
  repository,
  totpRequired,
}: {
  repository: string
  totpRequired: boolean
}) {
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  const matches = next.length > 0 && next === confirm
  const canSubmit =
    repository !== "" &&
    password.length > 0 &&
    (!totpRequired || normalizeCode(code) !== null) &&
    next.length >= MIN_PASSWORD &&
    matches

  async function submit() {
    const normalized = totpRequired ? normalizeCode(code) : ""
    if (!canSubmit || normalized === null) return
    setError(null)
    setBusy(true)
    try {
      await changePassword(repository, password, normalized, next)
      setPassword("")
      setCode("")
      setNext("")
      setConfirm("")
      toast.add({
        type: "success",
        title: "Password changed. Your tokens still work.",
      })
    } catch (err) {
      setError(describe(err))
      setCode("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Change password</CardTitle>
        <CardDescription>
          Changing the password does not revoke your tokens. Revoke them on the
          Tokens page if you need to.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <FieldGroup>
            {error && (
              <p role="alert" className="text-sm font-normal text-destructive">
                {error}
              </p>
            )}
            <Field>
              <FieldLabel htmlFor="pw-current">Current password</FieldLabel>
              <Input
                id="pw-current"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            {totpRequired && (
              <Field>
                <FieldLabel htmlFor="pw-code">Current code</FieldLabel>
                <Input
                  id="pw-code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={CODE_DIGITS + 2}
                  placeholder="123456"
                  className="data tracking-[0.25em]"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                />
              </Field>
            )}
            <Field>
              <FieldLabel htmlFor="pw-new">New password</FieldLabel>
              <Input
                id="pw-new"
                type="password"
                autoComplete="new-password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
              />
              <FieldDescription>
                At least {MIN_PASSWORD} characters.
              </FieldDescription>
            </Field>
            <Field data-invalid={(confirm.length > 0 && !matches) || undefined}>
              <FieldLabel htmlFor="pw-confirm">Confirm new password</FieldLabel>
              <Input
                id="pw-confirm"
                type="password"
                autoComplete="new-password"
                aria-invalid={confirm.length > 0 && !matches}
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
              />
              {confirm.length > 0 && !matches && (
                <FieldError
                  errors={[{ message: "The two passwords differ." }]}
                />
              )}
            </Field>
            <Field>
              <Button type="submit" disabled={!canSubmit || isBusy}>
                {isBusy && <Spinner />}
                Change password
              </Button>
            </Field>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}

/** What stands where the re-enrollment normally does on a substrate that
 * verifies no code. Replacing an authenticator here would be a ceremony with
 * nothing on the other end of it — and the seed a registration minted was
 * never shown to anybody, so the honest thing is to say who can put the factor
 * back and what it costs. */
function TotpOffCard() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Second factor: off</CardTitle>
        <CardDescription>
          This substrate verifies no code, so your password is all you need to
          sign in. It is a setting for local development.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">
          If you registered while it was off, nobody holds a secret for you. The
          operator issues one with{" "}
          <code className="data">substratectl user reset</code>.
        </p>
      </CardContent>
    </Card>
  )
}

function TotpCard({ repository }: { repository: string }) {
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [enrollment, setEnrollment] = useState<TOTPEnrollment | null>(null)
  const [newCode, setNewCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  const canBegin =
    repository !== "" && password.length > 0 && normalizeCode(code) !== null

  async function begin() {
    const normalized = normalizeCode(code)
    if (!canBegin || !normalized) return
    setError(null)
    setBusy(true)
    try {
      setEnrollment(await totpEnroll(repository, password, normalized))
    } catch (err) {
      setError(describe(err))
      setCode("")
    } finally {
      setBusy(false)
    }
  }

  async function finish() {
    const normalizedCurrent = normalizeCode(code)
    const normalizedNew = normalizeCode(newCode)
    if (!enrollment || !normalizedCurrent || !normalizedNew) return
    setError(null)
    setBusy(true)
    try {
      await totpChange(
        repository,
        password,
        normalizedCurrent,
        enrollment.totpSecret,
        normalizedNew
      )
      setEnrollment(null)
      setPassword("")
      setCode("")
      setNewCode("")
      toast.add({
        type: "success",
        title: "Authenticator replaced. The old secret no longer works.",
      })
    } catch (err) {
      setError(describe(err))
      setNewCode("")
    } finally {
      setBusy(false)
    }
  }

  function cancel() {
    setEnrollment(null)
    setNewCode("")
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Replace your authenticator</CardTitle>
        <CardDescription>
          Enter your current password and code, add the new secret, then enter a
          code from it. The old secret stops working straight away.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void (enrollment ? finish() : begin())
          }}
        >
          <FieldGroup>
            {error && (
              <p role="alert" className="text-sm font-normal text-destructive">
                {error}
              </p>
            )}
            <Field>
              <FieldLabel htmlFor="totp-current-pw">
                Current password
              </FieldLabel>
              <Input
                id="totp-current-pw"
                type="password"
                autoComplete="current-password"
                disabled={enrollment !== null}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="totp-current-code">Current code</FieldLabel>
              <Input
                id="totp-current-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={CODE_DIGITS + 2}
                placeholder="123456"
                className="data tracking-[0.25em]"
                disabled={enrollment !== null}
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            </Field>

            {enrollment && (
              <>
                <Separator />
                <div className="flex flex-col gap-2">
                  <p className="text-sm text-muted-foreground">
                    Add this to your authenticator with{" "}
                    <a
                      href={enrollment.otpauthUri}
                      className="underline underline-offset-4 hover:text-foreground"
                    >
                      the link
                    </a>
                    , or type the secret by hand.
                  </p>
                  <div className="flex items-center gap-2">
                    <code className="min-w-0 flex-1 truncate rounded-lg bg-muted px-2.5 py-1.5 data text-xs">
                      {enrollment.totpSecret}
                    </code>
                    <Button
                      type="button"
                      variant="outline"
                      size="icon-sm"
                      aria-label="Copy secret"
                      onClick={() => void copy(enrollment.totpSecret)}
                    >
                      <CopyIcon />
                    </Button>
                  </div>
                </div>
                <Field>
                  <FieldLabel htmlFor="totp-new-code">
                    Code from the new secret
                  </FieldLabel>
                  <Input
                    id="totp-new-code"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    maxLength={CODE_DIGITS + 2}
                    placeholder="123456"
                    className="data tracking-[0.25em]"
                    value={newCode}
                    onChange={(e) => setNewCode(e.target.value)}
                  />
                </Field>
              </>
            )}

            <div className="flex gap-2">
              <Button
                type="submit"
                disabled={
                  isBusy ||
                  (enrollment ? normalizeCode(newCode) === null : !canBegin)
                }
              >
                {isBusy && <Spinner />}
                {enrollment ? "Replace authenticator" : "Continue"}
              </Button>
              {enrollment && (
                <Button type="button" variant="ghost" onClick={cancel}>
                  Cancel
                </Button>
              )}
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}
