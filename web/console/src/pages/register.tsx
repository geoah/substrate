import { useState } from "react"
import { Link, useNavigate } from "@tanstack/react-router"
import { BoxesIcon, CheckIcon, CopyIcon } from "lucide-react"
import { QRCodeCanvas } from "qrcode.react"

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
import {
  CODE_DIGITS,
  normalizeCode,
  register,
  registerEnroll,
} from "@/lib/api/auth"
import { useAuthPolicy } from "@/lib/api/discovery"
import { saveSession } from "@/lib/api/session"
import { ApiError, type TOTPEnrollment } from "@/lib/api/types"

/** Long enough that argon2id is not the only thing between a guess and the
 * repository; the server has no opinion beyond non-empty. */
const MIN_PASSWORD = 12

/** A secret shown for the reader to carry off: mono, selectable, with a copy
 * affordance. Offered as text beside the QR so a manager or a phone that reads
 * neither the code nor the camera can still be handed the value directly. */
function CopyBlock({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)
  async function copy() {
    if (!navigator.clipboard?.writeText) return
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unreachable — the value is selectable by hand */
    }
  }
  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 truncate rounded-lg bg-muted px-2.5 py-1.5 data text-xs">
        {value}
      </code>
      <Button
        type="button"
        variant="outline"
        size="icon-sm"
        aria-label={`Copy ${label}`}
        onClick={copy}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </Button>
    </div>
  )
}

/** A key shown once for the reader to carry off: a selectable field with the
 * copy affordance beside it. A named input rather than a display block, so a
 * password manager that captures submitted fields has something to take. */
function KeepKeyField({
  id,
  label,
  value,
}: {
  id: string
  label: string
  value: string
}) {
  const [copied, setCopied] = useState(false)
  async function copy() {
    if (!navigator.clipboard?.writeText) return
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unreachable — the value is selectable by hand */
    }
  }
  return (
    <div className="flex items-center gap-2">
      <Input
        id={id}
        name={id}
        className="min-w-0 flex-1 data"
        readOnly
        autoComplete="off"
        value={value}
        onFocus={(e) => e.currentTarget.select()}
      />
      <Button
        type="button"
        variant="outline"
        size="icon-sm"
        aria-label={`Copy ${label}`}
        onClick={copy}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </Button>
    </div>
  )
}

/** The authority a registration gets when the reader leaves the field alone:
 * the username as a label under the host serving this console, the way a
 * handle sits under its server. Mirrors the server's own default so the form
 * shows the name the repository will actually get. */
function defaultAuthority(username: string, host: string): string {
  const h = host.replace(/\.$/, "").toLowerCase()
  if (!username || !h) return ""
  return `${username}.${h}`
}

/** Registration, in two steps.
 *
 * Step one collects the invite code, username AND password, then buys a TOTP
 * seed — it writes NOTHING (the browser holds the seed). Password before the
 * QR is deliberate: a password manager sees a new-login form submit and offers
 * to SAVE it first, so when step two reveals the authenticator QR the manager
 * attaches the one-time password to the login it just created rather than to
 * some existing item. Step two proves one code and commits the whole user in
 * one transaction — the repository, the sealed material, the credential and the
 * first token — ending LOGGED IN, because the response carries that token.
 *
 * Where the substrate verifies no second factor there IS no step two: the one
 * form commits, and the seed is minted server-side and enrolled nowhere. */
export function RegisterPage() {
  const { totpRequired } = useAuthPolicy()
  const navigate = useNavigate()
  const [inviteCode, setInviteCode] = useState("")
  const [username, setUsername] = useState("")
  // The authority is DERIVED from the username under this console's host
  // until the reader edits it, at which point their spelling is what is sent.
  // Sending the derived value rather than leaving it to the server keeps what
  // the form shows and what the repository gets one string.
  const [authorityEdit, setAuthorityEdit] = useState<string | null>(null)
  const authority =
    authorityEdit ?? defaultAuthority(username.trim(), window.location.hostname)
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [code, setCode] = useState("")
  const [recoveryKey, setRecoveryKey] = useState<string | null>(null)
  const [enrolled, setEnrolled] = useState<TOTPEnrollment | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  function fail(err: unknown) {
    if (err instanceof ApiError) {
      if (err.code === "unsupported") {
        setError(
          "Registration is closed — this substrate has no invite code configured, so it admits nobody. The operator sets one on the box."
        )
        return
      }
      if (err.code === "rate_limited") {
        setError(
          `Too many attempts — the door is rate limited on purpose. Try again in ${err.retryAfter ?? 5}s.`
        )
        return
      }
      setError(err.message)
      return
    }
    setError("Something went wrong. Try again.")
  }

  const passwordsMatch = password.length > 0 && password === confirm
  const canEnroll =
    inviteCode.trim().length > 0 &&
    username.trim().length > 0 &&
    authority.trim().length > 0 &&
    password.length >= MIN_PASSWORD &&
    passwordsMatch

  // DERIVED, not stored: a registration submitted before discovery answered
  // runs under the strict default and buys an enrollment (which writes
  // nothing). If the answer then says this substrate verifies no code, that
  // enrollment is a seed with no door behind it, and the second step it would
  // open is a ceremony over a secret nothing will check.
  const enrollment = totpRequired ? enrolled : null

  /** Step one. With no second factor to enroll it IS the whole registration:
   * buying a seed nobody will ever be shown would be theater. */
  async function beginEnrollment() {
    if (!canEnroll) return
    if (!totpRequired) {
      await finish()
      return
    }
    setError(null)
    setBusy(true)
    try {
      setEnrolled(await registerEnroll(inviteCode.trim(), username.trim()))
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const canFinish = totpRequired
    ? enrollment !== null && normalizeCode(code) !== null
    : canEnroll

  async function finish() {
    // An absent seed asks the substrate to mint one, which it only does where
    // it verifies no code — so the enrolled-nowhere seed is never a surprise.
    const normalized = totpRequired ? normalizeCode(code) : ""
    if (!canFinish || normalized === null) return
    setError(null)
    setBusy(true)
    try {
      const name = username.trim()
      const minted = await register({
        inviteCode: inviteCode.trim(),
        username: name,
        password,
        totpSecret: enrollment?.totpSecret ?? "",
        totpCode: normalized,
        label: "console",
        authority: authority.trim(),
      })
      // The recovery identity arrives ONCE, on this response, and no later
      // call can produce it: it lands in state BEFORE the fallible session
      // write, so a blocked localStorage cannot take the only showing with it,
      // and the page holds the reader until they carry it off instead of
      // navigating past it.
      const holdKeys = Boolean(minted.recoveryKey)
      if (holdKeys) {
        setRecoveryKey(minted.recoveryKey ?? null)
      }
      saveSession(minted.secret, name, minted.token.id)
      if (holdKeys) return
      await navigate({ to: "/", replace: true })
    } catch (err) {
      if (err instanceof ApiError && err.code === "auth") {
        setError(
          totpRequired
            ? "The invite code or the 6-digit code is wrong. Check the code your authenticator shows right now and try again."
            : "The invite code is wrong."
        )
        setCode("")
      } else {
        fail(err)
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-6">
      <div className="flex w-full max-w-md flex-col gap-6">
        <div className="flex items-center justify-center gap-2.5">
          <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <BoxesIcon className="size-4" />
          </div>
          <span className="text-lg font-semibold">substrate</span>
        </div>
        {recoveryKey !== null && (
          <Card>
            <CardHeader>
              <CardTitle>Your recovery key</CardTitle>
              <CardDescription>
                The recovery key is shown once and never shown again. Put it in
                your password manager now.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <form
                onSubmit={(e) => {
                  e.preventDefault()
                  void navigate({ to: "/", replace: true })
                }}
              >
                <FieldGroup>
                  {recoveryKey !== null && (
                    <Field>
                      <FieldLabel htmlFor="recovery-key">
                        Recovery key
                      </FieldLabel>
                      <KeepKeyField
                        id="recovery-key"
                        label="recovery key"
                        value={recoveryKey}
                      />
                      <FieldDescription>
                        Never stored by the substrate. With it, a backup of your
                        repository is recoverable on any substrate; without it,
                        only this server&rsquo;s own key can read your secrets.
                      </FieldDescription>
                    </Field>
                  )}
                  <Field>
                    <Button type="submit">I saved it — continue</Button>
                  </Field>
                </FieldGroup>
              </form>
            </CardContent>
          </Card>
        )}
        {recoveryKey === null && (
          <Card>
            <CardHeader>
              <CardTitle>Register</CardTitle>
              <CardDescription>
                An invite code creates your user and your repository, seeded
                with the shipped kinds.{" "}
                {totpRequired
                  ? `All three factors are required: username, password and a ${CODE_DIGITS}-digit code.`
                  : "This substrate does not verify a second factor, so there is no authenticator to enroll: a username and a password make the user."}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-6">
              {error && (
                <p
                  role="alert"
                  className="text-sm font-normal text-destructive"
                >
                  {error}
                </p>
              )}

              <form
                onSubmit={(e) => {
                  e.preventDefault()
                  void beginEnrollment()
                }}
              >
                <FieldGroup>
                  <Field>
                    <FieldLabel htmlFor="inviteCode">Invite code</FieldLabel>
                    <Input
                      id="inviteCode"
                      className="data"
                      value={inviteCode}
                      onChange={(e) => setInviteCode(e.target.value)}
                      disabled={enrollment !== null}
                      autoFocus
                    />
                    <FieldDescription>
                      The code the operator configured on this substrate.
                    </FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="username">Username</FieldLabel>
                    <Input
                      id="username"
                      autoComplete="username"
                      value={username}
                      onChange={(e) =>
                        setUsername(e.target.value.toLowerCase())
                      }
                      disabled={enrollment !== null}
                    />
                    <FieldDescription>
                      Lowercase letters and digits. It cannot be changed later.
                    </FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="authority">Authority</FieldLabel>
                    <Input
                      id="authority"
                      className="data"
                      autoComplete="off"
                      spellCheck={false}
                      value={authority}
                      onChange={(e) =>
                        setAuthorityEdit(e.target.value.toLowerCase())
                      }
                      disabled={enrollment !== null}
                    />
                    <FieldDescription>
                      Any hostname you control, such as ada.example.com: it is
                      the name your repository owns and every kind you declare
                      lives under. Left alone, it is your username under this
                      host. It cannot be changed later.
                    </FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="password">Password</FieldLabel>
                    <Input
                      id="password"
                      type="password"
                      autoComplete="new-password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      disabled={enrollment !== null}
                    />
                    <FieldDescription>
                      At least {MIN_PASSWORD} characters. There is no self-serve
                      recovery: lose both factors and only the operator can
                      reset you.
                    </FieldDescription>
                  </Field>
                  <Field
                    data-invalid={
                      (confirm.length > 0 && !passwordsMatch) || undefined
                    }
                  >
                    <FieldLabel htmlFor="confirm">Confirm password</FieldLabel>
                    <Input
                      id="confirm"
                      type="password"
                      autoComplete="new-password"
                      aria-invalid={confirm.length > 0 && !passwordsMatch}
                      value={confirm}
                      onChange={(e) => setConfirm(e.target.value)}
                      disabled={enrollment !== null}
                    />
                    {confirm.length > 0 && !passwordsMatch && (
                      <FieldError
                        errors={[{ message: "The two passwords differ." }]}
                      />
                    )}
                  </Field>
                  {!enrollment && (
                    <Field>
                      <Button type="submit" disabled={!canEnroll || isBusy}>
                        {isBusy && <Spinner />}
                        {totpRequired ? "Continue" : "Create my repository"}
                      </Button>
                    </Field>
                  )}
                </FieldGroup>
              </form>

              {enrollment && (
                <>
                  <Separator />
                  <div className="flex flex-col gap-3">
                    <div>
                      <h2 className="text-sm font-medium">
                        Your authenticator
                      </h2>
                      <p className="text-sm text-muted-foreground">
                        Scan this with your password manager or authenticator
                        app, then prove it with one code below. Nothing has been
                        written yet — leaving now creates nothing.
                      </p>
                    </div>
                    <div className="flex justify-center">
                      <div className="rounded-xl bg-white p-3">
                        <QRCodeCanvas
                          value={enrollment.otpauthUri}
                          size={168}
                          marginSize={0}
                          level="M"
                          title="TOTP enrollment QR code"
                        />
                      </div>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      Can&rsquo;t scan? Add the secret by hand, or open{" "}
                      <a
                        href={enrollment.otpauthUri}
                        className="underline underline-offset-4 hover:text-foreground"
                      >
                        the enrollment link
                      </a>{" "}
                      on a device with an authenticator.
                    </p>
                    <CopyBlock value={enrollment.totpSecret} label="secret" />
                    <CopyBlock
                      value={enrollment.otpauthUri}
                      label="otpauth URI"
                    />
                  </div>

                  <Separator />

                  <form
                    onSubmit={(e) => {
                      e.preventDefault()
                      void finish()
                    }}
                  >
                    <FieldGroup>
                      <Field>
                        <FieldLabel htmlFor="code">One-time code</FieldLabel>
                        <Input
                          id="code"
                          inputMode="numeric"
                          autoComplete="one-time-code"
                          maxLength={CODE_DIGITS + 2}
                          placeholder="123456"
                          className="data tracking-[0.25em]"
                          value={code}
                          onChange={(e) => setCode(e.target.value)}
                        />
                        <FieldDescription>
                          The {CODE_DIGITS}-digit code your authenticator shows
                          for this secret.
                        </FieldDescription>
                      </Field>
                      <Field>
                        <Button type="submit" disabled={!canFinish || isBusy}>
                          {isBusy && <Spinner />}
                          Create my repository
                        </Button>
                      </Field>
                    </FieldGroup>
                  </form>
                </>
              )}
            </CardContent>
          </Card>
        )}
        <p className="text-center text-xs text-muted-foreground">
          Already registered?{" "}
          <Link
            to="/login"
            className="underline underline-offset-4 hover:text-foreground"
          >
            Sign in
          </Link>
          .
        </p>
      </div>
    </div>
  )
}
