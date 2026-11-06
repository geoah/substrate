/** A write whose input carries a secret (an API key, a client secret, a
 * sealed setting) that leaves nothing of the input in TanStack's mutation
 * cache. TanStack keeps a mutation for its gc time after the form that sent
 * it closed: its `variables` in plain text, and its `mutationFn`, which
 * holds whatever form values it closed over. Devtools, or code reading the
 * query client, can read either back.
 *
 * Here the input rides a ref the request reads once and clears, the
 * mutation takes no variables, and the settled mutation is removed from the
 * cache whether the write succeeded or failed. The callbacks are TanStack's,
 * so a pending state lasts until `onSuccess` resolves; they get the result
 * and never the input. The returned object is useMutation's, with `mutate`
 * and `mutateAsync` taking the input; `variables` is always undefined. */

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
  UseMutationResult<TData, Error, void>,
  "mutate" | "mutateAsync"
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
  const held = useRef<{ input: TInput } | null>(null)
  const key = [...mutationKey, useId()]
  const mutation = useMutation<TData, Error, void>({
    mutationKey: key,
    mutationFn: async () => {
      const sent = held.current
      held.current = null
      if (!sent) throw new Error("There is no value to save.")
      return mutationFn(sent.input)
    },
    onMutate: onMutate && (() => void onMutate()),
    onSuccess: onSuccess && ((data) => onSuccess(data)),
    onError: onError && ((error) => onError(error)),
  })
  async function mutateAsync(input: TInput): Promise<TData> {
    held.current = { input }
    try {
      return await mutation.mutateAsync()
    } finally {
      held.current = null
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
