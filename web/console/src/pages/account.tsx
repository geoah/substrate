/** The account, as Settings shows it: the repository name, the person's
 * sign-in name, the two credential changes and the delete, all under the
 * password-factor rule.
 *
 * These endpoints do not accept a bearer token at all: the current password,
 * and the code where the account has a second factor, travel in the body, and
 * a request that brings only the browser's token is refused. That is the whole point: a leaked token's blast radius is
 * the data, never the account. So both forms ask for the password even though
 * you are plainly signed in.
 *
 * Live tokens SURVIVE a password change unless it also signs out everywhere:
 * a token is data access, the credential is the account. Signing one browser
 * or script out is the Signed in section's job, and signing out everywhere
 * (every token, this browser's included, decision 0272) is its last row and
 * the password change's option, offered where the server lists `auth` at
 * revision 2. */

import { useState, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
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
  identityQueryOptions,
  normalizeCode,
  renameRepository,
  signOutEverywhere,
  totpChange,
  totpEnroll,
} from "@/lib/api/auth"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  discoveryQueryOptions,
  doorNamesAPerson,
  hasFeature,
  useAuthPolicy,
} from "@/lib/api/discovery"
import { recordQueryOptions } from "@/lib/api/records"
import {
  clearSession,
  getIdentityName,
  getRepository,
  setRepository,
} from "@/lib/api/session"
import { ApiError, type TOTPEnrollment } from "@/lib/api/types"

const MIN_PASSWORD = 8

/** The `auth` revision that added signing out everywhere: the route, and the
 * password change's option. */
const SIGN_OUT_EVERYWHERE_REVISION = 2

/** The `auth` revision that added renaming the repository (decision 0287). */
const RENAME_REVISION = 5

/** Whether the server can rename the repository. A console bundled in an app
 * may talk to an older one. */
function useRenameOffered(): boolean {
  const discovery = useQuery(discoveryQueryOptions)
  return hasFeature(discovery.data, "auth", RENAME_REVISION)
}

/** Whether the server can sign the person out everywhere. A console bundled
 * in an app may talk to an older one. */
function useSignOutEverywhereOffered(): boolean {
  const discovery = useQuery(discoveryQueryOptions)
  return hasFeature(discovery.data, "auth", SIGN_OUT_EVERYWHERE_REVISION)
}

/** After a sign out everywhere this browser's token is gone too: forget it
 * and go to the sign-in page. */
function useSignedOutEverywhere(): (ended: number | undefined) => void {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  return (ended) => {
    clearSession()
    queryClient.clear()
    toast.add({
      type: "success",
      title:
        ended === undefined
          ? "Signed out everywhere."
          : `Signed out everywhere: ${ended} ${ended === 1 ? "sign-in" : "sign-ins"} ended.`,
    })
    void navigate({ to: "/login", replace: true })
  }
}

/** A server from before identities (decision 0274) keeps the sign-in as the
 * repository's credential record, whose TOTP ref, redacted on every read, is
 * there exactly while the account has a second factor (decision 0237). */
const credentialQuery = recordQueryOptions(
  "substrate.reamde.dev",
  "core",
  "credential",
  "self"
)

/** The name the person signs in with: their identity's, where the server
 * reads it, else what the session kept. An app's sign-in keeps no name, so
 * the read is what an app shell has. */
function useIdentityName(): string {
  const person = useQuery({
    ...identityQueryOptions,
    enabled: doorNamesAPerson(),
  })
  return person.data?.name ?? getIdentityName() ?? ""
}

/** Whether signing in and the credential changes owe a code: the door verifies
 * one and the person has a factor, which their identity says, or on a server
 * from before identities, their repository's credential record. Until the
 * read answers, the strict answer stands, so a code field is never hidden
 * from an account that owes one. */
function useCodeOwed(): boolean {
  // The policy's read is also what doorNamesAPerson follows, and its answer
  // renders this again.
  const { totpRequired } = useAuthPolicy()
  const personDoor = doorNamesAPerson()
  const person = useQuery({ ...identityQueryOptions, enabled: personDoor })
  const credential = useQuery({ ...credentialQuery, enabled: !personDoor })
  const secondFactor = personDoor
    ? person.data?.secondFactor
    : credential.data && credential.data.properties.totpRef !== undefined
  return totpRequired && (secondFactor ?? true)
}

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "forbidden") {
      return "Being signed in is not enough. Fill in your current password and code."
    }
    if (err.code === "totp_required") {
      return "Your Substrate asks for a code too. Reload the page to enter one."
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
 * Verify domain there, beside the mark it clears), the name the person signs
 * in with, the password and the second factor. */
export function AccountRows({ afterName }: { afterName?: ReactNode } = {}) {
  const [repository, setName] = useState(getRepository() ?? "")
  const identity = useIdentityName()
  const { totpRequired } = useAuthPolicy()
  const codeOwed = useCodeOwed()
  const renameOffered = useRenameOffered()
  const [technical] = useTechnicalDetails()
  const [open, setOpen] = useState<"password" | "totp" | "rename" | null>(null)
  const toggle = (which: "password" | "totp" | "rename") =>
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
        description={
          renameOffered
            ? "Your Substrate’s address: the home of your data and of the kinds you declare. Renaming keeps the old name working for 90 days."
            : "Your Substrate’s address: the home of your data and of the kinds you declare."
        }
        control={
          repository ? (
            <span className="inline-flex items-center gap-2">
              <CopyButton
                value={repository}
                label="Copy your repository name"
              />
              {renameOffered && (
                <Button
                  variant="outline"
                  size="sm"
                  aria-expanded={open === "rename"}
                  onClick={() => toggle("rename")}
                >
                  {open === "rename" ? "Cancel" : "Rename…"}
                </Button>
              )}
            </span>
          ) : undefined
        }
      >
        {open === "rename" && (
          <RenameRepositoryForm
            identity={identity}
            repository={repository}
            codeOwed={codeOwed}
            onDone={(name) => {
              setName(name)
              setOpen(null)
            }}
          />
        )}
      </SettingRow>
      {afterName}
      <SettingRow
        title={
          <span className="[overflow-wrap:anywhere]">
            {identity || "unknown"}
          </span>
        }
        description="The name you sign in with."
        control={
          identity ? (
            <CopyButton
              value={identity}
              label="Copy the name you sign in with"
            />
          ) : undefined
        }
      />
      <SettingRow
        title="Password"
        description={`Changing it needs your current password${codeOwed ? " and a code" : ""}. Browsers, scripts and apps stay signed in unless you also sign out everywhere.`}
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
            identity={identity}
            codeOwed={codeOwed}
            onDone={() => setOpen(null)}
          />
        )}
      </SettingRow>
      {totpRequired ? (
        <SettingRow
          title="Second factor"
          description={
            codeOwed
              ? "On. Signing in asks for a code from your authenticator app."
              : "Off. Signing in asks only for your password. Turn it on to also ask for a code from an authenticator app."
          }
          control={
            <Button
              variant="outline"
              size="sm"
              aria-expanded={open === "totp"}
              onClick={() => toggle("totp")}
            >
              {open === "totp"
                ? "Cancel"
                : codeOwed
                  ? "Replace authenticator…"
                  : "Turn on…"}
            </Button>
          }
        >
          {open === "totp" && (
            <TotpForm
              identity={identity}
              enabling={!codeOwed}
              onDone={() => setOpen(null)}
            />
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
                  It is a setting for local development. An account registered
                  while it was off has no second factor, and turns one on here
                  once the substrate verifies codes again.
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
  const identity = useIdentityName()
  const codeOwed = useCodeOwed()
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
          identity={identity}
          repository={repository}
          codeOwed={codeOwed}
        />
      )}
    </SettingRow>
  )
}

/** Settings' last Signed in row: one action that ends every token the
 * person holds, this browser's included. Offered where the server lists
 * `auth` at revision 2. */
export function SignOutEverywhereRow() {
  const identity = useIdentityName()
  const codeOwed = useCodeOwed()
  const offered = useSignOutEverywhereOffered()
  const [open, setOpen] = useState(false)
  if (!offered) return null
  return (
    <SettingRow
      title="Sign out everywhere"
      description="Ends every sign-in, every token you made, and every app’s and assistant’s access, this browser included. Use it if a sign-in may have leaked."
      control={
        <Button
          variant="outline"
          size="sm"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          {open ? "Cancel" : "Sign out everywhere…"}
        </Button>
      }
    >
      {open && (
        <SignOutEverywhereForm identity={identity} codeOwed={codeOwed} />
      )}
    </SettingRow>
  )
}

function SignOutEverywhereForm({
  identity,
  codeOwed,
}: {
  identity: string
  codeOwed: boolean
}) {
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)
  const signedOut = useSignedOutEverywhere()

  const canSubmit =
    identity !== "" &&
    password.length > 0 &&
    (!codeOwed || normalizeCode(code) !== null)

  async function submit() {
    const normalized = codeOwed ? normalizeCode(code) : ""
    if (!canSubmit || normalized === null) return
    setError(null)
    setBusy(true)
    try {
      const answer = await signOutEverywhere(identity, password, normalized)
      signedOut(answer.signedOut)
    } catch (err) {
      setError(describe(err))
      setCode("")
      setBusy(false)
    }
  }

  return (
    <form
      aria-label="Sign out everywhere"
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
          <FieldLabel htmlFor="out-password">Current password</FieldLabel>
          <Input
            id="out-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        {codeOwed && (
          <Field>
            <FieldLabel htmlFor="out-code">Current code</FieldLabel>
            <Input
              id="out-code"
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
          <Button type="submit" disabled={!canSubmit || isBusy}>
            {isBusy && <Spinner />}
            Sign out everywhere
          </Button>
        </Field>
      </FieldGroup>
    </form>
  )
}

/** The rename: the new name, completed under the server's name host as at
 * registration, and the person's current factors; only an admin of the
 * repository renames it, which the server says to anyone else. The old name
 * keeps resolving for 90 days, which the toast says, and this browser's token
 * and session stay: a rename moves neither the token nor the data, only the
 * name. */
function RenameRepositoryForm({
  identity,
  repository,
  codeOwed,
  onDone,
}: {
  identity: string
  repository: string
  codeOwed: boolean
  onDone: (name: string) => void
}) {
  const queryClient = useQueryClient()
  const { nameHost } = useAuthPolicy()
  const [name, setName] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  const typed = name.trim().toLowerCase().replace(/\.$/, "")
  const preview =
    typed && !typed.includes(".") && nameHost ? `${typed}.${nameHost}` : typed
  const canSubmit =
    identity !== "" &&
    typed.length > 0 &&
    preview !== repository &&
    password.length > 0 &&
    (!codeOwed || normalizeCode(code) !== null)

  async function submit() {
    const normalized = codeOwed ? normalizeCode(code) : ""
    if (!canSubmit || normalized === null) return
    setError(null)
    setBusy(true)
    let renamed
    try {
      renamed = await renameRepository(
        identity,
        repository,
        password,
        normalized,
        typed
      )
    } catch (err) {
      setError(describe(err))
      setCode("")
      setBusy(false)
      return
    }
    setRepository(renamed.name)
    await queryClient.invalidateQueries()
    const held = renamed.formerNames.find((f) => f.name === repository)
    toast.add({
      type: "success",
      title: `Renamed to ${renamed.name}.`,
      description: held
        ? `${repository} keeps working until ${new Date(held.until).toLocaleDateString()}.`
        : undefined,
    })
    setBusy(false)
    onDone(renamed.name)
  }

  return (
    <form
      aria-label="Rename your Substrate"
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
          <FieldLabel htmlFor="rename-name">New name</FieldLabel>
          <Input
            id="rename-name"
            autoComplete="off"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            placeholder={nameHost ? `you.${nameHost}` : "you.example.com"}
            className="font-mono"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <FieldDescription>
            {preview && preview !== typed ? (
              <>
                Your name becomes{" "}
                <span className="font-mono [overflow-wrap:anywhere] text-foreground">
                  {preview}
                </span>
                .{" "}
              </>
            ) : null}
            {repository} keeps working for 90 days, then anyone may register it.
          </FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="rename-password">Current password</FieldLabel>
          <Input
            id="rename-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        {codeOwed && (
          <Field>
            <FieldLabel htmlFor="rename-code">Current code</FieldLabel>
            <Input
              id="rename-code"
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
          <Button type="submit" disabled={!canSubmit || isBusy}>
            {isBusy && <Spinner />}
            Rename
          </Button>
        </Field>
      </FieldGroup>
    </form>
  )
}

function DeleteRepositoryForm({
  identity,
  repository,
  codeOwed,
}: {
  identity: string
  repository: string
  codeOwed: boolean
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
    (!codeOwed || normalizeCode(code) !== null)

  async function submit() {
    const normalized = codeOwed ? normalizeCode(code) : ""
    if (!canSubmit || normalized === null) return
    setError(null)
    setBusy(true)
    try {
      await deleteRepository(identity, repository, password, normalized)
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
        {codeOwed && (
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
  identity,
  codeOwed,
  onDone,
}: {
  identity: string
  codeOwed: boolean
  onDone: () => void
}) {
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [everywhere, setEverywhere] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)
  const offered = useSignOutEverywhereOffered()
  const signedOut = useSignedOutEverywhere()

  const matches = next.length > 0 && next === confirm
  const canSubmit =
    identity !== "" &&
    password.length > 0 &&
    (!codeOwed || normalizeCode(code) !== null) &&
    next.length >= MIN_PASSWORD &&
    matches

  async function submit() {
    const normalized = codeOwed ? normalizeCode(code) : ""
    if (!canSubmit || normalized === null) return
    setError(null)
    setBusy(true)
    try {
      const answer = await changePassword(
        identity,
        password,
        normalized,
        next,
        offered && everywhere
      )
      if (offered && everywhere) {
        signedOut(answer.signedOut)
        return
      }
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
        {codeOwed && (
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
        {offered && (
          <Field>
            <div className="flex items-start gap-2">
              <input
                id="pw-everywhere"
                type="checkbox"
                checked={everywhere}
                onChange={(e) => setEverywhere(e.target.checked)}
                className="mt-0.5 size-4 accent-primary"
              />
              <div className="flex flex-col gap-0.5">
                <FieldLabel htmlFor="pw-everywhere">
                  Also sign out everywhere
                </FieldLabel>
                <FieldDescription>
                  Ends every sign-in, every token you made, and every app’s and
                  assistant’s access, this browser included. You sign in again
                  with the new password.
                </FieldDescription>
              </div>
            </div>
          </Field>
        )}
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

/** Replaces the authenticator, or with `enabling`, enrolls the first one of an
 * account registered without: the same two calls, with no current code to
 * prove where there is none (decision 0237). */
function TotpForm({
  identity,
  enabling,
  onDone,
}: {
  identity: string
  enabling: boolean
  onDone: () => void
}) {
  const queryClient = useQueryClient()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [enrollment, setEnrollment] = useState<TOTPEnrollment | null>(null)
  const [newCode, setNewCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  const canBegin =
    identity !== "" &&
    password.length > 0 &&
    (enabling || normalizeCode(code) !== null)

  async function begin() {
    const normalized = enabling ? "" : normalizeCode(code)
    if (!canBegin || normalized === null) return
    setError(null)
    setBusy(true)
    try {
      setEnrollment(await totpEnroll(identity, password, normalized))
    } catch (err) {
      setError(describe(err))
      setCode("")
    } finally {
      setBusy(false)
    }
  }

  async function finish() {
    const normalizedCurrent = enabling ? "" : normalizeCode(code)
    const normalizedNew = normalizeCode(newCode)
    if (!enrollment || normalizedCurrent === null || !normalizedNew) return
    setError(null)
    setBusy(true)
    try {
      await totpChange(
        identity,
        password,
        normalizedCurrent,
        enrollment.totpSecret,
        normalizedNew
      )
      toast.add({
        type: "success",
        title: enabling
          ? "Two-factor is on. Signing in now asks for a code."
          : "Authenticator replaced. The old secret no longer works.",
      })
      void queryClient.invalidateQueries({ queryKey: credentialQuery.queryKey })
      void queryClient.invalidateQueries({
        queryKey: identityQueryOptions.queryKey,
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
      aria-label={
        enabling ? "Turn on two-factor" : "Replace your authenticator"
      }
      className="max-w-sm"
      onSubmit={(e) => {
        e.preventDefault()
        void (enrollment ? finish() : begin())
      }}
    >
      <FieldGroup>
        <p className="text-[12.5px] text-muted-foreground">
          {enabling
            ? "Enter your password, add the secret to your authenticator app, then enter a code from it. From then on, signing in asks for a code."
            : "Enter your current password and code, add the new secret to your app, then enter a code from it. The old secret stops working straight away."}
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
        {!enabling && (
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
        )}

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
                {enabling ? "Code from your app" : "Code from the new secret"}
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
            {!enrollment
              ? "Continue"
              : enabling
                ? "Turn on two-factor"
                : "Replace authenticator"}
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
