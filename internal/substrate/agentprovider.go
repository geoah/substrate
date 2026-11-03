package substrate

// AuthorizationProviderInput is an owner's choice for one package input.
// Every offered row must price a model for every agent consuming that input.
type AuthorizationProviderInput struct {
	Package  string                        `json:"package"`
	Input    string                        `json:"input"`
	Agents   []AuthorizationProviderAgent  `json:"agents"`
	Options  []AuthorizationProviderOption `json:"options"`
	Selected string                        `json:"selected,omitempty"`
}

type AuthorizationProviderAgent struct {
	Agent            string   `json:"agent"`
	Models           []string `json:"models"`
	SpendCentsPerDay int      `json:"spendCentsPerDay"`
}

type AuthorizationProviderOption struct {
	ID     string            `json:"id"`
	Label  string            `json:"label"`
	Models map[string]string `json:"models"`
}

type AuthorizationProviderChoice struct {
	Package  string `json:"package"`
	Input    string `json:"input"`
	Provider string `json:"provider"`
}

const BlockNoModel = "no-model"
