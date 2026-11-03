// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { CodeChanges, OAuthHosts } from "./outside-code"

afterEach(cleanup)
it("labels OAuth hosts and preserves full names", () => {
  render(<OAuthHosts hosts={["authorization.provider.example.com"]} />)
  expect(screen.getByText("OAuth endpoint hosts")).toBeTruthy()
  expect(screen.getByText("authorization.provider.example.com")).toBeTruthy()
})
it("shows source and permission diffs under the full declaration identity", () => {
  render(
    <CodeChanges
      changes={[
        {
          kind: "function",
          id: "publisher.example.com/outside/probe",
          diff: "- old source\n+ new source",
        },
      ]}
    />
  )
  expect(
    screen.getByText("function: publisher.example.com/outside/probe")
  ).toBeTruthy()
  expect(screen.getByText(/old source/)).toBeTruthy()
})
