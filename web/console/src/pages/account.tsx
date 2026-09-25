/** The account, as Settings shows it: the repository name, and the two
 * credential changes, both under the password-factor rule.
 *
 * These endpoints do not accept a bearer token at all: the current password
 * AND code travel in the body, and a request that brings only the browser's
 * token is refused. That is the whole point: a leaked token's blast radius is
 * the data, never the account. So both forms ask for the password even though
 * you are plainly signed in.
 *
 * Live tokens SURVIVE a password change: a token is data access, the credential
 * is the account. Signing a browser or a script out is the Signed in section's
 * job. */

import { useState } from "react"
import { CopyIcon } from "lucide-react"

import { CopyButton } from "@/components/identity/copy-button"
import { SettingRow } from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
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

/** The account rows: the repository name, the password, the second factor. */
export function AccountRows() {
  const repository = getRepository() ?? ""
  const { totpRequired } = useAuthPolicy()
  const [open, setOpen] = useState<"password" | "totp" | null>(null)
  const toggle = (which: "password" | "totp") =>
    setOpen((o) => (o === which ? null : which))

  return (
    <>
      <SettingRow
        title={
          <span className="font-mono text-[13px]">
            {repository || "unknown"}
          </span>
        }
        description="The name you sign in with. It’s also the address of your data."
        control={
          repository ? (
            <CopyButton value={repository} label="Copy your repository name" />
          ) : undefined
        }
      />
      <SettingRow
        title="Password"
        description={`Changing it needs your current password${totpRequired ? " and a code" : ""}. Browsers and scripts you signed in stay signed in.`}
        control={
          <Button
            variant="outline"
            size="sm"
            aria-expanded={open === "password"}
            onClick={() => toggle("password")}
          >
            {open === "password" ? "Cancel" : "Change…"}
          </Button>
        }
      >
        {open === "password" && (
          <PasswordForm
            repository={repository}
            totpRequired={totpRequired}
            onDone={() => setOpen(null)}
          />
        )}
      </SettingRow>
      {totpRequired ? (
        <SettingRow
          title="Second factor"
          description="On. Signing in asks for a code from your authenticator app."
          control={
            <Button
              variant="outline"
              size="sm"
              aria-expanded={open === "totp"}
              onClick={() => toggle("totp")}
            >
              {open === "totp" ? "Cancel" : "Replace authenticator…"}
            </Button>
          }
        >
          {open === "totp" && (
            <TotpForm repository={repository} onDone={() => setOpen(null)} />
          )}
        </SettingRow>
      ) : (
        <SettingRow
          title="Second factor: off"
          description={
            <>
              This substrate asks for no code, so your password is all you need
              to sign in. It is a setting for local development. If you
              registered while it was off, nobody holds a secret for you; the
              operator issues one with{" "}
              <code className="font-mono">substratectl user reset</code>.
            </>
          }
        />
      )}
    </>
  )
}

function PasswordForm({
  repository,
  totpRequired,
  onDone,
}: {
  repository: string
  totpRequired: boolean
  onDone: () => void
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
      toast.add({
        type: "success",
        title: "Password changed. You stay signed in everywhere.",
      })
      onDone()
    } catch (err) {
      setError(describe(err))
      setCode("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      aria-label="Change your password"
      className="max-w-sm"
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
              className="font-mono tracking-[0.25em]"
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
            <FieldError errors={[{ message: "The two passwords differ." }]} />
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
  )
}

function TotpForm({
  repository,
  onDone,
}: {
  repository: string
  onDone: () => void
}) {
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
      toast.add({
        type: "success",
        title: "Authenticator replaced. The old secret no longer works.",
      })
      onDone()
    } catch (err) {
      setError(describe(err))
      setNewCode("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      aria-label="Replace your authenticator"
      className="max-w-sm"
      onSubmit={(e) => {
        e.preventDefault()
        void (enrollment ? finish() : begin())
      }}
    >
      <FieldGroup>
        <p className="text-[12.5px] text-muted-foreground">
          Enter your current password and code, add the new secret to your app,
          then enter a code from it. The old secret stops working straight away.
        </p>
        {error && (
          <p role="alert" className="text-sm font-normal text-destructive">
            {error}
          </p>
        )}
        <Field>
          <FieldLabel htmlFor="totp-current-pw">Current password</FieldLabel>
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
            className="font-mono tracking-[0.25em]"
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
                <code className="min-w-0 flex-1 truncate rounded-lg bg-muted px-2.5 py-1.5 font-mono text-xs">
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
                className="font-mono tracking-[0.25em]"
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
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setEnrollment(null)
                setNewCode("")
              }}
            >
              Start over
            </Button>
          )}
        </div>
      </FieldGroup>
    </form>
  )
}
