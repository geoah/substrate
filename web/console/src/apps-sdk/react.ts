/** `react` inside the guest: the console's own React, re-exported as an
 * entry the import map names, so the app, the SDK and the kit share ONE
 * instance with the console's chunk. Two copies would break hooks.
 *
 * The names are spelled out because React is a CommonJS package: vite's dev
 * interop rewrites a named re-export from one into a property read, and a
 * `export *` into nothing at all. `react.test.ts` holds this list to the
 * package's own export names (the `unstable_` ones excepted, which the types
 * do not carry either), so a React upgrade that adds one fails there. */
export {
  Activity,
  Children,
  Component,
  Fragment,
  Profiler,
  PureComponent,
  StrictMode,
  Suspense,
  act,
  cache,
  cacheSignal,
  captureOwnerStack,
  cloneElement,
  createContext,
  createElement,
  createRef,
  forwardRef,
  isValidElement,
  lazy,
  memo,
  startTransition,
  use,
  useActionState,
  useCallback,
  useContext,
  useDebugValue,
  useDeferredValue,
  useEffect,
  useEffectEvent,
  useId,
  useImperativeHandle,
  useInsertionEffect,
  useLayoutEffect,
  useMemo,
  useOptimistic,
  useReducer,
  useRef,
  useState,
  useSyncExternalStore,
  useTransition,
  version,
} from "react"
export { default } from "react"
