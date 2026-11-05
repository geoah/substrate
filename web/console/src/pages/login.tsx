import { useState } from "react"
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
import { ApiError } from "@/lib/api/types"
import { safeReturnPath } from "@/lib/return-path"
import { nativeShell, substrateHostname } from "@/lib/shell"
import { useSite } from "@/lib/site"
import { loginRoute } from "@/router"

/** The door refuses to say WHICH factor was wrong — repository, password and
 * code fail as one, and a lockout reads the same — so the console must not
 * invent specifics the server withheld. Only the rate limit earns sugar (the
 * wait). */
function describeError(err: unknown, totpRequired: boolean): string {
  if (err instanceof ApiError) {
    if (err.code === "rate_limited") {
      const wait = err.retryAfter ?? 5
      return `Too many attempts. Try again in ${wait}s.`
    }
    if (err.code === "auth") {
      return totpRequired
        ? "The address, password or code is wrong. Wait for a fresh code and try again."
        : "The address or password is wrong. Check both and try again."
    }
    return err.message
  }
  return "Something went wrong. Try again."
}

/** The code is required exactly when the substrate verifies one; where it does
 * not, an empty field is valid and the input is never rendered. */
function loginSchema(totpRequired: boolean) {
  return z.object({
    repository: z.string().trim().min(1, "Enter your address."),
    password: z.string().min(1, "Enter your password."),
    code: z.string().refine((v) => !totpRequired || normalizeCode(v) !== null, {
      message: `Enter the current ${CODE_DIGITS}-digit code.`,
    }),
  })
}

type LoginValues = z.infer<ReturnType<typeof loginSchema>>

/** Sign in: the repository, the password and the current TOTP code — all
 * three, every time. The response is a token RECORD and its secret; there is
 * no session beside it, so what the browser keeps is a token like any other
 * client's. */
export function LoginPage() {
  useDocumentTitle("Sign in")
  const navigate = useNavigate()
  const search = loginRoute.useSearch()
  // Where the sign-in returns to: an app's approval page, or the page whose
  // session ran out. Only a path on this console is followed.
  const returnTo = safeReturnPath(search.redirect)
  const { inviteRequired, totpRequired } = useAuthPolicy()
  const site = useSite()
  const host = substrateHostname()
  const [apiError, setApiError] = useState<string | null>(null)
  // The policy lands one render AFTER the first paint, so the resolver has to
  // FOLLOW it rather than be fixed at mount: react-hook-form reads the options
  // it is given on every render, and this form must not be remounted to change
  // them — a remount resets the fields, and by then a password manager has
  // filled the repository and password in.
  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema(totpRequired)),
    // An app that named the repository it wants (`login_hint`, already held
    // to a valid name by the route) fills the field; the owner may change it.
    defaultValues: {
      repository: search.login_hint ?? "",
      password: "",
      code: "",
    },
  })

  async function onSubmit(values: LoginValues) {
    setApiError(null)
    // Where nothing verifies a code, none is sent: the field is not shown, so
    // there is nothing to normalize and nothing to refuse.
    const code = totpRequired ? normalizeCode(values.code) : ""
    if (code === null) return
    const repository = values.repository.trim()
    let minted
    try {
      minted = await login(repository, values.password, code)
    } catch (err) {
      setApiError(describeError(err, totpRequired))
      // A stale code cannot succeed twice; drop it so the next try starts fresh.
      form.resetField("code")
      return
    }
    // The door answers with the repository it RESOLVED, so a bare label is
    // stored as the authority it named.
    saveSession(minted.secret, minted.repository ?? repository, minted.token.id)
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
      <form onSubmit={form.handleSubmit(onSubmit)}>
        <FieldGroup>
          <Field data-invalid={!!errors.repository || undefined}>
            <FieldLabel htmlFor="repository">Your address</FieldLabel>
            <Input
              id="repository"
              className="h-10"
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              autoFocus
              placeholder={host ? `you.${host}` : undefined}
              aria-invalid={!!errors.repository || !!apiError}
              {...form.register("repository")}
            />
            <FieldDescription>Your name on its own works too.</FieldDescription>
            <FieldError errors={[errors.repository]} />
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
          {totpRequired && (
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
                The {CODE_DIGITS}-digit code from your authenticator app.
              </FieldDescription>
              <FieldError errors={[errors.code]} />
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
