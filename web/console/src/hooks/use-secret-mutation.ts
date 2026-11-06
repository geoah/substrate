/** A write whose input carries a secret (an API key, a client secret, a
 * sealed setting) that leaves nothing of the input in TanStack's mutation
 * cache. TanStack keeps a mutation for its gc time after the form that sent
 * it closed: its `variables` in plain text, and its `mutationFn`, which
 * holds whatever form values it closed over. Devtools, or code reading the
 * query client, can read either back.
 *
 * Here each call puts its input in a ref under a new ticket number, the
 * mutation's variables are that number, and the request takes the input
 * out of the ref once. Each call sends its own input, even when a second
 * call comes before TanStack starts the first one's request. The settled
 * mutation is removed from the cache whether the write succeeded or
 * failed. The callbacks are TanStack's, so a pending state lasts until
 * `onSuccess` resolves; they get the result and never the input. The
 * returned object is useMutation's, with `mutate` and `mutateAsync` taking
 * the input and no `variables`. */

import { useId, useRef } from "react"
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query"

export interface SecretMutationOptions<TInput, TData> {
  /** The mutation's key; one segment unique to this hook is appended, so
   * removing a settled write never touches another surface's. */
  mutationKey: readonly unknown[]
  mutationFn: (input: TInput) => Promise<TData>
  onMutate?: () => unknown
  onSuccess?: (data: TData) => unknown
  onError?: (error: Error) => unknown
}

/** Omit over each member of a union, so `isSuccess` still narrows `data`. */
type OmitEach<T, K extends PropertyKey> = T extends unknown ? Omit<T, K> : never

export type SecretMutationResult<TInput, TData> = OmitEach<
  UseMutationResult<TData, Error, number>,
  "mutate" | "mutateAsync" | "variables"
> & {
  /** Sends the input; a refusal reaches `onError` and `error`, never a
   * rejection. */
  mutate: (input: TInput) => void
  mutateAsync: (input: TInput) => Promise<TData>
}

export function useSecretMutation<TInput, TData>({
  mutationKey,
  mutationFn,
  onMutate,
  onSuccess,
  onError,
}: SecretMutationOptions<TInput, TData>): SecretMutationResult<TInput, TData> {
  const client = useQueryClient()
  const held = useRef(new Map<number, TInput>())
  const tickets = useRef(0)
  const key = [...mutationKey, useId()]
  const mutation = useMutation<TData, Error, number>({
    mutationKey: key,
    mutationFn: async (ticket) => {
      const waiting = held.current
      if (!waiting.has(ticket)) throw new Error("There is no value to save.")
      const input = waiting.get(ticket) as TInput
      waiting.delete(ticket)
      return mutationFn(input)
    },
    onMutate: onMutate && (() => void onMutate()),
    onSuccess: onSuccess && ((data) => onSuccess(data)),
    onError: onError && ((error) => onError(error)),
  })
  async function mutateAsync(input: TInput): Promise<TData> {
    const ticket = (tickets.current += 1)
    held.current.set(ticket, input)
    try {
      return await mutation.mutateAsync(ticket)
    } finally {
      held.current.delete(ticket)
      const cache = client.getMutationCache()
      for (const m of cache.findAll({ mutationKey: key, exact: true })) {
        if (m.state.status !== "pending") cache.remove(m)
      }
    }
  }
  return {
    ...mutation,
    mutateAsync,
    mutate: (input) => void mutateAsync(input).catch(() => {}),
  }
}
