package engine

import "testing"

// A title moves only where it is its rendering respelled: the same text, the
// name at one or more of the `~` that stand where a name stood.
func TestATitleIsItsRenderingRespelled(t *testing.T) {
	const name = "ada.example.com"
	for _, c := range []struct {
		stored, rendering string
		want              bool
	}{
		{name + "/tasks/run", "~/tasks/run", true},
		{"bundle:" + name + ":tasks", "bundle:~:tasks", true},
		{name, "~", true},
		{"ok: " + name + "/auto/stamp", "ok: ~/auto/stamp", true},
		{"substrate.reamde.dev/core/kind/" + name + "/tasks/task", "substrate.reamde.dev/core/kind/~/tasks/task", true},
		// A round moved part of it already.
		{"[~ a [" + name + " b ]]", "[~ a [~ b ]]", true},
		// Nothing to respell.
		{"~/tasks/run", "~/tasks/run", false},
		{"groceries", "groceries", false},
		// A title its writer gave, and a stale one.
		{"Notes about " + name + "/notes/memo", "~/notes/memo", false},
		{name + "/plan", "Note: ~/plan", false},
		{name + "/a and more", "~/a", false},
		// A `~` before `/` stands whatever precedes it: what precedes it is
		// the same text in both.
		{"x" + name + "/tasks/run", "x~/tasks/run", true},
		{"~ x" + name + "/tasks/run", "~ x~/tasks/run", true},
		// Any other `~` inside a longer word stands for no name.
		{name + "x", "~x", false},
		{"x" + name, "x~", false},
		// Another name.
		{"bob.example.com/tasks/run", "~/tasks/run", false},
	} {
		if got := spelledBefore(c.stored, c.rendering, name); got != c.want {
			t.Errorf("spelledBefore(%q, %q) = %v, want %v", c.stored, c.rendering, got, c.want)
		}
	}
}
