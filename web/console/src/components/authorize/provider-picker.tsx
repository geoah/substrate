/** The AI provider an approval binds a package's agents to (decision 0217):
 * one choice per bundle input, with the agents that run under it, the most
 * each may spend in a day and, once a provider is chosen, the model each
 * would use. The technical view adds the package's, the input's and each
 * agent's id. */

import { Mono } from "@/components/authorize/parts"
import type { AuthorizationProviderInput } from "@/lib/api/types"
import { providerInputName, spendWords } from "@/lib/app-setup"
import { displayName } from "@/lib/kind-names"

export function ProviderPicker({
  input,
  inputs = [input],
  value,
  disabled,
  technical = false,
  onChange,
}: {
  input: AuthorizationProviderInput
  /** Every provider input the page shows, so a package with more than one
   * names each. */
  inputs?: readonly AuthorizationProviderInput[]
  value: string
  disabled: boolean
  technical?: boolean
  onChange: (value: string) => void
}) {
  const chosen = input.options.find((option) => option.id === value)
  const id = `${input.package}/${input.input}`
  return (
    <div className="flex flex-col gap-2 text-xs">
      <label className="flex flex-col gap-1">
        <span className="font-medium [overflow-wrap:anywhere]">
          AI provider for {providerInputName(input, inputs)}
        </span>
        {technical && (
          <span className="text-muted-foreground">
            Input <Mono>{id}</Mono>
          </span>
        )}
        <select
          className="rounded-lg border border-input bg-background px-2.5 py-1.5 text-sm"
          value={value}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
        >
          <option value="">Choose an AI provider</option>
          {input.options.map((option) => (
            <option key={option.id} value={option.id}>
              {option.label}
            </option>
          ))}
        </select>
      </label>
      <ul className="flex flex-col gap-1">
        {input.agents.map((agent) => (
          <li key={agent.agent} className="flex flex-col gap-0.5">
            <span>
              <span className="font-medium">{displayName(agent.agent)}</span>
              <span className="text-muted-foreground">
                {" "}
                spends {spendWords(agent.spendCentsPerDay)}
              </span>
            </span>
            {chosen && (
              <span className="text-muted-foreground">
                Model <Mono>{chosen.models[agent.agent]}</Mono>
              </span>
            )}
            {technical && (
              <span className="text-muted-foreground">
                Agent <Mono>{agent.agent}</Mono>
              </span>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
