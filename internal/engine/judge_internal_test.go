package engine

// The judge's reply contract, decoded without a database: the engine states
// the shape (judgeReplyContract) and this is the other half of that promise —
// what it will and will not accept back.

import (
	"strings"
	"testing"
)

// haikuFencedVerdict is a REAL reply, from issue #555: a judge told in prose
// to answer with only the JSON object and no fences answered with the object
// inside a ```json fence anyway, and the engine recorded a correct 0.92
// accept as `outcome: error`. Verbatim, because the fixture's whole value is
// that nobody wrote it.
const haikuFencedVerdict = "```json\n" +
	`{"verdict": "accept", "confidence": 0.92, "rationale": "The summary is a concrete, plain-English sentence describing a specific request (review comments on PR #60287) directed at a named person (Hao Chen), supported by the cited source message, with a reasonable salience value of 62."}` +
	"\n```"

// TestDecodeJudgeVerdictReadsAFenceAndNothingLooser: the fence is
// PRESENTATION and comes off; everything else a model might pad its answer
// with still fails closed, because the verdict feeds an authorization
// decision and a guess is not an answer.
func TestDecodeJudgeVerdictReadsAFenceAndNothingLooser(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		reply      string
		wantErr    bool
		verdict    string
		confidence float64
	}{
		{
			name:       "a bare object",
			reply:      `{"verdict":"accept","confidence":0.8,"rationale":"small and honest"}`,
			verdict:    judgeVerdictAccept,
			confidence: 0.8,
		},
		{
			name:       "whitespace around the object",
			reply:      "\n  {\"verdict\":\"reject\",\"confidence\":1,\"rationale\":\"a deletion\"}  \n",
			verdict:    judgeVerdictReject,
			confidence: 1,
		},
		{
			name:       "the fenced reply from #555",
			reply:      haikuFencedVerdict,
			verdict:    judgeVerdictAccept,
			confidence: 0.92,
		},
		{
			name:       "a fence with no language tag",
			reply:      "```\n" + `{"verdict":"escalate","confidence":0.5,"rationale":"unsure"}` + "\n```",
			verdict:    judgeVerdictEscalate,
			confidence: 0.5,
		},
		{
			name:       "a fence the model closed on the object's own line",
			reply:      "```json\n" + `{"verdict":"accept","confidence":0.3,"rationale":"ok"}` + "```",
			verdict:    judgeVerdictAccept,
			confidence: 0.3,
		},
		{
			name:    "prose before the object",
			reply:   `Sure! Here is my verdict: {"verdict":"accept","confidence":0.99,"rationale":"fine"}`,
			wantErr: true,
		},
		{
			name:    "prose after the fence",
			reply:   "```json\n" + `{"verdict":"accept","confidence":0.9,"rationale":"fine"}` + "\n```\nHope that helps!",
			wantErr: true,
		},
		{
			name:    "a fence inside a fence",
			reply:   "```\n```json\n" + `{"verdict":"accept","confidence":0.9,"rationale":"fine"}` + "\n```\n```",
			wantErr: true,
		},
		{
			name:    "an unclosed fence",
			reply:   "```json\n" + `{"verdict":"accept","confidence":0.9,"rationale":"fine"}`,
			wantErr: true,
		},
		{
			name:    "trailing garbage",
			reply:   `{"verdict":"accept","confidence":0.9,"rationale":"fine"}}`,
			wantErr: true,
		},
		{
			name:    "a second object after the first",
			reply:   `{"verdict":"accept","confidence":0.9,"rationale":"fine"}{"verdict":"reject"}`,
			wantErr: true,
		},
		{
			name:    "an unknown key",
			reply:   `{"verdict":"accept","confidence":0.9,"rationale":"fine","notes":"extra"}`,
			wantErr: true,
		},
		{
			name:    "a miscased key",
			reply:   `{"Verdict":"accept","confidence":0.9,"rationale":"fine"}`,
			wantErr: true,
		},
		{
			name:    "no rationale",
			reply:   `{"verdict":"accept","confidence":0.9}`,
			wantErr: true,
		},
		{
			name:    "no confidence",
			reply:   `{"verdict":"accept","rationale":"fine"}`,
			wantErr: true,
		},
		{
			name:    "confidence past one",
			reply:   `{"verdict":"accept","confidence":1.2,"rationale":"very fine"}`,
			wantErr: true,
		},
		{
			name:    "a verdict outside the vocabulary",
			reply:   `{"verdict":"approve","confidence":0.9,"rationale":"fine"}`,
			wantErr: true,
		},
		{
			name:    "an empty reply",
			reply:   "   ",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var v judgeVerdict
			err := decodeJudgeVerdict(c.reply, &v)
			if c.wantErr {
				if err == nil {
					t.Fatalf("admitted %q as %+v", c.reply, v)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused %q: %v", c.reply, err)
			}
			if v.Verdict != c.verdict || v.Confidence != c.confidence || v.Rationale == "" {
				t.Fatalf("decoded %+v, want %s at %v with a rationale", v, c.verdict, c.confidence)
			}
		})
	}
}

// TestJudgeReplyContractStatesEveryFieldTheDecoderRequires: the prompt suffix
// and the decoder are one contract, so the suffix names all three fields and
// all three verdicts. A field the decoder requires and the prompt never
// mentions is exactly the bug #555 reported, one layer up.
func TestJudgeReplyContractStatesEveryFieldTheDecoderRequires(t *testing.T) {
	t.Parallel()
	for _, word := range []string{
		"verdict", "confidence", "rationale",
		judgeVerdictAccept, judgeVerdictReject, judgeVerdictEscalate,
	} {
		if !strings.Contains(judgeReplyContract, word) {
			t.Fatalf("the reply contract never says %q: %s", word, judgeReplyContract)
		}
	}
}
