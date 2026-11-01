import { useEffect, useState } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { Link, useNavigate } from "@tanstack/react-router"
import { BoxesIcon } from "lucide-react"
import { useForm } from "react-hook-form"
import { z } from "zod"

import { ShellServer } from "@/components/shell-server"
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
import { Spinner } from "@/components/ui/spinner"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { CODE_DIGITS, login, normalizeCode } from "@/lib/api/auth"
import { useAuthPolicy } from "@/lib/api/discovery"
import { saveSession } from "@/lib/api/session"
import { ApiError } from "@/lib/api/types"
import { safeReturnPath } from "@/lib/return-path"
import {
  cancelShellSignIn,
  nativeShell,
  shellSignIn,
  shellSignsIn,
} from "@/lib/shell"
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
        ? "The repository name, password or code is wrong. Wait for a fresh code and try again."
        : "The repository name or password is wrong. Check both and try again."
    }
    return err.message
  }
  return "Something went wrong. Try again."
}

/** The code is required exactly when the substrate verifies one; where it does
 * not, an empty field is valid and the input is never rendered. */
function loginSchema(totpRequired: boolean) {
  return z.object({
    repository: z.string().trim().min(1, "Enter your repository name."),
    password: z.string().min(1, "Enter your password."),
    code: z.string().refine((v) => !totpRequired || normalizeCode(v) !== null, {
      message: `Enter the current ${CODE_DIGITS}-digit code.`,
    }),
  })
}

type LoginValues = z.infer<ReturnType<typeof loginSchema>>

/** Sign in through the app: the substrate's own sign-in and approval page
 * opens in Safari, or the default browser on a Mac, where a password
 * manager fills it and the address bar names the server, and the app hands
 * the session back. The password never reaches this page. */
function ShellSignInCard({
  onSignedIn,
}: {
  onSignedIn: (secret: string, repository: string, tokenId: string) => void
}) {
  const ios = nativeShell()?.platform === "ios"
  const browser = ios ? "Safari" : "your browser"
  const [waiting, setWaiting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function start() {
    setError(null)
    setWaiting(true)
    const reply = await shellSignIn().catch(
      (err: unknown) => ({ error: String(err) }) as const
    )
    setWaiting(false)
    if ("secret" in reply)
      onSignedIn(reply.secret, reply.repository, reply.tokenId)
    else if ("error" in reply) setError(reply.error)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Sign in</CardTitle>
        <CardDescription>
          Your server’s own sign-in page opens in {browser}, where your password
          manager works. Sign in there and approve full access for this app.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {error && (
          <p role="alert" className="text-sm font-normal text-destructive">
            {error}
          </p>
        )}
        <Button onClick={() => void start()} disabled={waiting}>
          {waiting && <Spinner />}
          {waiting
            ? `Waiting for ${ios ? "Safari" : "your browser"}…`
            : ios
              ? "Sign in with Safari"
              : "Continue in your browser"}
        </Button>
        {/* Safari's sheet has its own Cancel; a browser tab left open never
            answers, so the Mac's wait offers one here. */}
        {waiting && !ios && (
          <Button variant="ghost" onClick={() => cancelShellSignIn()}>
            Cancel
          </Button>
        )}
      </CardContent>
    </Card>
  )
}

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
  const [apiError, setApiError] = useState<string | null>(null)
  // Undefined while the server is asked whether the app may sign in for
  // the page; a browser never asks.
  const [shellDoor, setShellDoor] = useState<boolean | undefined>(() =>
    nativeShell()?.signIn ? undefined : false
  )
  useEffect(() => {
    if (shellDoor !== undefined) return
    let live = true
    void shellSignsIn().then((yes) => {
      if (live) setShellDoor(yes)
    })
    return () => {
      live = false
    }
  }, [shellDoor])
  // The policy lands one render AFTER the first paint, so the resolver has to
  // FOLLOW it rather than be fixed at mount: react-hook-form reads the options
  // it is given on every render, and this form must not be remounted to change
  // them — a remount resets the fields, and by then a password manager has
  // filled the repository and password in.
  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema(totpRequired)),
    defaultValues: { repository: "", password: "", code: "" },
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

  function signedInByShell(
    secret: string,
    repository: string,
    tokenId: string
  ) {
    saveSession(secret, repository, tokenId)
    void navigate({ to: returnTo ?? "/", replace: true })
  }

  const { errors, isSubmitting } = form.formState

  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-6">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center justify-center gap-2.5">
          <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <BoxesIcon className="size-4" />
          </div>
          <span className="text-lg font-semibold">substrate</span>
        </div>
        {shellDoor === undefined ? (
          <div className="flex justify-center p-6">
            <Spinner aria-label="Checking how to sign in" />
          </div>
        ) : shellDoor ? (
          <ShellSignInCard onSignedIn={signedInByShell} />
        ) : (
          <Card>
            <CardHeader>
              <CardTitle>Sign in</CardTitle>
              <CardDescription>
                {totpRequired
                  ? `Your name, password and the current ${CODE_DIGITS}-digit code.`
                  : "Your name and password."}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <form onSubmit={form.handleSubmit(onSubmit)}>
                <FieldGroup>
                  <Field data-invalid={!!errors.repository || undefined}>
                    <FieldLabel htmlFor="repository">Repository</FieldLabel>
                    <Input
                      id="repository"
                      autoComplete="username"
                      autoFocus
                      aria-invalid={!!errors.repository || !!apiError}
                      {...form.register("repository")}
                    />
                    <FieldDescription>
                      The name you chose when you registered, such as
                      ada.example.com.
                    </FieldDescription>
                    <FieldError errors={[errors.repository]} />
                  </Field>
                  <Field data-invalid={!!errors.password || undefined}>
                    <FieldLabel htmlFor="password">Password</FieldLabel>
                    <Input
                      id="password"
                      type="password"
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
                        className="data tracking-[0.25em]"
                        aria-invalid={!!errors.code || !!apiError}
                        {...form.register("code")}
                      />
                      <FieldDescription>
                        The current {CODE_DIGITS}-digit code from your
                        authenticator.
                      </FieldDescription>
                      <FieldError errors={[errors.code]} />
                    </Field>
                  )}
                  {apiError && (
                    <p
                      role="alert"
                      className="text-sm font-normal text-destructive"
                    >
                      {apiError}
                    </p>
                  )}
                  <Field>
                    <Button type="submit" disabled={isSubmitting}>
                      {isSubmitting && <Spinner />}
                      Sign in
                    </Button>
                  </Field>
                </FieldGroup>
              </form>
            </CardContent>
          </Card>
        )}
        <p className="text-center text-xs text-muted-foreground">
          {inviteRequired ? "Have an invite code?" : "New here?"}{" "}
          <Link
            to="/register"
            search={{ redirect: returnTo }}
            className="underline underline-offset-4 hover:text-foreground"
          >
            Register
          </Link>{" "}
          to choose your name.
        </p>
        <ShellServer />
      </div>
    </div>
  )
}
