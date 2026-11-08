import { defineConfig } from "vitest/config"

// One run over every web package's tests. jsdom gives the DOM the React
// pieces and the sign-in read; fake-indexeddb stands in for the browser's
// storage (kit/web/testing/setup.ts).
export default defineConfig({
  test: {
    include: [
      "kit/web/*/src/**/*.test.{ts,tsx}",
      "template/web/src/**/*.test.{ts,tsx}",
    ],
    environment: "jsdom",
    setupFiles: ["kit/web/testing/setup.ts"],
    restoreMocks: true,
  },
})
