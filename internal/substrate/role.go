package substrate

// RoleRules is one role a person holds, as the API reads it to hold the
// person's requests to it (decision 0275): the role record's id and its
// rules, each a list of verbs over a list of kind patterns.
type RoleRules struct {
	ID    string
	Rules []RoleRule
}

// RoleRule is one rule of a role.
type RoleRule struct {
	Verbs []string
	Kinds []string
}
