/** The host-owned chrome an app screen renders under: a header with back
 * and title, a segmented control for two screens or a bottom tab bar for
 * three to five, and the ONE primary button pinned above the safe area and
 * above the tabs, hidden while a text input has focus so the keyboard never
 * covers a button that cannot be reached. B drives it from bridge state
 * (`substrate/title`, `substrate/primary-action`) rather than a view spec. Under 768 px the
 * shell renders nothing else, so the chrome owns the viewport (`h-dvh`); on
 * a desktop it fills the shell's content column. A screen draws no bottom bar
 * and no login of its own. */

import { useEffect, useState, type ReactNode } from "react"
import { ArrowLeftIcon, CircleIcon } from "lucide-react"

import { AppIcon } from "@/components/apps/icon"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useIsMobile } from "@/hooks/use-mobile"
import { cn } from "@/lib/utils"

export interface ChromeScreen {
  name: string
  label: string
  icon?: string
}

function isTextInput(target: EventTarget | null): boolean {
  return (
    target instanceof HTMLElement &&
    target.matches(
      "input:not([type=button]):not([type=checkbox]):not([type=radio]), textarea, select, [contenteditable=true]"
    )
  )
}

/** Whether a text input holds focus anywhere in the document, sheets
 * included, because they portal outside the chrome. */
function useInputFocused(): boolean {
  const [focused, setFocused] = useState(false)
  useEffect(() => {
    const on = (e: FocusEvent) => setFocused(isTextInput(e.target))
    const off = () => setFocused(false)
    document.addEventListener("focusin", on)
    document.addEventListener("focusout", off)
    return () => {
      document.removeEventListener("focusin", on)
      document.removeEventListener("focusout", off)
    }
  }, [])
  return focused
}

export function AppChrome({
  title,
  subtitle,
  onBack,
  screens = [],
  screen,
  onScreen,
  primary,
  actions,
  status,
  children,
}: {
  title: string
  subtitle?: string
  /** Absent hides the back button. */
  onBack?: () => void
  screens?: ChromeScreen[]
  /** The active screen's name. */
  screen?: string
  onScreen?: (name: string) => void
  /** The one primary action button. */
  primary?: ReactNode
  /** Header-placed actions, trailing the title. */
  actions?: ReactNode
  /** A line under the header: an unresolved input, a warning. */
  status?: ReactNode
  children: ReactNode
}) {
  const isMobile = useIsMobile()
  const inputFocused = useInputFocused()
  const segmented = screens.length === 2
  const tabBar = screens.length >= 3 && screens.length <= 5
  const showPrimary = Boolean(primary) && !inputFocused

  return (
    <div
      data-chrome="app"
      className={cn(
        "flex min-h-0 w-full flex-1 flex-col bg-background",
        isMobile && "h-dvh"
      )}
    >
      <header className="z-10 flex shrink-0 flex-col border-b bg-background/95 pt-[env(safe-area-inset-top)] backdrop-blur">
        <div className="flex min-h-12 items-center gap-1 px-2">
          {onBack && (
            <Button
              variant="ghost"
              size="icon"
              className="size-11 shrink-0"
              aria-label="Back"
              onClick={onBack}
            >
              <ArrowLeftIcon className="size-5" />
            </Button>
          )}
          <div className={cn("min-w-0 flex-1", !onBack && "pl-2")}>
            <h1 className="truncate text-base leading-tight font-semibold">
              {title}
            </h1>
            {subtitle && (
              <p className="truncate text-xs text-muted-foreground">
                {subtitle}
              </p>
            )}
          </div>
          {actions && (
            <div className="flex shrink-0 items-center gap-1">{actions}</div>
          )}
        </div>
        {segmented && (
          <Tabs
            value={screen}
            onValueChange={(value) => onScreen?.(String(value))}
            className="px-3 pb-2"
          >
            <TabsList className="h-10 w-full md:h-9 md:w-auto md:min-w-72">
              {screens.map((s) => (
                <TabsTrigger key={s.name} value={s.name} className="text-sm">
                  {s.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        )}
        {status && (
          <div className="border-t px-4 py-2 text-xs text-muted-foreground">
            {status}
          </div>
        )}
      </header>

      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain">
        {children}
      </div>

      {(primary || tabBar) && (
        <div
          className={cn(
            "z-10 shrink-0 border-t bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur",
            !showPrimary && !tabBar && "hidden"
          )}
        >
          {/* Hidden, never unmounted: the button owns the sheet it opened,
              and the sheet's first input is what takes the focus. */}
          {primary && (
            <div
              className={cn(
                "mx-auto w-full max-w-md px-4 py-3",
                !showPrimary && "hidden"
              )}
            >
              {primary}
            </div>
          )}
          {tabBar && (
            <nav className="flex h-14 items-stretch" aria-label="Screens">
              {screens.map((s) => {
                const active = s.name === screen
                return (
                  <button
                    key={s.name}
                    type="button"
                    aria-current={active ? "page" : undefined}
                    onClick={() => onScreen?.(s.name)}
                    className={cn(
                      "flex min-w-0 flex-1 flex-col items-center justify-center gap-0.5 text-[0.7rem] select-none",
                      active ? "text-primary" : "text-muted-foreground"
                    )}
                  >
                    <AppIcon
                      name={s.icon}
                      fallback={CircleIcon}
                      className="size-5"
                    />
                    <span className="truncate">{s.label}</span>
                  </button>
                )
              })}
            </nav>
          )}
        </div>
      )}
    </div>
  )
}
