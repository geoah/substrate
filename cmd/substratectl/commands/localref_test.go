package commands

import "testing"

// A kind argument reads as the server answers it (decision record 0259): an
// unquoted `~/tasks/task` the shell expanded to the home directory is `~`
// again, the context's repository name is another spelling of `~`, and
// anything else is left as it was typed.
func TestALocalKindArgumentReadsAsTheServerAnswersIt(t *testing.T) {
	t.Setenv("HOME", "/home/ada")
	a := &app{ctx: &Context{Name: "default", Repository: "ada.example.com"}}
	for in, want := range map[string]string{
		"/home/ada/tasks/task":                          "~/tasks/task",
		"~/tasks/task":                                  "~/tasks/task",
		"ada.example.com/tasks/task":                    "~/tasks/task",
		"providers.substrate.reamde.dev/google/account": "providers.substrate.reamde.dev/google/account",
		"/home/ada":                                     "/home/ada",
		"/home/adam/tasks/task":                         "/home/adam/tasks/task",
	} {
		if got := a.localRef(in); got != want {
			t.Errorf("localRef(%q) = %q, want %q", in, got, want)
		}
	}
}
