/** The error boundary around every mounted view, so a layout that throws
 * takes its own rectangle and nothing beside it: not the overview a card sits
 * on, not the app the screen belongs to. Retry remounts the children. */

import { Component, type ErrorInfo, type ReactNode } from "react"
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

interface Props {
  children: ReactNode
  /** Which view failed, for the card and the console line. */
  label?: string
}

interface State {
  error?: Error
  /** Bumped by Retry so the subtree remounts fresh. */
  attempt: number
}

export class ViewBoundary extends Component<Props, State> {
  state: State = { attempt: 0 }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`view ${this.props.label ?? ""} failed`, error, info)
  }

  retry = () => {
    this.setState((s) => ({ error: undefined, attempt: s.attempt + 1 }))
  }

  render() {
    if (this.state.error) {
      return (
        <Empty className="py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TriangleAlertIcon />
            </EmptyMedia>
            <EmptyTitle>This view could not render</EmptyTitle>
            <EmptyDescription className="data break-words">
              {this.state.error.message}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" onClick={this.retry}>
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      )
    }
    return (
      <div key={this.state.attempt} className="contents">
        {this.props.children}
      </div>
    )
  }
}
