import type { ReactNode } from "react"
import { Link } from "@tanstack/react-router"

import { ColorSchemeMenu } from "@/components/color-scheme-menu"
import { ShellServer } from "@/components/shell-server"
import { cn } from "@/lib/utils"
import { useSite, type SiteInfo } from "@/lib/site"

/** The substrate's own mark, for a deployment that sets no site. */
function DefaultMark({ className }: { className?: string }) {
  return (
    <img
      src="/substrate.svg"
      alt=""
      aria-hidden
      className={cn("rounded-[22%]", className)}
    />
  )
}

/** The brand at the top of a door or the landing page: the site's logo and
 * name where the deployment sets one, the substrate's otherwise. Renders an
 * empty block of the same height while the answer is in flight, so a branded
 * deployment never flashes the default brand first.
 *
 * `showProvider` adds the line that says what a branded deployment is: a
 * provider hosting Substrates, not a product of its own. Without a site the
 * door is Substrate's own and the line is never shown. */
export function DoorBrand({
  site,
  size = "lg",
  showProvider = false,
}: {
  site: SiteInfo | null | undefined
  size?: "sm" | "lg"
  showProvider?: boolean
}) {
  const mark = size === "lg" ? "size-12" : "size-7"
  if (site === undefined) {
    return <div aria-hidden className={size === "lg" ? "h-12" : "h-7"} />
  }
  const name = site?.name ?? "substrate"
  return (
    <div
      data-slot="door-brand"
      className={cn(
        "flex items-center",
        size === "lg" ? "flex-col gap-3 text-center" : "gap-2.5"
      )}
    >
      {site?.logo ? (
        <img src={site.logo} alt="" aria-hidden className={mark} />
      ) : (
        <DefaultMark className={mark} />
      )}
      <div className="flex flex-col gap-1">
        <span
          className={cn(
            "font-semibold tracking-tight text-foreground",
            size === "lg" ? "text-xl" : "text-base"
          )}
        >
          {name}
        </span>
        {showProvider && site && (
          <span className="text-sm text-muted-foreground">
            A Substrate provider
          </span>
        )}
      </div>
    </div>
  )
}

/** The frame both doors share: the brand, one card holding the step, and the
 * way to the other door under it. Top-aligned on a phone, where the keyboard
 * takes the lower half, and centred from `sm` up. */
export function DoorLayout({
  title,
  description,
  eyebrow,
  children,
  footer,
}: {
  title: string
  description?: ReactNode
  /** A small line over the title, such as the step a reader is on. */
  eyebrow?: string
  children: ReactNode
  footer?: ReactNode
}) {
  const site = useSite()
  return (
    <div className="relative min-h-svh overflow-hidden bg-background">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 h-80 bg-[radial-gradient(60%_100%_at_50%_0%,var(--primary-soft),transparent)]"
      />
      <ColorSchemeMenu className="absolute top-3 right-3 z-10 sm:top-4 sm:right-4" />
      <main className="relative mx-auto flex min-h-svh w-full max-w-[600px] flex-col px-4 pt-10 pb-10 sm:justify-center sm:px-6 sm:pt-10">
        <div className="mb-6 flex justify-center sm:mb-8">
          <Link to="/" aria-label={site?.name ?? "Home"} className="rounded-lg">
            <DoorBrand site={site} showProvider />
          </Link>
        </div>
        <section className="rounded-2xl border bg-card p-5 shadow-card sm:p-8">
          <header className="mb-6 flex flex-col gap-1.5">
            {eyebrow && (
              <p className="text-xs font-medium tracking-wide text-primary-text uppercase">
                {eyebrow}
              </p>
            )}
            <h1 className="text-2xl font-semibold tracking-tight text-foreground">
              {title}
            </h1>
            {description && (
              <p className="text-sm text-muted-foreground">{description}</p>
            )}
          </header>
          {children}
        </section>
        {footer && (
          <div className="mt-6 text-center text-sm text-muted-foreground">
            {footer}
          </div>
        )}
        <div className="mt-4">
          <ShellServer />
        </div>
      </main>
    </div>
  )
}

/** The inline link between the doors. */
export const doorLinkClass =
  "font-medium text-primary-text underline-offset-4 hover:underline"
