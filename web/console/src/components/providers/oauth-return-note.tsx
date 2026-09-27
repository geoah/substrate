/** What an account's connect came back with, when the consent tab could not
 * report to the page that opened it and landed on the console itself
 * instead: the substrate's return page falls back to
 * `?connected=<account record path>` or `?error=<correlation>`
 * (internal/api/bundles.go), and the old `/registry` address carries both on
 * to the Providers pages (`lib/oauth-return.ts`). */

import { CircleCheckIcon, TriangleAlertIcon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import type { OAuthReturnSearch } from "@/lib/oauth-return"
import { splitRecordPath } from "@/lib/record-path"
import { cn } from "@/lib/utils"

export function OAuthReturnNote({
  connected,
  error,
  onDismiss,
  className,
}: OAuthReturnSearch & { onDismiss: () => void; className?: string }) {
  if (!connected && !error) return null
  const account = connected ? (splitRecordPath(connected)?.id ?? connected) : ""
  const ok = !error
  const Icon = ok ? CircleCheckIcon : TriangleAlertIcon
  return (
    <div
      role="status"
      data-slot="oauth-return"
      className={cn(
        "flex items-start gap-2.5 rounded-lg px-3.5 py-3 text-[13px]",
        ok ? "bg-ok-soft" : "bg-bad-soft",
        className
      )}
    >
      <Icon
        aria-hidden
        className={cn(
          "mt-0.5 size-4 shrink-0",
          ok ? "text-ok" : "text-destructive"
        )}
      />
      <div className="min-w-0 flex-1 space-y-0.5">
        <p className="font-medium">
          {ok ? "Account connected" : "Connecting the account failed"}
        </p>
        <p className="text-muted-foreground">
          {ok ? (
            <>
              <span className="font-mono text-xs [overflow-wrap:anywhere]">
                {account}
              </span>{" "}
              is approved and its first sync is starting.
            </>
          ) : (
            <>
              The provider refused. Reference{" "}
              <span className="font-mono text-xs [overflow-wrap:anywhere]">
                {error}
              </span>
              .
            </>
          )}
        </p>
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Dismiss"
        onClick={onDismiss}
        className="-my-1 shrink-0"
      >
        <XIcon />
      </Button>
    </div>
  )
}
