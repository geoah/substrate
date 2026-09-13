/** The three React shims re-export by name (CommonJS interop leaves a star
 * empty in dev), so each list is held to the package's own export names as
 * CommonJS reports them: an upgrade that adds a name fails here until the
 * shim carries it, and a guest never meets a `react` missing a hook the
 * console has. What the types do not carry (`unstable_*`, react-dom's
 * `version`) is not the contract and is left out on both sides; the lists
 * are read through the same interop the console uses. */

import { describe, expect, it } from "vitest"

import * as runtime from "react/jsx-runtime"
import * as client from "react-dom/client"
import * as react from "react"

import * as shimJsxRuntime from "./jsx-runtime"
import * as shimReact from "./react"
import * as shimReactDomClient from "./react-dom-client"

/** vitest's CommonJS interop adds a `module.exports` key to a namespace;
 * it is not an export. */
const publicNames = (mod: object, skip: string[] = []) =>
  Object.keys(mod)
    .filter(
      (k) =>
        !k.startsWith("__") &&
        !k.startsWith("unstable_") &&
        k !== "default" &&
        k !== "module.exports" &&
        !skip.includes(k)
    )
    .sort()

describe("the React shims", () => {
  it("re-export every public name of react, and the same objects", () => {
    expect(publicNames(shimReact)).toEqual(publicNames(react))
    expect(shimReact.useState).toBe(react.useState)
    expect(shimReact.default.useState).toBe(react.useState)
  })

  it("re-export every public name of react/jsx-runtime", () => {
    expect(publicNames(shimJsxRuntime)).toEqual(publicNames(runtime))
    expect(shimJsxRuntime.jsx).toBe(runtime.jsx)
  })

  it("re-export every public name of react-dom/client", () => {
    expect(publicNames(shimReactDomClient)).toEqual(
      publicNames(client, ["version"])
    )
    expect(shimReactDomClient.createRoot).toBe(client.createRoot)
  })
})
