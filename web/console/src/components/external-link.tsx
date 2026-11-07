/** A link that leaves the console: it opens in a new tab (the system
 * browser in the Apple shell), carries no referrer and no opener, and says
 * so twice, with the external-link mark beside its text and in words for a
 * screen reader. Every link the console opens outside itself is one of
 * these, so a reader always knows before they click. */

import type { AnchorHTMLAttributes, ReactNode } from "react"
import { ExternalLinkIcon } from "lucide-react"

import { cn } from "@/lib/utils"

export function ExternalLink({
  href,
  children,
  className,
  iconClassName,
  nofollow = false,
  ...rest
}: {
  href: string
  children: ReactNode
  className?: string
  iconClassName?: string
  /** A link a model or a web page chose, which the console vouches for
   * nothing about. */
  nofollow?: boolean
} & Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href" | "target" | "rel">) {
  return (
    <a
      href={href}
      target="_blank"
      rel={nofollow ? "noopener noreferrer nofollow" : "noopener noreferrer"}
      className={className}
      {...rest}
    >
      {children}{" "}
      {/* The space sits outside: a name computed from the children joins
          them without one. */}
      <span className="sr-only">(opens in a new tab)</span>
      <ExternalLinkIcon
        aria-hidden
        data-external-link-mark=""
        className={cn(
          "inline size-[0.85em] shrink-0 align-[-0.05em] opacity-70",
          iconClassName
        )}
      />
    </a>
  )
}
