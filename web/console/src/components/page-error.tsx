/** What the console shows in place of a part of itself that failed while
 * rendering, so one failure never blanks the rest. `PageError` is every
 * route's error component (the router's default): the shell and its sidebar
 * stay, and the page says what went wrong and offers to try again.
 * `SectionBoundary` does the same for a part of the shell itself — the
 * sidebar, the breadcrumb, the ⌘K menu — which would otherwise take every
 * page down with it.
 *
 * A render failure is most often data the console did not expect: a
 * substrate on an older version than the console answers with a shape it
 * predates. */

import { Component, type ErrorInfo, type ReactNode } from "react"
import { Link } from "@tanstack/react-router"
import { TriangleAlertIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { cn } from "@/lib/utils"

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export function PageError({
  error,
  reset,
}: {
  error: unknown
  reset: () => void
}) {
  return (
    <div className="px-4 py-10 md:px-8">
      <Empty className="rounded-[10px] border py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>This page couldn’t be shown</EmptyTitle>
          <EmptyDescription>
            Something on it didn’t load the way the console expected. If this
            substrate runs an older version than the console, updating it
            usually fixes this.
          </EmptyDescription>
          <p className="text-xs [overflow-wrap:anywhere] text-faint">
            {messageOf(error)}
          </p>
        </EmptyHeader>
        <EmptyContent>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={reset}>
              Try again
            </Button>
            <Button variant="ghost" size="sm" render={<Link to="/" />}>
              Go home
            </Button>
          </div>
        </EmptyContent>
      </Empty>
    </div>
  )
}

interface SectionBoundaryProps {
  /** What the section is, as the note's subject ("The sidebar"). */
  name: string
  /** A change clears a caught failure, so the section tries again (the
   * address, for a part of the shell that outlives every page). */
  resetKey?: unknown
  /** Where the note sits in its parent's layout (a grid row's span). */
  className?: string
  children: ReactNode
}

interface SectionBoundaryState {
  error?: unknown
  key?: unknown
}

export class SectionBoundary extends Component<
  SectionBoundaryProps,
  SectionBoundaryState
> {
  state: SectionBoundaryState = { key: this.props.resetKey }

  static getDerivedStateFromError(error: unknown): SectionBoundaryState {
    return { error }
  }

  static getDerivedStateFromProps(
    props: SectionBoundaryProps,
    state: SectionBoundaryState
  ): SectionBoundaryState | null {
    return props.resetKey !== state.key
      ? { error: undefined, key: props.resetKey }
      : null
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error(`${this.props.name} failed`, error, info.componentStack)
  }

  render() {
    if (this.state.error === undefined) return this.props.children
    return (
      <p
        role="alert"
        className={cn(
          "px-3 py-2 text-[12.5px] text-muted-foreground",
          this.props.className
        )}
      >
        {this.props.name} couldn’t be shown: {messageOf(this.state.error)}
      </p>
    )
  }
}
