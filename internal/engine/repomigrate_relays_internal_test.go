package engine

import "testing"

// The migration's copy of REPLY_RELAYS answers the cases the bundle's own
// test holds syncgmail's to (internal/providertest/py/test_google_gmail.py).
func TestIsReplyRelayMatchesTheGmailSyncsRule(t *testing.T) {
	t.Parallel()
	const token = "AAEXAMPLETOKEN0000000000000000000000000000000000"
	for _, addr := range []string{
		"reply+" + token + "@reply.github.com",
		"Reply+" + token + "@Reply.GitHub.com",
		"repo@noreply.github.com",
		"author@noreply.github.com",
		"mention@noreply.github.com",
		"subscribed@noreply.github.com",
		"comment@noreply.github.com",
		"state_change@noreply.github.com",
		"review_requested@noreply.github.com",
		"team_mention@noreply.github.com",
		"push@noreply.github.com",
		"ci_activity@noreply.github.com",
		"assign@noreply.github.com",
		"manual@noreply.github.com",
		"your_activity@noreply.github.com",
		"Mention@NoReply.GitHub.com",
	} {
		if !isReplyRelay(addr) {
			t.Errorf("%s is a relay, and the migration does not say so", addr)
		}
	}
	for _, addr := range []string{
		"notifications@github.com",
		"noreply@github.com",
		"12345+grace@users.noreply.github.com",
		"12345+Grace@Users.NoReply.GitHub.com",
		"repo@noreply.github.com.example.com",
		"repo@xnoreply.github.com",
		"@noreply.github.com",
		"reply+@reply.github.com",
		"reply+" + token + "@github.com",
		"reply+" + token + "@reply.github.com.example.com",
		"reply+" + token + "@example.com",
		"someone@reply.github.com",
		"ada+reply@example.com",
		"noreply.github.com",
	} {
		if isReplyRelay(addr) {
			t.Errorf("%s names a party, and the migration calls it a relay", addr)
		}
	}
}
