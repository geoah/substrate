/** "Add your OpenAI key": an agent whose AI model has no key cannot answer,
 * so where it would refuse, the console says so and links to that model's
 * card on Providers (`/providers?model=<row id>`), whose key form opens
 * there. The key is written in one place, the card. */

import { Link } from "@tanstack/react-router"
import { KeyRoundIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { providerName } from "@/lib/agent-chat"

/** A button-styled link to the model's card: the words say which key. */
export function AddKeyButton({
  providerId,
  name,
  size = "sm",
  variant = "outline",
}: {
  /** The llm/provider row's id (`openai`). */
  providerId: string
  /** The row's name as its card shows it; the id's otherwise. */
  name?: string
  size?: "sm" | "xs"
  variant?: "outline" | "default" | "ghost"
}) {
  return (
    <Button
      size={size}
      variant={variant}
      render={
        <Link to="/providers" search={{ model: providerId }}>
          <KeyRoundIcon />
          Add your {name ?? providerName(providerId)} key
        </Link>
      }
    />
  )
}

/** "Runs on OpenAI, gpt-5": the model's card is a link away. */
export function RunsOnLine({
  providerId,
  name,
  model,
}: {
  providerId?: string
  name?: string
  model?: string
}) {
  if (!providerId) return model ? <>Runs on {model}</> : null
  return (
    <>
      Runs on{" "}
      <Link
        to="/providers"
        search={{ model: providerId }}
        className="text-primary-text underline-offset-2 hover:underline"
      >
        {name ?? providerName(providerId)}
      </Link>
      {model ? `, ${model}` : ""}
    </>
  )
}
