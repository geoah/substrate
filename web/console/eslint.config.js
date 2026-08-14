import js from "@eslint/js"
import globals from "globals"
import configPrettier from "eslint-config-prettier"
import reactHooks from "eslint-plugin-react-hooks"
import reactRefresh from "eslint-plugin-react-refresh"
import tseslint from "typescript-eslint"
import { defineConfig, globalIgnores } from "eslint/config"

export default defineConfig([
  globalIgnores(["dist"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
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
