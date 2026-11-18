package authority

import (
	"strings"
	"testing"
)

const pushClientID = "https://apps.example.com/apps/chat/client.json"

func pushDocument(push string) []byte {
	entry := `{"client_id":"` + pushClientID + `","name":"Chat","apple":{"appIDs":["TEAMID.dev.example.chat"]}`
	if push != "" {
		entry += `,"push":` + push
	}
	return []byte(`{"authority":"example.com","name":"Example","apps":[` + entry + `}]}`)
}

// The relay an app's entry names is read, with or without a path.
func TestAppPushRelayRead(t *testing.T) {
	for _, relay := range []string{
		"https://push.example.com",
		"https://push.example.com/",
		"https://example.com/push",
		"https://example.com/push/",
	} {
		doc, err := ParseAuthorityDocument(pushDocument(`{"relay":"`+relay+`"}`), "example.com")
		if err != nil {
			t.Fatalf("%s: %v", relay, err)
		}
		app, ok := doc.App(pushClientID)
		if !ok || app.PushRelay() != relay {
			t.Fatalf("%s: read %q", relay, app.PushRelay())
		}
	}
}

// `push` is the one key read leniently: a value of the wrong shape, or a
// relay that is not one, is dropped, the app still verifies and names no
// relay, and every other key stays strict.
func TestAppPushReadLeniently(t *testing.T) {
	for _, push := range []string{
		`"x"`,
		`null`,
		`1`,
		`[]`,
		`{}`,
		`{"relay":1}`,
		`{"relay":null}`,
		`{"relay":"http://push.example.com"}`,
		`{"relay":"https://push.example.com?x=1"}`,
		`{"relay":"https://push.example.com/#frag"}`,
		`{"relay":"https://user@push.example.com"}`,
		`{"relay":"https://"}`,
		`{"relay":"push.example.com"}`,
		`{"relay":"https://push.example.com/` + strings.Repeat("a", 2048) + `"}`,
	} {
		doc, err := ParseAuthorityDocument(pushDocument(push), "example.com")
		if err != nil {
			t.Fatalf("push %s refused the document: %v", push, err)
		}
		app, ok := doc.App(pushClientID)
		if !ok {
			t.Fatalf("push %s dropped the app", push)
		}
		if app.Push != nil || app.PushRelay() != "" {
			t.Fatalf("push %s read as %+v", push, app.Push)
		}
		if app.Apple == nil || len(app.Apple.AppIDs) != 1 || app.Name != "Chat" {
			t.Fatalf("push %s lost the rest of the entry: %+v", push, app)
		}
	}
	// A key the relay object does not know is ignored, as everywhere.
	doc, err := ParseAuthorityDocument(pushDocument(`{"relay":"https://push.example.com","future":true}`), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if app, _ := doc.App(pushClientID); app.PushRelay() != "https://push.example.com" {
		t.Fatalf("an unknown key beside the relay dropped it: %+v", app.Push)
	}
	// Two copies of the key are still a malformed document.
	if _, err := ParseAuthorityDocument([]byte(`{"authority":"example.com","apps":[{"client_id":"`+pushClientID+`","push":"x","push":{"relay":"https://push.example.com"}}]}`), "example.com"); err == nil {
		t.Fatal("a duplicated push key was read")
	}
	// Every other key is held as strictly as before.
	for _, entry := range []string{
		`{"client_id":"` + pushClientID + `","name":null,"push":{"relay":"https://push.example.com"}}`,
		`{"client_id":"` + pushClientID + `","apple":"x","push":{"relay":"https://push.example.com"}}`,
		`{"client_id":"` + pushClientID + `","packages":[1],"push":{"relay":"https://push.example.com"}}`,
	} {
		if _, err := ParseAuthorityDocument([]byte(`{"authority":"example.com","apps":[`+entry+`]}`), "example.com"); err == nil {
			t.Fatalf("a strict key was read leniently: %s", entry)
		}
	}
}
