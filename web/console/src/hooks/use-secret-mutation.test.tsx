// @vitest-environment jsdom
/** useSecretMutation sends each call's own input, however the calls
 * overlap, and leaves nothing of any input in the mutation cache: not while
 * a request is open, not after it settles, and not when the form that sent
 * it is gone before it settles. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"

import { useSecretMutation } from "./use-secret-mutation"
import {
  expectNoTrace,
  mutationText,
} from "./use-secret-mutation.test-fixtures"

/** A request the test settles by hand, one per call, in call order. */
function openRequests() {
  const sent: string[] = []
  const open: {
    resolve: (saved: string) => void
    reject: (error: Error) => void
  }[] = []
  const mutationFn = (input: string) => {
    sent.push(input)
    return new Promise<string>((resolve, reject) => {
      open.push({ resolve, reject })
    })
  }
  return { sent, open, mutationFn }
}

function setup(mutationFn: (input: string) => Promise<string>) {
  const client = new QueryClient()
  const onSuccess = vi.fn()
  const onError = vi.fn()
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const view = renderHook(
    () =>
      useSecretMutation({
        mutationKey: ["secret-test"],
        mutationFn,
        onSuccess,
        onError,
      }),
    { wrapper }
  )
  return { client, onSuccess, onError, ...view }
}

describe("useSecretMutation", () => {
  it("sends each of two calls made in the same tick with its own input", async () => {
    const sent: string[] = []
    const { client, result, onSuccess } = setup(async (input) => {
      sent.push(input)
      return `saved ${input}`
    })

    let first!: Promise<string>
    let second!: Promise<string>
    act(() => {
      first = result.current.mutateAsync("sk-first")
      second = result.current.mutateAsync("sk-second")
    })

    await expect(first).resolves.toBe("saved sk-first")
    await expect(second).resolves.toBe("saved sk-second")
    expect(sent).toEqual(["sk-first", "sk-second"])
    expect(onSuccess.mock.calls).toEqual([
      ["saved sk-first"],
      ["saved sk-second"],
    ])
    expectNoTrace(client, "sk-first")
    expectNoTrace(client, "sk-second")
  })

  it("sends a call made while another's request is open with its own input, and holds neither in the cache", async () => {
    const requests = openRequests()
    const { client, result, onError } = setup(requests.mutationFn)

    let first!: Promise<string>
    act(() => {
      first = result.current.mutateAsync("sk-first")
    })
    await waitFor(() => expect(requests.sent).toEqual(["sk-first"]))
    expect(mutationText(client)).not.toContain("sk-first")

    let second!: Promise<string>
    act(() => {
      second = result.current.mutateAsync("sk-second")
    })
    await waitFor(() =>
      expect(requests.sent).toEqual(["sk-first", "sk-second"])
    )
    expect(mutationText(client)).not.toContain("sk-")
    expect(client.getMutationCache().getAll()).toHaveLength(2)

    // The first is refused while the second is still open: the refusal
    // removes the first and leaves the second pending.
    act(() => requests.open[0].reject(new Error("version mismatch")))
    await expect(first).rejects.toThrow("version mismatch")
    expect(onError).toHaveBeenCalledTimes(1)
    expect(
      client
        .getMutationCache()
        .getAll()
        .map((m) => m.state.status)
    ).toEqual(["pending"])

    act(() => requests.open[1].resolve("saved sk-second"))
    await expect(second).resolves.toBe("saved sk-second")
    expectNoTrace(client, "sk-first")
    expectNoTrace(client, "sk-second")
  })

  it("removes a write that settles after its form is gone", async () => {
    const requests = openRequests()
    const { client, result, unmount } = setup(requests.mutationFn)

    let saved!: Promise<string>
    let refused!: Promise<string>
    act(() => {
      saved = result.current.mutateAsync("sk-saved")
    })
    await waitFor(() => expect(requests.sent).toEqual(["sk-saved"]))
    act(() => {
      refused = result.current.mutateAsync("sk-refused")
    })
    await waitFor(() =>
      expect(requests.sent).toEqual(["sk-saved", "sk-refused"])
    )
    unmount()
    expect(mutationText(client)).not.toContain("sk-")

    requests.open[0].resolve("saved sk-saved")
    requests.open[1].reject(new Error("version mismatch"))
    await expect(saved).resolves.toBe("saved sk-saved")
    await expect(refused).rejects.toThrow("version mismatch")
    expectNoTrace(client, "sk-saved")
    expectNoTrace(client, "sk-refused")
  })
})
