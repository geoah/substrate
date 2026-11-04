import { MoonIcon, SunIcon, SunMoonIcon, type LucideIcon } from "lucide-react"

import { useOptionalTheme, type Theme } from "@/components/theme-provider"
import { buttonVariants } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { markSignedOutTheme } from "@/lib/console-preferences"
import { cn } from "@/lib/utils"

/** The same three choices, labels and icons as the account menu's
 * Appearance group, so a visitor meets one vocabulary before and after
 * signing in. */
const CHOICES = [
  { value: "system", label: "System", Icon: SunMoonIcon },
  { value: "light", label: "Light", Icon: SunIcon },
  { value: "dark", label: "Dark", Icon: MoonIcon },
] as const satisfies readonly {
  value: Theme
  label: string
  Icon: LucideIcon
}[]

/** The appearance switch for the signed-out pages (the landing page and the
 * doors), where no account menu exists. The choice lives in this browser
 * only, and is marked so the first signed-in console carries it onto the
 * repository's preference instead of following the stored one. Renders
 * nothing outside a `ThemeProvider`. */
export function ColorSchemeMenu({ className }: { className?: string }) {
  const theme = useOptionalTheme()
  if (!theme) return null
  const current = CHOICES.find((c) => c.value === theme.theme) ?? CHOICES[0]
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={`Appearance: ${current.label}`}
        title="Appearance"
        className={cn(
          buttonVariants({ variant: "ghost", size: "icon-lg" }),
          "text-muted-foreground",
          className
        )}
      >
        <current.Icon />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="min-w-40" align="end" sideOffset={4}>
        <DropdownMenuRadioGroup
          value={theme.theme}
          onValueChange={(value) => {
            const next = value as Theme
            markSignedOutTheme(next)
            theme.setTheme(next)
          }}
        >
          <DropdownMenuLabel className="text-[11.5px] font-normal text-faint">
            Appearance
          </DropdownMenuLabel>
          {CHOICES.map(({ value, label, Icon }) => (
            <DropdownMenuRadioItem key={value} value={value}>
              <Icon /> {label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
