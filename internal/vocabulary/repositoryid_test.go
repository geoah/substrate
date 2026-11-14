package vocabulary

import "testing"

// A minted repository id is in its grammar, is never an authority, and two
// mints differ.
func TestARepositoryIDIsNeverAName(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 64 {
		id, err := NewRepositoryID()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidRepositoryID(id) || ValidRepositoryAuthority(id) || ValidAuthority(id) {
			t.Fatalf("%s: valid id %v, authority %v", id, ValidRepositoryID(id), ValidAuthority(id))
		}
		if seen[id] {
			t.Fatalf("%s minted twice", id)
		}
		seen[id] = true
	}
	for _, s := range []string{"", "ada.example.com", "rnd-", "rnd-0123456789abcdefghjkmnpqr", "RND-01jbx9m2qz7k4c5v8n3w6t1hrd", "rnd-81jbx9m2qz7k4c5v8n3w6t1hrd", "rnd-01jbx9m2qz7k4c5v8n3w6t1hr"} {
		if ValidRepositoryID(s) {
			t.Errorf("%q passed as a repository id", s)
		}
	}
	if !ValidRepositoryID("rnd-01jbx9m2qz7k4c5v8n3w6t1hrd") {
		t.Error("a well-formed id was refused")
	}
}
