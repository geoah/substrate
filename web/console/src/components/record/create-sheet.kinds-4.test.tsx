// @vitest-environment jsdom
/** File 4 of the every-kind create-sheet sweep; create-sheet.kinds.ts says
 * why it is cut in four. */

import { vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}))

import { sweepKinds } from "./create-sheet.kinds"

sweepKinds(4)
