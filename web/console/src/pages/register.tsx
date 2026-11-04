import { useState } from "react"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
} from "lucide-react"
import { QRCodeCanvas } from "qrcode.react"

import { DnsRecord } from "@/components/dns-record"
import { DoorLayout, doorLinkClass } from "@/components/door/door-layout"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from "@/components/ui/input-group"
import { Spinner } from "@/components/ui/spinner"
import { useDocumentTitle } from "@/hooks/use-document-title"
import {
  CODE_DIGITS,
  normalizeCode,
  register,
  registerEnroll,
} from "@/lib/api/auth"
import { useAuthPolicy } from "@/lib/api/discovery"
import { saveSession } from "@/lib/api/session"
import { ApiError, type TOTPEnrollment } from "@/lib/api/types"
import { safeReturnPath } from "@/lib/return-path"
import { substrateHostname, substrateOrigin } from "@/lib/shell"
import { useSite } from "@/lib/site"
import { registerRoute } from "@/router"

/** Long enough that argon2id is not the only thing between a guess and the
 * repository; the server has no opinion beyond non-empty. */
const MIN_PASSWORD = 8

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
      <code className="min-w-0 flex-1 rounded-lg bg-muted px-3 py-2 data text-xs [overflow-wrap:anywhere]">
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
        className="h-10 min-w-0 flex-1 data"
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

/** A name as the reader typed it, the way the server reads it: trimmed,
 * lowercase, without a trailing dot. */
function cleanName(name: string): string {
  return name.trim().replace(/\.$/, "").toLowerCase()
}

/** One DNS label, the server's grammar for each label of an authority
 * (`vocabulary.ValidRepositoryAuthority`): checked here so a name the server
 * would refuse is refused as it is typed, not after the password. */
const LABEL = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/

function validDomain(name: string): boolean {
  const labels = name.split(".")
  return labels.length > 1 && labels.every((l) => LABEL.test(l))
}

/** The authority a repository name resolves to, mirroring the server's rule
 * (`vocabulary.RepositoryAuthority`) so the form shows the name the
 * repository will actually get. On this server's address the reader types one
 * label and it sits under the substrate's host, which never changes with what
 * they type: a domain of their own is the advanced option's alone, so nobody
 * claims one by accident. */
function repositoryAuthority(
  name: string,
  host: string,
  ownDomain: boolean
): string {
  const n = cleanName(name)
  if (ownDomain) return validDomain(n) ? n : ""
  if (!LABEL.test(n)) return ""
  const h = host.replace(/\.$/, "").toLowerCase()
  return h ? `${n}.${h}` : ""
}

/** What choosing their own domain means, said before the reader commits to a
 * name nothing lets them change: it must be theirs, and it is proved after
 * registration with one TXT record (decision 0208). */
function OwnDomainGuide({ domain }: { domain: string }) {
  const name = domain || "home.example.org"
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border bg-muted/50 p-3.5 text-sm">
      <p className="text-warning">
        <span className="font-medium">Only use a domain you own.</span> After
        signing up, you prove it by adding this DNS record. Until then, your
        Substrate is marked unverified.
      </p>
      <div className="rounded-md border border-border bg-background px-3 py-2.5 text-[12.5px]">
        <DnsRecord
          name={`_substrate.${name}`}
          type="TXT"
          value={`v=substrate1 server=${substrateOrigin()} proof=…`}
        />
      </div>
      <p className="text-muted-foreground">
        Settings, Verify domain has the exact value. This one record is all you
        add: the domain doesn&rsquo;t need to point at this server.
      </p>
    </div>
  )
}

/** Registration, in two steps.
 *
 * Step one collects the invite code (where this substrate reads one) and the
 * repository AND the password, then buys a TOTP seed — it writes NOTHING (the
 * browser holds the seed). Password before the
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
  useDocumentTitle("Create your Substrate")
  const { inviteRequired, totpRequired } = useAuthPolicy()
  const site = useSite()
  const host = substrateHostname().replace(/\.$/, "").toLowerCase()
  const navigate = useNavigate()
  // Where registering returns to, as signing in does: only a path on this
  // console is followed.
  const returnTo = safeReturnPath(registerRoute.useSearch().redirect)
  const [inviteCode, setInviteCode] = useState("")
  const [repository, setRepository] = useState("")
  // With no host to complete a label under (a native shell whose server
  // address does not parse), the reader's own domain is the only name there
  // is.
  const [ownDomainChosen, setOwnDomain] = useState(false)
  const ownDomain = ownDomainChosen || !host
  // The name the repository will actually get. Sending the resolved value
  // rather than leaving the completion to the server keeps what the form
  // shows and what the repository gets one string.
  const authority = repositoryAuthority(repository, host, ownDomain)
  const typed = cleanName(repository)
  // A domain is wrong until its last label is typed, so its error waits for
  // the reader to leave the field; a label is wrong the moment it is.
  const [domainLeft, setDomainLeft] = useState(false)
  const nameProblem =
    !typed || authority
      ? null
      : ownDomain
        ? domainLeft
          ? "Enter a full domain, like home.example.org."
          : null
        : typed.includes(".")
          ? "To use your own domain, choose Advanced below."
          : "Use letters, numbers and hyphens, starting and ending with a letter or number."
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [code, setCode] = useState("")
  const [recoveryKey, setRecoveryKey] = useState<string | null>(null)
  const [enrolled, setEnrolled] = useState<TOTPEnrollment | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [isBusy, setBusy] = useState(false)

  function fail(err: unknown) {
    if (err instanceof ApiError) {
      if (err.code === "rate_limited") {
        setError(`Too many attempts. Try again in ${err.retryAfter ?? 5}s.`)
        return
      }
      setError(err.message)
      return
    }
    setError("Something went wrong. Try again.")
  }

  // A return path wins over Verify domain: an app's approval page that sent
  // the reader here is waiting for them, and Settings keeps.
  function land() {
    if (returnTo) return navigate({ to: returnTo, replace: true })
    if (ownDomain) {
      return navigate({ to: "/settings", hash: "verify-domain", replace: true })
    }
    return navigate({ to: "/", replace: true })
  }

  const passwordsMatch = password.length > 0 && password === confirm
  // A door that reads no invite code is not owed one; whatever is typed is
  // sent regardless, because the substrate ignores it either way.
  const canEnroll =
    (!inviteRequired || inviteCode.trim().length > 0) &&
    authority.length > 0 &&
    password.length >= MIN_PASSWORD &&
    passwordsMatch

  // DERIVED, not stored: a registration submitted before discovery answered
  // runs under the strict default and buys an enrollment (which writes
  // nothing). If the answer then says this substrate verifies no code, that
  // enrollment is a seed with no door behind it, and the second step it would
  // open is a ceremony over a secret nothing will check.
  const enrollment = totpRequired ? enrolled : null

  /** Back to step one. The enrollment wrote nothing, so dropping it loses
   * nothing but the seed, and the next Continue buys a fresh one. */
  function startOver() {
    setEnrolled(null)
    setCode("")
    setError(null)
  }

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
      setEnrolled(await registerEnroll(inviteCode.trim(), authority))
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
      const minted = await register({
        inviteCode: inviteCode.trim(),
        repository: authority,
        password,
        totpSecret: enrollment?.totpSecret ?? "",
        totpCode: normalized,
        label: "console",
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
      saveSession(
        minted.secret,
        minted.repository ?? authority,
        minted.token.id
      )
      if (holdKeys) return
      await land()
    } catch (err) {
      if (err instanceof ApiError && err.code === "auth") {
        // Which factor was wrong is whichever this door reads: a substrate
        // that reads neither has nothing here to get wrong.
        setError(
          totpRequired && inviteRequired
            ? "The invite code or the 6-digit code is wrong. Check the code your authenticator shows right now and try again."
            : totpRequired
              ? "The 6-digit code is wrong. Check the code your authenticator shows right now and try again."
              : inviteRequired
                ? "The invite code is wrong."
                : err.message
        )
        setCode("")
      } else {
        fail(err)
      }
    } finally {
      setBusy(false)
    }
  }

  // Who resets a lost password: the people running this server, named the
  // way the reader knows them.
  const operator = site
    ? `the people who run ${site.name}`
    : "whoever runs this server"
  const twoSteps = totpRequired && recoveryKey === null
  const errorNote = error && (
    <p
      role="alert"
      className="mb-5 rounded-lg bg-bad-soft px-3 py-2 text-sm text-destructive"
    >
      {error}
    </p>
  )

  if (recoveryKey !== null) {
    return (
      <DoorLayout
        eyebrow="Substrate created"
        title="Save your recovery key"
        description="It is shown once and never again. Put it in your password manager now."
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void land()
          }}
        >
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="recovery-key">Recovery key</FieldLabel>
              <KeepKeyField
                id="recovery-key"
                label="recovery key"
                value={recoveryKey}
              />
              <FieldDescription>
                Nobody can recover this key for you. With it, a download of
                everything you keep here opens on another server. Without it,
                only this server can read your saved keys and passwords.
              </FieldDescription>
            </Field>
            <Field>
              <Button type="submit" size="lg" className="h-10">
                Saved, continue
              </Button>
            </Field>
          </FieldGroup>
        </form>
      </DoorLayout>
    )
  }

  return (
    <DoorLayout
      eyebrow={
        twoSteps ? (enrollment ? "Step 2 of 2" : "Step 1 of 2") : undefined
      }
      title={enrollment ? "Connect an authenticator" : "Create your Substrate"}
      description={
        enrollment
          ? "Scan the code with your authenticator app or password manager, then type the code it shows. Nothing is saved until you finish."
          : site
            ? `Choose your address on ${site.name} and a password.`
            : "Choose your address and a password."
      }
      footer={
        <>
          Already have a Substrate?{" "}
          <Link
            to="/login"
            search={{ redirect: returnTo }}
            className={doorLinkClass}
          >
            Sign in
          </Link>
        </>
      }
    >
      {errorNote}
      <form
        hidden={enrollment !== null}
        onSubmit={(e) => {
          e.preventDefault()
          void beginEnrollment()
        }}
      >
        <FieldGroup>
          {inviteRequired && (
            <Field>
              <FieldLabel htmlFor="inviteCode">Invite code</FieldLabel>
              <Input
                id="inviteCode"
                className="h-10 data text-base"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                value={inviteCode}
                onChange={(e) => setInviteCode(e.target.value)}
                disabled={enrollment !== null}
                autoFocus
              />
              <FieldDescription>
                From the person who invited you.
              </FieldDescription>
            </Field>
          )}
          <Field data-invalid={nameProblem ? true : undefined}>
            <FieldLabel htmlFor="repository">
              {ownDomain ? "Your domain" : "Choose your address"}
            </FieldLabel>
            <InputGroup className="h-10">
              <InputGroupInput
                id="repository"
                className="text-base md:text-sm"
                autoComplete="username"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                placeholder={ownDomain ? "home.example.org" : "yourname"}
                aria-describedby="repository-address"
                aria-invalid={nameProblem ? true : undefined}
                value={repository}
                onChange={(e) => setRepository(e.target.value.toLowerCase())}
                onBlur={() => setDomainLeft(ownDomain)}
                disabled={enrollment !== null}
              />
              {!ownDomain && (
                <InputGroupAddon align="inline-end">
                  <InputGroupText className="text-base text-muted-foreground md:text-sm">
                    .{host}
                  </InputGroupText>
                </InputGroupAddon>
              )}
            </InputGroup>
            {nameProblem && <FieldError errors={[{ message: nameProblem }]} />}
            <p
              id="repository-address"
              aria-live="polite"
              className="text-sm text-muted-foreground"
            >
              {authority ? (
                <>
                  You&rsquo;ll sign in as{" "}
                  <span className="font-medium [overflow-wrap:anywhere] text-foreground">
                    {authority}
                  </span>
                  . It can&rsquo;t be changed later.
                </>
              ) : (
                "Your Substrate’s address and sign-in name. It can’t be changed later."
              )}
            </p>
            {host && (
              <button
                type="button"
                className="inline-flex w-fit cursor-pointer items-center gap-1 text-sm text-muted-foreground hover:text-foreground disabled:cursor-default disabled:opacity-50"
                aria-expanded={ownDomain}
                aria-controls={ownDomain ? "own-domain-guide" : undefined}
                disabled={enrollment !== null}
                onClick={() => {
                  // A typed name crosses over when it reads as the other
                  // option's, so pasting a domain first and then choosing
                  // this costs nothing; anything else is cleared rather than
                  // reinterpreted.
                  const keep = ownDomain
                    ? !typed.includes(".")
                    : typed.includes(".")
                  if (!keep) setRepository("")
                  setDomainLeft(false)
                  setOwnDomain(!ownDomain)
                }}
              >
                {ownDomain ? (
                  <>
                    <ChevronLeftIcon className="size-4" aria-hidden />
                    Use an address on {host} instead
                  </>
                ) : (
                  <>
                    <ChevronRightIcon className="size-4" aria-hidden />
                    Advanced: use your own domain
                  </>
                )}
              </button>
            )}
            {ownDomain && (
              <div id="own-domain-guide">
                <OwnDomainGuide domain={authority} />
              </div>
            )}
          </Field>
          <Field>
            <FieldLabel htmlFor="password">Password</FieldLabel>
            <Input
              id="password"
              type="password"
              className="h-10"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={enrollment !== null}
            />
            <FieldDescription>
              At least {MIN_PASSWORD} characters.{" "}
              {totpRequired
                ? `If you lose it and your authenticator, only ${operator} can reset them.`
                : `If you lose it, only ${operator} can reset it.`}
            </FieldDescription>
          </Field>
          <Field
            data-invalid={(confirm.length > 0 && !passwordsMatch) || undefined}
          >
            <FieldLabel htmlFor="confirm">Confirm password</FieldLabel>
            <Input
              id="confirm"
              type="password"
              className="h-10"
              autoComplete="new-password"
              aria-invalid={confirm.length > 0 && !passwordsMatch}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              disabled={enrollment !== null}
            />
            {confirm.length > 0 && !passwordsMatch && (
              <FieldError errors={[{ message: "The two passwords differ." }]} />
            )}
          </Field>
          {!enrollment && (
            <Field>
              <Button
                type="submit"
                size="lg"
                className="h-10"
                disabled={!canEnroll || isBusy}
              >
                {isBusy && <Spinner />}
                {totpRequired ? "Continue" : "Create your Substrate"}
              </Button>
            </Field>
          )}
        </FieldGroup>
      </form>

      {enrollment && (
        <div className="flex flex-col gap-6">
          <div className="flex items-center justify-between gap-3 rounded-lg bg-muted px-3 py-2.5">
            <div className="flex min-w-0 flex-col">
              <span className="text-xs text-muted-foreground">
                Your Substrate&rsquo;s address
              </span>
              <span className="text-sm font-medium [overflow-wrap:anywhere]">
                {authority}
              </span>
            </div>
            <Button type="button" variant="ghost" size="sm" onClick={startOver}>
              Change
            </Button>
          </div>

          <div className="flex flex-col items-center gap-4">
            <div className="rounded-xl border bg-white p-3.5">
              <QRCodeCanvas
                value={enrollment.otpauthUri}
                size={176}
                marginSize={0}
                level="M"
                title="Authenticator setup QR code"
              />
            </div>
            <a
              href={enrollment.otpauthUri}
              className={doorLinkClass + " text-center text-sm"}
            >
              On this phone? Open it in your authenticator
            </a>
          </div>

          <div className="flex flex-col gap-2">
            <p className="text-sm text-muted-foreground">
              Can&rsquo;t scan it? Type this setup key into your app instead.
            </p>
            <CopyBlock value={enrollment.totpSecret} label="setup key" />
          </div>

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
                  className="h-10 data text-base tracking-[0.25em]"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                />
                <FieldDescription>
                  The {CODE_DIGITS}-digit code your app shows now.
                </FieldDescription>
              </Field>
              <Field>
                <Button
                  type="submit"
                  size="lg"
                  className="h-10"
                  disabled={!canFinish || isBusy}
                >
                  {isBusy && <Spinner />}
                  Create your Substrate
                </Button>
              </Field>
            </FieldGroup>
          </form>
        </div>
      )}
    </DoorLayout>
  )
}
