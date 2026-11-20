/** A read the screens show: what the kit's `useRead` answers, and what the
 * demo answers in its place, so a screen never knows which it has. */
export interface Read<T> {
  data: T | undefined
  error: unknown
  loading: boolean
  refreshing: boolean
  refresh: () => void
}

const noop = () => {}

/** A read that has its answer: the demo's, or a value derived in place. */
export function settled<T>(data: T): Read<T> {
  return {
    data,
    error: undefined,
    loading: false,
    refreshing: false,
    refresh: noop,
  }
}

/** A read with nothing yet. */
export function pending<T>(): Read<T> {
  return {
    data: undefined,
    error: undefined,
    loading: true,
    refreshing: false,
    refresh: noop,
  }
}
