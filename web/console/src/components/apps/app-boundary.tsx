/** The error boundary around every mounted guest, so a host component that
 * throws takes its own rectangle and nothing beside it: not the overview a
 * card sits on, not the record page the card hangs off. Retry remounts the
 * children. (A throw INSIDE the guest never reaches here: the frame is
 * another document, and its errors arrive over the bridge.) */

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
  /** Which app failed, for the card and the console line. */
  label?: string
}

interface State {
  error?: Error
  /** Bumped by Retry so the subtree remounts fresh. */
  attempt: number
}

export class AppBoundary extends Component<Props, State> {
  state: State = { attempt: 0 }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`app ${this.props.label ?? ""} failed`, error, info)
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
            <EmptyTitle>This app could not render</EmptyTitle>
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
