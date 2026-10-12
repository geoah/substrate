package synth

import (
	"strings"
	"testing"
)

// The expected names were produced by the Python rule
// (internal/providere2e/runner/mockserver.py, recording_name) on 2026-10-12,
// so a drift on either side fails here.
func TestRecordingNameMatchesTheMockServer(t *testing.T) {
	cases := []struct {
		method, path string
		pairs        []Pair
		want         string
	}{
		{"GET", "/v2/cycle", nil, "GET_v2_cycle.json"},
		{
			"GET", "/v2/cycle",
			[]Pair{{"limit", "25"}, {"start", "2026-07-14T00:00:00Z"}, {"nextToken", "MjAyNjA5MTA2MjM"}},
			"GET_v2_cycle__limit-25_nextToken-MjAyNjA5MTA2MjM_start-2026-07-14T00_00_00Z.json",
		},
		{
			"GET", "/fitness/v1/users/me/dataSources/derived:com.google.heart_rate.bpm:com.google.android.gms:merge_heart_rate_bpm/datasets/1791344581754000000-1791430981754000000",
			[]Pair{{"limit", "1000"}},
			"GET_fitness_v1_users_me_dataSources_derived_com.google.heart_rate.bpm_com.google.android.gms_merge_heart_rate_bpm_datasets_1791344581754000000-1791430981754000000__limit-1000.json",
		},
		{
			"GET", "/fitness/v1/users/me/sessions",
			[]Pair{{"includeDeleted", "true"}, {"pageToken", "1790405742001"}},
			"GET_fitness_v1_users_me_sessions__includeDeleted-true_pageToken-1790405742001.json",
		},
		{
			"GET", "/wellness-api/rest/backfill/dailies",
			[]Pair{{"summaryStartTimeInSeconds", "1789074000"}, {"summaryEndTimeInSeconds", "1789160400"}},
			"GET_wellness-api_rest_backfill_dailies__summaryEndTimeInSeconds-1789160400_summaryStartTimeInSeconds-1789074000.json",
		},
		{"GET", "/", nil, "GET_root.json"},
		{"GET", "/oauth2/v3/userinfo", []Pair{{"access_token", "secret"}, {"alt", "json"}}, "GET_oauth2_v3_userinfo__alt-json.json"},
		{
			"GET", "/calendar/v3/calendars/primary/events",
			[]Pair{{"maxResults", "250"}, {"singleEvents", "true"}, {"q", strings.Repeat("x", 100)}},
			"GET_calendar_v3_calendars_primary_events__maxResults-250_q-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx-ec99ff1f.json",
		},
		{"post", "/conversations.history", nil, "POST_conversations.history.json"},
	}
	for _, c := range cases {
		if got := RecordingName(c.method, c.path, c.pairs...); got != c.want {
			t.Errorf("%s %s %v:\n got %s\nwant %s", c.method, c.path, c.pairs, got, c.want)
		}
	}
}

func TestPaginationParamsLeaveWhoopsNextTokenAlone(t *testing.T) {
	// The mock's second try drops every PaginationParams pair, so WHOOP's
	// page-two recording is found only because nextToken is not one.
	if PaginationParams["nextToken"] {
		t.Fatal("nextToken is a pagination parameter: the WHOOP pages would collapse onto page one")
	}
	if !PaginationParams["pageToken"] || !PaginationParams["limit"] {
		t.Fatal("pageToken and limit are pagination parameters: the Google Fit pages must be named exactly")
	}
}
