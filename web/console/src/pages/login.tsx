import { useState } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { Link, useNavigate } from "@tanstack/react-router"
import { BoxesIcon } from "lucide-react"
import { useForm } from "react-hook-form"
import { z } from "zod"

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
import { CODE_DIGITS, login, normalizeCode } from "@/lib/api/auth"
import { useAuthPolicy } from "@/lib/api/discovery"
import { saveSession } from "@/lib/api/session"
import { ApiError } from "@/lib/api/types"
import { loginRoute } from "@/router"

/** The door refuses to say WHICH factor was wrong — username, password and
 * code fail as one, and a lockout reads the same — so the console must not
 * invent specifics the server withheld. Only the rate limit earns sugar (the
 * wait). */
function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "rate_limited") {
      const wait = err.retryAfter ?? 5
      return `Too many attempts — the door is rate limited on purpose. Try again in ${wait}s.`
    }
    if (err.code === "auth") {
      return "The username, password or code is wrong — or there have been too many failed attempts. Wait for a fresh code and try again."
    }
    return err.message
  }
  return "Something went wrong. Try again."
}

/** The code is required exactly when the substrate verifies one; where it does
 * not, an empty field is valid and the input is never rendered. */
function loginSchema(totpRequired: boolean) {
  return z.object({
    username: z.string().trim().min(1, "Enter your username."),
    password: z.string().min(1, "Enter your password."),
    code: z.string().refine((v) => !totpRequired || normalizeCode(v) !== null, {
      message: `Enter the current ${CODE_DIGITS}-digit code.`,
    }),
  })
}

type LoginValues = z.infer<ReturnType<typeof loginSchema>>

/** Sign in: username, password and the current TOTP code — all three, every
 * time. The response is a token RECORD and its secret; there is no session
 * beside it, so what the browser keeps is a token like any other client's. */
export function LoginPage() {
  const navigate = useNavigate()
  const search = loginRoute.useSearch()
  const { totpRequired } = useAuthPolicy()
  const [apiError, setApiError] = useState<string | null>(null)
  // The policy lands one render AFTER the first paint, so the resolver has to
  // FOLLOW it rather than be fixed at mount: react-hook-form reads the options
  // it is given on every render, and this form must not be remounted to change
  // them — a remount resets the fields, and by then a password manager has
  // filled the username and password in.
  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema(totpRequired)),
    defaultValues: { username: "", password: "", code: "" },
  })

  async function onSubmit(values: LoginValues) {
    setApiError(null)
    // Where nothing verifies a code, none is sent: the field is not shown, so
    // there is nothing to normalize and nothing to refuse.
    const code = totpRequired ? normalizeCode(values.code) : ""
    if (code === null) return
    const username = values.username.trim()
    let minted
    try {
      minted = await login(username, values.password, code)
    } catch (err) {
      setApiError(describeError(err))
      // A stale code cannot succeed twice; drop it so the next try starts fresh.
      form.resetField("code")
      return
    }
    saveSession(minted.secret, username, minted.token.id)
    await navigate({ to: search.redirect ?? "/", replace: true })
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
        <Card>
          <CardHeader>
            <CardTitle>Sign in</CardTitle>
            <CardDescription>
              {totpRequired
                ? `Your username, password and the current ${CODE_DIGITS}-digit code.`
                : "Your username and password — this substrate does not verify a second factor."}{" "}
              Signing in mints a token that stays in this browser.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={form.handleSubmit(onSubmit)}>
              <FieldGroup>
                <Field data-invalid={!!errors.username || undefined}>
                  <FieldLabel htmlFor="username">Username</FieldLabel>
                  <Input
                    id="username"
                    autoComplete="username"
                    autoFocus
                    aria-invalid={!!errors.username || !!apiError}
                    {...form.register("username")}
                  />
                  <FieldError errors={[errors.username]} />
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
        <p className="text-center text-xs text-muted-foreground">
          Holding an invite code?{" "}
          <Link
            to="/register"
            className="underline underline-offset-4 hover:text-foreground"
          >
            Register
          </Link>{" "}
          — it creates your repository and signs you in.
        </p>
      </div>
    </div>
  )
}
