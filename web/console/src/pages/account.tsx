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
import { getUsername } from "@/lib/api/session"
import { ApiError, type TOTPEnrollment } from "@/lib/api/types"

const MIN_PASSWORD = 12

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "forbidden") {
      return "This endpoint does not accept a token — fill in your current password and code."
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
    toast.add({ type: "error", title: "Could not reach the clipboard — select and copy." })
  }
}

export function AccountPage() {
  const username = getUsername() ?? ""

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-end justify-between gap-3 px-6 pt-5 pb-2">
        <div>
          <h1 className="text-lg font-semibold">Account</h1>
          <p className="text-xs text-muted-foreground">
            Your credential. Both changes below need your current password and
            code in the request — a signed-in browser is not enough, by design.
          </p>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="flex flex-col gap-6 px-6 py-4">
          <Card>
            <CardHeader>
              <CardTitle>You</CardTitle>
              <CardDescription>
                One repository, implied by your token — there is nothing to
                pick. Signed-in browsers and scripts are all token records; see{" "}
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
                <span className="text-muted-foreground">Username</span>
                <span className="data">{username || "unknown"}</span>
              </div>
            </CardContent>
          </Card>

          <PasswordCard username={username} />
          <TotpCard username={username} />
        </div>
      </div>
    </div>
  )
}

function PasswordCard({ username }: { username: string }) {
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  const matches = next.length > 0 && next === confirm
  const canSubmit =
    username !== "" &&
    password.length > 0 &&
    normalizeCode(code) !== null &&
    next.length >= MIN_PASSWORD &&
    matches

  async function submit() {
    const normalized = normalizeCode(code)
    if (!canSubmit || !normalized) return
    setError(null)
    setBusy(true)
    try {
      await changePassword(username, password, normalized, next)
      setPassword("")
      setCode("")
      setNext("")
      setConfirm("")
      toast.add({ type: "success", title: "Password changed. Your tokens still work." })
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
          Your live tokens keep working: a token is data access, the credential
          is the account.
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

function TotpCard({ username }: { username: string }) {
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [enrollment, setEnrollment] = useState<TOTPEnrollment | null>(null)
  const [newCode, setNewCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  const canBegin =
    username !== "" && password.length > 0 && normalizeCode(code) !== null

  async function begin() {
    const normalized = normalizeCode(code)
    if (!canBegin || !normalized) return
    setError(null)
    setBusy(true)
    try {
      setEnrollment(await totpEnroll(username, password, normalized))
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
        username,
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
          Prove the current factors, add the new secret, then prove that. The
          old secret stops working the moment the new one lands.
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
                    Add this to your authenticator —{" "}
                    <a
                      href={enrollment.otpauthUri}
                      className="underline underline-offset-4 hover:text-foreground"
                    >
                      the link
                    </a>
                    , or the secret by hand:
                  </p>
                  <div className="flex items-center gap-2">
                    <code className="data min-w-0 flex-1 truncate rounded-lg bg-muted px-2.5 py-1.5 text-xs">
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
                    Code from the NEW secret
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
                  (enrollment
                    ? normalizeCode(newCode) === null
                    : !canBegin)
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
