package vocabulary

import "testing"

// A derived minted id has the minted shape, is the same for the same parts
// and differs for different ones, including parts that join to one string.
func TestDerivedMintedID(t *testing.T) {
	a := DerivedMintedID("apple", "book-1", "contact-1")
	if !MintedID(a) || a != DerivedMintedID("apple", "book-1", "contact-1") {
		t.Fatalf("derived %q", a)
	}
	for _, other := range []string{
		DerivedMintedID("apple", "book-1", "contact-2"),
		DerivedMintedID("apple", "book-1contact-1"),
	} {
		if other == a {
			t.Fatalf("two different keys derived %q", a)
		}
	}
}

// MintedID holds the alphabet: no i, l, o or u, lowercase only, 26
// characters, the first 0 to 7.
func TestMintedIDShape(t *testing.T) {
	for id, want := range map[string]bool{
		"01h2xcejqtf2nbrexx3vqjhp41": true,
		"7zzzzzzzzzzzzzzzzzzzzzzzzz": true,
		"8zzzzzzzzzzzzzzzzzzzzzzzzz": false,
		"01h2xcejqtf2nbrexx3vqjhp4l": false,
		"01H2XCEJQTF2NBREXX3VQJHP41": false,
		"01h2xcejqtf2nbrexx3vqjhp4":  false,
		"floor-first-full-body":      false,
		// A UUID's canonical text, lowercase, is the apps' spelling.
		"0188bac7-4afa-78aa-bc3b-bd1eef28d881": true,
		"0188BAC7-4AFA-78AA-BC3B-BD1EEF28D881": false,
		"0188bac74afa78aabc3bbd1eef28d881":     false,
		"0188bac7-4afa-78aa-bc3b-bd1eef28d88":  false,
	} {
		if got := MintedID(id); got != want {
			t.Errorf("MintedID(%q) = %v, want %v", id, got, want)
		}
	}
}
