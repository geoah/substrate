package vocabulary

import "strings"

// THE CONTROL KINDS (decision 0276): the kinds whose records decide what code
// runs, what it may do, what the vocabulary is, and who may do what. A rule
// of a role reaches one only by naming it exactly, never by a glob or a
// trait, and the role `admin` reaches every one by being `admin`. This is the
// one list, by class; the table of actor types against control kinds tests
// every class, and fails on a class with no row.
//
// Sign-in material (AuthKinds, decision 0080) is held the same way beside
// them: no glob reaches it, a rule has to name it.

// ControlClass is one class of control kinds.
type ControlClass string

const (
	// ControlTriggers are when code runs.
	ControlTriggers ControlClass = "triggers"
	// ControlPolicies are which writes of code wait for a person. The change
	// requests a policy holds are not control kinds: a request is the gate,
	// writes nothing until it is decided, and its accept writes the target
	// as the hand that decides, held to that hand's rules.
	ControlPolicies ControlClass = "policies"
	// ControlProviders are the AI providers an agent spends on, and the
	// models they offer.
	ControlProviders ControlClass = "providers"
	// ControlSettings are a package's settings: its setting records, the
	// kinds its bundle takes as inputs, and the accounts that connect it.
	ControlSettings ControlClass = "settings"
	// ControlApplets are the applets, which run reads and writes under the
	// token of whoever opens them.
	ControlApplets ControlClass = "applets"
	// ControlDeclarations are the vocabulary and the code it installs.
	ControlDeclarations ControlClass = "declarations"
	// ControlActors are the hands that write.
	ControlActors ControlClass = "actors"
	// ControlRoles are what each person may do.
	ControlRoles ControlClass = "roles"
)

// ControlClasses are every class, in the order the docs list them.
var ControlClasses = []ControlClass{
	ControlTriggers, ControlPolicies, ControlProviders, ControlSettings,
	ControlApplets, ControlDeclarations, ControlActors, ControlRoles,
}

// controlKinds are the control kinds named one by one; the settings a
// package's bundle takes as inputs, the accounts and every applets kind are
// found by what they are (ControlClassOf).
var controlKinds = map[string]ControlClass{
	CoreKind("trigger"):       ControlTriggers,
	KindRecordPatchPolicy:     ControlPolicies,
	LLMKind("provider"):       ControlProviders,
	LLMKind("model"):          ControlProviders,
	CoreKind("setting"):       ControlSettings,
	CoreKind("kind"):          ControlDeclarations,
	CoreKind("package"):       ControlDeclarations,
	CoreKind("authority"):     ControlDeclarations,
	CoreKind("trait"):         ControlDeclarations,
	CoreKind("propertytype"):  ControlDeclarations,
	CoreKind("recordmapping"): ControlDeclarations,
	CoreKind("function"):      ControlDeclarations,
	CoreKind("agent"):         ControlDeclarations,
	CoreKind("bundle"):        ControlDeclarations,
	// A repository's override of how long a kind's history is kept
	// (decision 0282) stands in for a key of the kind's declaration, and
	// shortening it removes history: an admin's, as the declaration is.
	CoreKind("retention"): ControlDeclarations,
	CoreKind("actor"):     ControlActors,
	CoreKind("role"):      ControlRoles,
}

// ControlClassOf is the control class of a kind, "" for a kind that is no
// control kind. bundleInput is whether an installed bundle takes the kind as
// one of its inputs, which the bundle says and the kind does not.
func ControlClassOf(k *Kind, bundleInput bool) ControlClass {
	if c, ok := controlKinds[k.Identity]; ok {
		return c
	}
	switch {
	case strings.HasPrefix(k.Identity, PackageApplets+"/"):
		return ControlApplets
	case bundleInput, k.Implements(TraitAccountConfigCore):
		return ControlSettings
	}
	return ""
}

// ControlSignIn is how a kind's description names sign-in material beside the
// control classes.
const ControlSignIn = "sign-in"

// SignInMaterial reports whether a kind holds sign-in material, which no
// glob reaches (decision 0080).
func SignInMaterial(kind string) bool { return AuthKinds[kind] }
