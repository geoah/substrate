package contract

import (
	"encoding/json"
	"testing"
	"time"
)

// Absent leaves a property, null clears it, an empty list empties it.
func TestAbsentNullAndEmptyAreThreeThings(t *testing.T) {
	ev := ChatUpsert{Head: Head{
		Seq: 1, At: At(time.Date(2026, 11, 9, 10, 0, 1, 123456789, time.UTC)), Type: TypeChatUpsert,
		Network: "whatsapp", Account: "1",
	}, Chat: Chat{
		ID: "c", Topic: Clear[string](), ChatName: Set("Ada"),
		Members: &Members{Complete: true, Items: []Member{}},
	}}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"seq":1,"at":"2026-11-09T10:00:01.123Z","type":"chat.upsert","network":"whatsapp","account":"1",` +
		`"chat":{"id":"c","chatName":"Ada","topic":null,"members":{"complete":true,"items":[]}}}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
	u, _ := json.Marshal(User{ID: "u", PhoneNumbers: []string{}})
	if string(u) != `{"id":"u","phoneNumbers":[]}` {
		t.Fatalf("user %s", u)
	}
}
