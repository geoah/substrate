package vocabulary_test

// `mediaTypes` is reserved on the terms `unique` and `deprecated` are
// (reserved_test.go): admitted on a blob-ref, validated, stored in the
// definition, and read by no write. A client reads it to pick a
// renderer and an upload filter.

import (
	"slices"
	"testing"
)

func TestMediaTypesReserved(t *testing.T) {
	t.Run("admitted on a blobref, parsed and stored", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    file: {type: blobref, mediaTypes: [application/vnd.inkidian+json]}
    scan: {type: blobref, mediaTypes: [application/pdf, image/*]}
`)
		for name, want := range map[string][]string{
			"file": {"application/vnd.inkidian+json"},
			"scan": {"application/pdf", "image/*"},
		} {
			if got := ty.Props[name].MediaTypes; !slices.Equal(got, want) {
				t.Fatalf("%s: MediaTypes = %v, want %v", name, got, want)
			}
		}
		// Stored: the definition map is what the projection persists and what
		// the console reads, so a key that did not ride into it would be lost
		// on restart and invisible to the one reader it has.
		props, _ := ty.Definition["properties"].(map[string]any)
		scan, _ := props["scan"].(map[string]any)
		got, _ := scan["mediaTypes"].([]any)
		if len(got) != 2 || got[0] != "application/pdf" || got[1] != "image/*" {
			t.Fatalf("definition mediaTypes = %#v", scan["mediaTypes"])
		}
	})

	t.Run("absent is no list", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    file: {type: blobref}
`)
		if got := ty.Props["file"].MediaTypes; got != nil {
			t.Fatalf("MediaTypes = %v, want none", got)
		}
	})

	t.Run("a datatype that names no bytes is an error", func(t *testing.T) {
		for name, body := range map[string]string{
			"json": `  properties:
    ink: {type: json, mediaTypes: [application/vnd.inkidian+json]}
`,
			"string": `  properties:
    name: {type: string, mediaTypes: [text/plain]}
`,
			"markdown": `  properties:
    body: {type: markdown, mediaTypes: [text/markdown]}
`,
			"object": `  properties:
    spec: {type: object, mediaTypes: [application/json], fields: {a: {type: string}}}
`,
			"reference": `  properties:
    owner: {type: reference, kind: any, mediaTypes: [application/pdf]}
`,
			"an object field": `  properties:
    spec: {type: object, fields: {name: {type: string, mediaTypes: [text/plain]}}}
`,
		} {
			t.Run(name, func(t *testing.T) {
				loadThingErr(t, body, "only a blobref property declares mediaTypes")
			})
		}
	})

	t.Run("a malformed list is an error", func(t *testing.T) {
		for name, tc := range map[string]struct{ value, want string }{
			"not a media type":  {`[pdf]`, `"pdf" is not a media type`},
			"a wildcard type":   {`["*/pdf"]`, `"*/pdf" is not a media type`},
			"everything":        {`["*/*"]`, "*/* admits every format"},
			"a parameter":       {`["text/plain; charset=utf-8"]`, `name the format alone, such as "text/plain"`},
			"uppercase":         {`[Application/PDF]`, `is written in lowercase, "application/pdf"`},
			"listed twice":      {`[application/pdf, image/png, application/pdf]`, `"application/pdf" is listed twice`},
			"empty":             {`[]`, "an empty list names no format"},
			"a string, no list": {`application/pdf`, "a list of media types"},
			"not a string":      {`[3]`, "a list of media types"},
		} {
			t.Run(name, func(t *testing.T) {
				loadThingErr(t, `  properties:
    file: {type: blobref, mediaTypes: `+tc.value+`}
`, tc.want)
			})
		}
	})
}
