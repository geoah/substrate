// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { PublisherDetails } from "./publisher"

afterEach(cleanup)

it("shows publisher identity and the document URL", () => {
  render(
    <PublisherDetails
      publisher={{
        authority: "publisher.example",
        name: "Notes",
        documentUrl: "https://publisher.example/authority.json",
      }}
    />
  )
  expect(screen.getByText("Notes · publisher.example")).toBeTruthy()
  expect(screen.getByRole("link").getAttribute("href")).toBe(
    "https://publisher.example/authority.json"
  )
})

it("identifies a development document as a file on this server", () => {
  render(
    <PublisherDetails
      publisher={{
        authority: "publisher.example",
        documentUrl: "file:/tmp/authority.json",
        fromFile: true,
      }}
    />
  )
  expect(screen.getByText("from a file on this server")).toBeTruthy()
  expect(screen.queryByRole("link")).toBeNull()
})
