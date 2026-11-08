import js from "@eslint/js"
import configPrettier from "eslint-config-prettier"
import reactHooks from "eslint-plugin-react-hooks"
import { defineConfig, globalIgnores } from "eslint/config"
import globals from "globals"
import tseslint from "typescript-eslint"

// The web kit's layering, one pattern per rule: client is below everything,
// auth and local sit on client alone, and only react knows React. An app
// reaches the kit through its package names, never a path into it.
// `no-restricted-imports` is replaced, not merged, by a later block that sets
// it again, so every block lists every pattern that applies to its files.
const INTO_KIT = {
  regex: "^(\\.\\./)+.*kit/web/",
  message:
    "Import the kit by its package name (@substrate/…), not a path into it.",
}
const REACT = {
  regex: "^(react|react-dom|@substrate/react)(/|$)",
  message:
    "Only @substrate/react knows React: keep this package framework-free.",
}
const ABOVE_CLIENT = {
  regex: "^@substrate/(auth|local|react)$",
  message: "@substrate/client is below every other kit package.",
}
const SIBLING = {
  regex: "^@substrate/(auth|local)$",
  message: "auth and local each sit on client alone, beside each other.",
}

export default defineConfig([
  globalIgnores(["**/dist", "**/node_modules"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommendedTypeChecked,
      reactHooks.configs.flat.recommended,
      // LAST, so it wins: it switches off every rule that only argues about
      // layout, which Prettier decides.
      configPrettier,
    ],
    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        // Each file is read through the tsconfig that includes it; the
        // build configs, which none does, through a default project.
        projectService: {
          allowDefaultProject: [
            "vitest.config.ts",
            "*/web/vite.config.ts",
            "kit/web/vite.lib.ts",
          ],
        },
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // React discards what an event handler returns, so an async handler
      // is fine; a promise handed where a value is READ is still refused.
      "@typescript-eslint/no-misused-promises": [
        "error",
        { checksVoidReturn: { attributes: false } },
      ],
      "no-restricted-imports": ["error", { patterns: [INTO_KIT] }],
    },
  },
  {
    files: ["kit/web/client/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [INTO_KIT, REACT, ABOVE_CLIENT] },
      ],
    },
  },
  {
    files: ["kit/web/{auth,local,testing}/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": ["error", { patterns: [INTO_KIT, REACT] }],
    },
  },
  {
    files: ["kit/web/auth/src/**/*.ts", "kit/web/local/src/**/*.ts"],
    ignores: ["**/*.test.ts"],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [INTO_KIT, REACT, SIBLING] },
      ],
    },
  },
  {
    // Tests build fixtures from loose JSON and mocks, and write async
    // callbacks a harness awaits whether or not they await anything.
    files: ["**/*.test.{ts,tsx}", "kit/web/testing/**/*.ts"],
    rules: {
      "@typescript-eslint/require-await": "off",
      "@typescript-eslint/no-non-null-assertion": "off",
      "@typescript-eslint/no-unsafe-argument": "off",
      "@typescript-eslint/no-unsafe-assignment": "off",
      "@typescript-eslint/no-unsafe-member-access": "off",
      "@typescript-eslint/unbound-method": "off",
    },
  },
  {
    files: ["*/web/vite.config.ts", "vitest.config.ts"],
    languageOptions: { globals: globals.node },
  },
])
