import js from "@eslint/js"
import globals from "globals"
import configPrettier from "eslint-config-prettier"
import reactHooks from "eslint-plugin-react-hooks"
import reactRefresh from "eslint-plugin-react-refresh"
import tseslint from "typescript-eslint"
import { defineConfig, globalIgnores } from "eslint/config"

// The import layering, one pattern per rule. `no-restricted-imports` is
// replaced, not merged, by a later block that sets it again, so every block
// below lists every pattern that applies to its files.
//
// The patterns read the import string, not the file it resolves to, so they
// hold only while an import that leaves its own directory spells the `@/`
// alias. A `../` import would walk past every one of them.
const PARENT_RELATIVE = {
  regex: "^\\.\\./",
  message: "Import from outside this directory through the `@/` alias.",
}
// The raw HTTP envelope belongs to src/lib/api: a module outside it reads and
// writes through a lib/api function, so a route has one caller to change and
// one place a test can stub. Tests stub `request` itself and are exempt.
// `./api/http` (from src/lib) and `./lib/api/http` (from src) are the same
// file under its relative spellings.
const RAW_REQUEST = {
  regex: "^(@/lib|\\.(/lib)?)/api/http(\\.ts)?$",
  importNames: ["request"],
  message:
    "Call the server through a function in src/lib/api, not the raw `request` envelope.",
}
// src/lib is below every component, hook and page, and the router is the
// top: a lib module that imports one of them is a cycle waiting for a second
// edge.
const ABOVE_LIB = {
  regex: "^@/(components|hooks|pages)/|^@/router(\\.tsx)?$",
  message:
    "src/lib sits below components, hooks, pages and the router: move the shared code down into src/lib instead.",
}
// Components and hooks are what pages are built from, so they never reach
// back up into a page or the route tree.
const ABOVE_COMPONENTS = {
  regex: "^@/pages/|^@/router(\\.tsx)?$",
  message:
    "Components and hooks sit below pages and the router: pass the value in as a prop or move the shared code down.",
}

export default defineConfig([
  globalIgnores(["dist"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommendedTypeChecked,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
      // LAST, so it wins: it switches off every rule that only argues about
      // layout. Prettier decides layout here, and two tools with an opinion
      // about the same comma is how a lint job starts contradicting a
      // formatter.
      configPrettier,
    ],
    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        // The type-checked rules read each file through the tsconfig that
        // includes it: tsconfig.app.json for src, tsconfig.node.json for
        // vite.config.ts.
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // TanStack Router redirects by throwing `redirect(...)` from a
      // `beforeLoad`, and the value it throws is a Response, not an Error.
      "@typescript-eslint/only-throw-error": [
        "error",
        {
          allow: [
            {
              from: "package",
              package: "@tanstack/router-core",
              name: "Redirect",
            },
          ],
        },
      ],
      // React discards what an event handler returns, so an async `onSubmit`
      // is fine; a promise handed where a value is READ is still refused.
      "@typescript-eslint/no-misused-promises": [
        "error",
        { checksVoidReturn: { attributes: false } },
      ],
      "no-restricted-imports": [
        "error",
        { patterns: [PARENT_RELATIVE, RAW_REQUEST] },
      ],
    },
  },
  {
    files: ["src/lib/**/*.{ts,tsx}"],
    ignores: ["src/lib/api/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [PARENT_RELATIVE, RAW_REQUEST, ABOVE_LIB] },
      ],
    },
  },
  {
    files: ["src/lib/api/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [PARENT_RELATIVE, ABOVE_LIB] },
      ],
    },
  },
  {
    files: ["src/components/**/*.{ts,tsx}", "src/hooks/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [PARENT_RELATIVE, RAW_REQUEST, ABOVE_COMPONENTS] },
      ],
    },
  },
  {
    // Tests build fixtures from loose JSON and mocks, write async callbacks a
    // harness awaits whether or not they await anything, stub `request` to
    // stand in for the server, and may mount a page around the component
    // under test. The layering holds what ships, not what tests reach for.
    files: ["**/*.test.{ts,tsx}"],
    rules: {
      "@typescript-eslint/require-await": "off",
      "@typescript-eslint/no-base-to-string": "off",
      "@typescript-eslint/no-unsafe-argument": "off",
      "@typescript-eslint/no-unsafe-assignment": "off",
      "@typescript-eslint/no-unsafe-call": "off",
      "@typescript-eslint/no-unsafe-member-access": "off",
      "@typescript-eslint/no-unsafe-return": "off",
      "no-restricted-imports": "off",
    },
  },
  {
    // The render check runs under node, not in the browser the rest of this
    // tree targets.
    files: ["scripts/**/*.mjs"],
    extends: [js.configs.recommended, configPrettier],
    languageOptions: {
      globals: globals.node,
    },
  },
  {
    // Registry-managed code (shadcn CLI output). Never forked by hand, so its
    // style is the registry's business, not this lint's.
    files: ["src/components/ui/**/*.{ts,tsx}", "src/hooks/use-mobile.ts"],
    rules: {
      "react-refresh/only-export-components": "off",
      "react-hooks/set-state-in-effect": "off",
    },
  },
])
