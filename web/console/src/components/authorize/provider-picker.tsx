import type { AuthorizationProviderInput } from "@/lib/api/types"

export function ProviderPicker({
  input,
  value,
  disabled,
  onChange,
}: {
  input: AuthorizationProviderInput
  value: string
  disabled: boolean
  onChange: (value: string) => void
}) {
  return (
    <div className="space-y-2 px-3 pb-3">
      <label className="flex flex-col gap-1 text-sm">
        <span className="break-all">
          AI provider for {input.package} / {input.input}
        </span>
        <select
          className="rounded border bg-background p-2"
          value={value}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
        >
          <option value="">Choose an AI provider</option>
          {input.options.map((option) => (
            <option key={option.id} value={option.id}>
              {option.label} — {Object.values(option.models).join(", ")}
            </option>
          ))}
        </select>
      </label>
      {input.agents.map((agent) => (
        <p
          key={agent.agent}
          className="text-xs break-all text-muted-foreground"
        >
          {agent.agent}: daily cap {agent.spendCentsPerDay} US cents.
          {value && (
            <>
              {" "}
              Model:{" "}
              {
                input.options.find((option) => option.id === value)?.models[
                  agent.agent
                ]
              }
              .
            </>
          )}
        </p>
      ))}
    </div>
  )
}
