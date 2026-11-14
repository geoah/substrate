import { useEffect, useState } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { Link, useNavigate } from "@tanstack/react-router"
import { useForm } from "react-hook-form"
import { z } from "zod"

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
import { Spinner } from "@/components/ui/spinner"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { CODE_DIGITS, login, normalizeCode } from "@/lib/api/auth"
import { useAuthPolicy } from "@/lib/api/discovery"
import { saveSession } from "@/lib/api/session"
import { ApiError, type RepositoryChoice } from "@/lib/api/types"
import { safeReturnPath } from "@/lib/return-path"
import { nativeShell, substrateHostname } from "@/lib/shell"
import { useSite } from "@/lib/site"
import { loginRoute } from "@/router"

/** The door refuses to say WHICH factor was wrong — name, password and code
 * fail as one, and a lockout reads the same — so the console must not invent
 * specifics the server withheld. Only the rate limit earns sugar (the wait),
 * and a repository set aside, which the door says only once the factors are
 * right, earns who acts on it. */
function describeError(err: unknown, codeAsked: boolean): string {
  if (err instanceof ApiError) {
    if (err.code === "rate_limited") {
      const wait = err.retryAfter ?? 5
      return `Too many attempts. Try again in ${wait}s.`
    }
    if (err.code === "auth") {
      return codeAsked
        ? "The name, password or code is wrong. Wait for a fresh code and try again."
        : "The name or password is wrong. Check both and try again."
    }
    if (err.code === "unavailable" && err.reason === "set_aside") {
      return "This repository is set aside: the server found it damaged when it started, so it serves the others and writes nothing to this one. It serves it again once its operator repairs it and restarts the server."
    }
    return err.message
  }
  return "Something went wrong. Try again."
}

/** The code is required once the substrate has asked for it; before that an
 * empty field is valid and the input is never rendered. */
function loginSchema(codeAsked: boolean) {
  return z.object({
    identity: z.string().trim().min(1, "Enter your name."),
    password: z.string().min(1, "Enter your password."),
    code: z.string().refine((v) => !codeAsked || normalizeCode(v) !== null, {
      message: `Enter the current ${CODE_DIGITS}-digit code.`,
    }),
  })
}

type LoginValues = z.infer<ReturnType<typeof loginSchema>>

/** Sign in, in two steps: the person's name and the password, then the
 * current TOTP code only where the substrate answers `totp_required`, which it
 * does only for a right password on an account that has a second factor
 * (decision 0237). A person who may open several repositories is answered
 * with them, and picks one; the code they sent was not spent (decision
 * 0274). The response is a token RECORD and its secret; there is no session
 * beside it, so what the browser keeps is a token like any other client's. */
export function LoginPage() {
  useDocumentTitle("Sign in")
  const navigate = useNavigate()
  const search = loginRoute.useSearch()
  // Where the sign-in returns to: an app's approval page, or the page whose
  // session ran out. Only a path on this console is followed.
  const returnTo = safeReturnPath(search.redirect)
  const { inviteRequired, nameHost } = useAuthPolicy()
  const site = useSite()
  const host = nameHost ?? substrateHostname()
  const [apiError, setApiError] = useState<string | null>(null)
  // Whether the substrate asked for a code. The resolver FOLLOWS it rather
  // than being fixed at mount: react-hook-form reads the options it is given
  // on every render, and remounting the form would reset the repository and
  // password the reader just sent.
  const [codeAsked, setCodeAsked] = useState(false)
  // The repositories the person may open, once the substrate asked which.
  const [choices, setChoices] = useState<RepositoryChoice[] | null>(null)
  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema(codeAsked)),
    // An app that named the repository it wants (`login_hint`, already held
    // to a valid name by the route) fills the field: a person signs in with
    // their repository's name unless they took another (decision 0274), and
    // may change it.
    defaultValues: {
      identity: search.login_hint ?? "",
      password: "",
      code: "",
    },
  })

  // The field appears with the answer that asked for it, and the reader's
  // next keystroke belongs in it.
  const { setFocus } = form
  useEffect(() => {
    if (codeAsked) setFocus("code")
  }, [codeAsked, setFocus])

  async function signIn(values: LoginValues, repository?: string) {
    setApiError(null)
    // Until the substrate asks, no code is sent: the field is not shown, so
    // there is nothing to normalize and nothing to refuse.
    const code = codeAsked ? normalizeCode(values.code) : ""
    if (code === null) return
    const identity = values.identity.trim()
    let minted
    try {
      minted = await login(
        identity,
        values.password,
        code,
        "console",
        repository
      )
    } catch (err) {
      if (err instanceof ApiError && err.code === "totp_required") {
        setCodeAsked(true)
        return
      }
      if (
        err instanceof ApiError &&
        err.reason === "repository_required" &&
        err.repositories?.length
      ) {
        setChoices(err.repositories)
        return
      }
      setChoices(null)
      setApiError(describeError(err, codeAsked))
      // A stale code cannot succeed twice; drop it so the next try starts fresh.
      form.resetField("code")
      return
    }
    // The door answers with the repository and the person it RESOLVED, so a
    // bare label is stored as the name it completed.
    saveSession(
      minted.secret,
      minted.repository ?? identity,
      minted.token.id,
      minted.identity ?? identity
    )
    await navigate({ to: returnTo ?? "/", replace: true })
  }

  const { errors, isSubmitting } = form.formState

  return (
    <DoorLayout
      title="Sign in to your Substrate"
      description={site ? `Welcome back to ${site.name}.` : "Welcome back."}
      footer={
        nativeShell()?.appStore ? undefined : (
          <>
            {inviteRequired ? "Have an invite?" : "New here?"}{" "}
            <Link
              to="/register"
              search={{ redirect: returnTo }}
              className={doorLinkClass}
            >
              Create your Substrate
            </Link>
          </>
        )
      }
    >
      <form onSubmit={form.handleSubmit((values) => signIn(values))}>
        <FieldGroup>
          <Field data-invalid={!!errors.identity || undefined}>
            <FieldLabel htmlFor="identity">Your name</FieldLabel>
            <Input
              id="identity"
              className="h-10"
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              autoFocus
              placeholder={host ? `you.${host}` : undefined}
              aria-invalid={!!errors.identity || !!apiError}
              {...form.register("identity", {
                onChange: () => setChoices(null),
              })}
            />
            <FieldDescription>
              The name you sign in with. The part before the first dot works on
              its own.
            </FieldDescription>
            <FieldError errors={[errors.identity]} />
          </Field>
          <Field data-invalid={!!errors.password || undefined}>
            <FieldLabel htmlFor="password">Password</FieldLabel>
            <Input
              id="password"
              type="password"
              className="h-10"
              autoComplete="current-password"
              aria-invalid={!!errors.password || !!apiError}
              {...form.register("password")}
            />
            <FieldError errors={[errors.password]} />
          </Field>
          {codeAsked && (
            <Field data-invalid={!!errors.code || undefined}>
              <FieldLabel htmlFor="code">One-time code</FieldLabel>
              <Input
                id="code"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={CODE_DIGITS + 2}
                placeholder="123456"
                className="h-10 data text-base tracking-[0.25em]"
                aria-invalid={!!errors.code || !!apiError}
                {...form.register("code")}
              />
              <FieldDescription>
                Your Substrate has two-factor sign-in on. Enter the{" "}
                {CODE_DIGITS}-digit code from your authenticator app.
              </FieldDescription>
              <FieldError errors={[errors.code]} />
            </Field>
          )}
          {choices && (
            <Field>
              <FieldLabel>Open which Substrate?</FieldLabel>
              <FieldDescription>
                You may open more than one. Pick the one to sign in to.
              </FieldDescription>
              {choices.map((choice) => (
                <Button
                  key={choice.id}
                  type="button"
                  variant="outline"
                  className="h-auto min-h-10 justify-start text-left [overflow-wrap:anywhere] whitespace-normal"
                  disabled={isSubmitting}
                  onClick={() =>
                    void form.handleSubmit((values) =>
                      signIn(values, choice.id)
                    )()
                  }
                >
                  {choice.name}
                </Button>
              ))}
            </Field>
          )}
          {apiError && (
            <p
              role="alert"
              className="rounded-lg bg-bad-soft px-3 py-2 text-sm text-destructive"
            >
              {apiError}
            </p>
          )}
          <Field>
            <Button
              type="submit"
              size="lg"
              className="h-10"
              disabled={isSubmitting}
            >
              {isSubmitting && <Spinner />}
              Sign in
            </Button>
          </Field>
        </FieldGroup>
      </form>
    </DoorLayout>
  )
}
