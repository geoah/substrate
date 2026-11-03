package domains

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestParseReadsTheKeysItKnowsAndKeepsTheRest(t *testing.T) {
	rec, found, err := Parse([]string{
		"google-site-verification=abc",
		"v=substrate1 server=https://Substrate.Example.com/ proof=k3j9x2 doc=https://cdn.example.net/a.json sha256=" +
			"9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08 future=yes",
	})
	if err != nil || !found {
		t.Fatalf("Parse: found %v, err %v", found, err)
	}
	if rec.Server != "https://substrate.example.com" {
		t.Errorf("Server = %q, want the origin lower-cased without its slash", rec.Server)
	}
	if rec.Proof != "k3j9x2" || rec.Doc != "https://cdn.example.net/a.json" {
		t.Errorf("Proof %q, Doc %q", rec.Proof, rec.Doc)
	}
	if rec.SHA256 != "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" {
		t.Errorf("SHA256 = %q, want lower-case hex", rec.SHA256)
	}
	if rec.Other["future"] != "yes" {
		t.Errorf("Other = %v, want the unknown key kept", rec.Other)
	}
}

func TestParseFindsNothingWithoutTheVersion(t *testing.T) {
	for _, txts := range [][]string{nil, {"v=spf1 -all"}, {"v=substrate10 server=https://a.example"}, {"v=substrate1x"}} {
		if _, found, err := Parse(txts); found || err != nil {
			t.Errorf("Parse(%q): found %v, err %v; want neither", txts, found, err)
		}
	}
}

func TestParseRefusesWhatCannotBeRead(t *testing.T) {
	for name, txts := range map[string][]string{
		"two records":        {"v=substrate1 server=https://a.example", "v=substrate1 server=https://b.example"},
		"no value":           {"v=substrate1 server="},
		"not key=value":      {"v=substrate1 server"},
		"a key twice":        {"v=substrate1 proof=a proof=b"},
		"http server":        {"v=substrate1 server=http://a.example"},
		"server with a path": {"v=substrate1 server=https://a.example/repo"},
		"http doc":           {"v=substrate1 doc=http://a.example/doc.json"},
		"short sha256":       {"v=substrate1 doc=https://a.example/d.json sha256=abcd"},
		"sha256 without doc": {"v=substrate1 sha256=9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
	} {
		if _, found, err := Parse(txts); !found || !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: found %v, err %v; want a malformed record", name, found, err)
		}
	}
}

func TestLookupAsksOnlyTheNamesOwnLabel(t *testing.T) {
	r := Static{
		"_substrate.example.org.": {"v=substrate1 doc=https://example.org/.well-known/substrate/authority.json"},
	}
	rec, found, err := Lookup(context.Background(), r, "Example.org.")
	if err != nil || !found || rec.Doc == "" {
		t.Fatalf("Lookup(example.org): %+v, found %v, err %v", rec, found, err)
	}
	// A parent's record says nothing about a name under it.
	if _, found, err := Lookup(context.Background(), r, "app.example.org"); found || err != nil {
		t.Errorf("Lookup(app.example.org): found %v, err %v; want no record", found, err)
	}
}

func TestLookupTellsAbsenceFromFailure(t *testing.T) {
	failing := resolverFunc(func(context.Context, string) ([]string, error) {
		return nil, &net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}
	})
	if _, found, err := Lookup(context.Background(), failing, "example.org"); found || err == nil {
		t.Errorf("a failed lookup: found %v, err %v; want an error", found, err)
	}
	for _, bad := range []string{"", "localhost", "https://example.org", "a b.example"} {
		if _, _, err := Lookup(context.Background(), Static{}, bad); err == nil {
			t.Errorf("Lookup(%q): want a refusal of the name", bad)
		}
	}
}

type resolverFunc func(context.Context, string) ([]string, error)

func (f resolverFunc) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return f(ctx, name)
}

// A record written with tabs or doubled spaces is still the record, and one
// that is read back renders every key it carried, the ones this binary does
// not read included, so a record rewritten from it loses nothing.
func TestARecordReadsWithAnyWhitespaceAndRendersWhatItCarries(t *testing.T) {
	rec, found, err := Parse([]string{"v=substrate1\tserver=https://substrate.example.net  proof=abc123\tlater=x"})
	if err != nil || !found {
		t.Fatalf("Parse = %v, %v", found, err)
	}
	if rec.Server != "https://substrate.example.net" || rec.Proof != "abc123" || rec.Other["later"] != "x" {
		t.Fatalf("Parse = %+v", rec)
	}
	if got, want := rec.String(), "v=substrate1 server=https://substrate.example.net proof=abc123 later=x"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
	if _, found, _ := Parse([]string{"v=substrate10 server=https://x.example"}); found {
		t.Fatal("a longer version word read as this one")
	}
}
