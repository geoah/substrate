import { cn } from "@/lib/utils"

/** The title's type, for a heading a page draws itself (an input that edits
 * the title in place) so it reads the same as the one `PageHeader` draws. */
export function pageTitleClass(size: "page" | "record" = "page"): string {
  return cn(
    "text-balance [overflow-wrap:break-word]",
    size === "page"
      ? "text-[26px] leading-tight font-[650] tracking-[-0.02em]"
      : "text-[32px] leading-[1.15] font-bold tracking-[-0.025em]"
  )
}
