/** The account, as Settings shows it: the repository name, the two
 * credential changes and the delete, all under the password-factor rule.
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

import { useState, type ReactNode } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { CopyIcon } from "lucide-react"

import { CopyButton } from "@/components/identity/copy-button"
import { UnverifiedMark } from "@/components/identity/unverified-mark"
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
  deleteRepository,
  normalizeCode,
  totpChange,
  totpEnroll,
} from "@/lib/api/auth"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useAuthPolicy } from "@/lib/api/discovery"
import { clearSession, getRepository } from "@/lib/api/session"
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

/** The account rows: the repository name, then `afterName` (Settings puts
 * Verify domain there, beside the mark it clears), the password and the
 * second factor. */
export function AccountRows({ afterName }: { afterName?: ReactNode } = {}) {
  const repository = getRepository() ?? ""
  const { totpRequired } = useAuthPolicy()
  const [technical] = useTechnicalDetails()
  const [open, setOpen] = useState<"password" | "totp" | null>(null)
  const toggle = (which: "password" | "totp") =>
    setOpen((o) => (o === which ? null : which))

  return (
    <>
      <SettingRow
        title={
          <span className="inline-flex flex-wrap items-center gap-2">
            <span className="[overflow-wrap:anywhere]">
              {repository || "unknown"}
            </span>
            <UnverifiedMark />
          </span>
        }
        description="The name you sign in with. It’s also the address of your data."
        control={
          repository ? (
            <CopyButton value={repository} label="Copy your repository name" />
          ) : undefined
        }
      />
      {afterName}
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
          title="Second factor"
          description={
            <>
              Off. This substrate signs you in with a password alone.
              {technical && (
                <>
                  {" "}
                  It is a setting for local development. If you registered while
                  it was off, nobody holds a secret for you; the operator issues
                  one with{" "}
                  <code className="font-mono">substratectl user reset</code>.
                </>
              )}
            </>
          }
        />
      )}
    </>
  )
}

/** The danger zone's one row: deleting the repository for good. */
export function DeleteRepositoryRow() {
  const repository = getRepository() ?? ""
  const { totpRequired } = useAuthPolicy()
  const [open, setOpen] = useState(false)
  return (
    <SettingRow
      title="Delete your Substrate"
      description="Erases every record, token and file in it, and signs out everything signed in to it. The name is freed, so anyone can register it again. This can’t be undone."
      control={
        <Button
          variant={open ? "outline" : "destructive"}
          size="sm"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          {open ? "Cancel" : "Delete…"}
        </Button>
      }
    >
      {open && (
        <DeleteRepositoryForm
          repository={repository}
          totpRequired={totpRequired}
        />
      )}
    </SettingRow>
  )
}

function DeleteRepositoryForm({
  repository,
  totpRequired,
}: {
  repository: string
  totpRequired: boolean
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [typed, setTyped] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  const confirmed = repository !== "" && typed.trim() === repository
  const canSubmit =
    confirmed &&
    password.length > 0 &&
    (!totpRequired || normalizeCode(code) !== null)

  async function submit() {
    const normalized = totpRequired ? normalizeCode(code) : ""
    if (!canSubmit || normalized === null) return
    setError(null)
    setBusy(true)
    try {
      await deleteRepository(repository, password, normalized)
    } catch (err) {
      setError(describe(err))
      setCode("")
      setBusy(false)
      return
    }
    // Every token went with the repository, so this browser's session is
    // already dead; nothing is revoked, only forgotten.
    clearSession()
    queryClient.clear()
    toast.add({ type: "success", title: `Deleted ${repository}.` })
    void navigate({ to: "/register", replace: true })
  }

  return (
    <form
      aria-label="Delete your Substrate"
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
        <Field data-invalid={(typed.length > 0 && !confirmed) || undefined}>
          <FieldLabel htmlFor="delete-name">Confirm the name</FieldLabel>
          <Input
            id="delete-name"
            autoComplete="off"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            placeholder={repository}
            className="font-mono"
            aria-invalid={typed.length > 0 && !confirmed}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
          />
          <FieldDescription>
            Type{" "}
            <span className="font-mono [overflow-wrap:anywhere] text-foreground">
              {repository}
            </span>{" "}
            exactly.
          </FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="delete-password">Current password</FieldLabel>
          <Input
            id="delete-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        {totpRequired && (
          <Field>
            <FieldLabel htmlFor="delete-code">Current code</FieldLabel>
            <Input
              id="delete-code"
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
          <Button
            type="submit"
            variant="destructive"
            disabled={!canSubmit || isBusy}
          >
            {isBusy && <Spinner />}
            Delete forever
          </Button>
        </Field>
      </FieldGroup>
    </form>
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
