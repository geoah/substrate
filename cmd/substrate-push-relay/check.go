package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/geoah/substrate/internal/apns"
	"github.com/geoah/substrate/internal/pushwire"
)

const checkUsage = `substrate-push-relay check: mint a provider token for each environment the
relay holds a key for and send Apple one alert, then print what Apple answered.

Without --device-token the alert goes to a device token no install holds,
on the first topic. Apple answers BadDeviceToken when it read the request and
accepted the key: that proves the key, its id and the team id, not that the
key may send to that topic in that environment. InvalidProviderToken is a
key Apple refuses, and fails the check.

With --device-token and --environment it sends one reply alert, the app's
own fallback words and an event id that resolves nowhere, to that install:
the acceptance, to a consenting test install. It passes on 200.

It reads the relay's own configuration from the environment.

Flags:
`

// check is the `check` command. It answers the exit status.
func check(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	deviceToken := fs.String("device-token", "", "Apple's token of a consenting test install, in hex")
	environment := fs.String("environment", "", "the environment that issued --device-token: production or development")
	topic := fs.String("topic", "", "the topic to send to (default: the first in SUBSTRATE_PUSH_RELAY_APNS_TOPICS)")
	fs.Usage = func() {
		_, _ = io.WriteString(fs.Output(), checkUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fs.Usage()
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "check takes no arguments, only flags: %v\n", fs.Args())
		return 2
	}
	if *deviceToken != "" && *environment == "" {
		_, _ = fmt.Fprintln(stderr, "check: --device-token needs --environment, the one that issued the token")
		return 2
	}
	if *deviceToken != "" && (len(*deviceToken) < pushwire.MinToken || len(*deviceToken) > pushwire.MaxToken || !isHex(*deviceToken)) {
		_, _ = fmt.Fprintf(stderr, "check: --device-token is not %d to %d hex characters\n", pushwire.MinToken, pushwire.MaxToken)
		return 2
	}

	c, err := loadConfig()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "check: %v\n", err)
		return 1
	}
	if *topic == "" {
		*topic = c.topics[0]
	} else if !slices.Contains(c.topics, *topic) {
		_, _ = fmt.Fprintf(stderr, "check: the relay does not push to %q; its topics are %v\n", *topic, c.topics)
		return 2
	}
	apple, err := clients(c, nil)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "check: %v\n", err)
		return 1
	}
	if *environment != "" && apple[*environment] == nil {
		_, _ = fmt.Fprintf(stderr, "check: the relay holds no key for %q\n", *environment)
		return 2
	}

	failed := false
	for _, k := range c.keys {
		env := k.environment
		if *environment != "" && env != *environment {
			continue
		}
		client := apple[env]
		if _, err := client.Token(); err != nil {
			_, _ = fmt.Fprintf(stdout, "%s: FAILED: could not mint a provider token: %v\n", env, err)
			failed = true
			continue
		}
		token := *deviceToken
		if token == "" {
			b := make([]byte, 32)
			_, _ = rand.Read(b)
			token = hex.EncodeToString(b)
		}
		n := alert()
		resp, err := client.Send(ctx, apns.Notification{DeviceToken: token, Headers: n.Headers(*topic), Payload: n.Payload})
		switch {
		case err != nil:
			_, _ = fmt.Fprintf(stdout, "%s: FAILED: Apple did not answer: %v\n", env, err)
			failed = true
		case *deviceToken != "" && resp.Status == http.StatusOK:
			_, _ = fmt.Fprintf(stdout, "%s: ok: Apple accepted the alert for the test install on %s (apns-id %s)\n", env, *topic, resp.ID)
		case *deviceToken == "" && resp.Status == http.StatusBadRequest && resp.Reason == "BadDeviceToken":
			_, _ = fmt.Fprintf(stdout, "%s: ok: Apple answered %s: the request reached Apple, which read it and accepted the key. "+
				"This does not prove the key may send to %s in %s: send one push to a test install with --device-token for that.\n", env, resp, *topic, env)
		case resp.Reason == apns.ReasonInvalidProviderToken:
			_, _ = fmt.Fprintf(stdout, "%s: FAILED: Apple answered %s: it refuses the key, its key id or the team id\n", env, resp)
			failed = true
		default:
			_, _ = fmt.Fprintf(stdout, "%s: FAILED: Apple answered %s\n", env, resp)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// alert is the one alert check sends: a reply with the app's own fallback
// words, a badge of 0 that leaves nothing to clear, and an event id and a
// generation no server or install holds.
func alert() pushwire.AppleNotification {
	id := make([]byte, 32)
	gen := make([]byte, pushwire.GenerationSize)
	_, _ = rand.Read(id)
	_, _ = rand.Read(gen)
	return pushwire.ApplePush(pushwire.PushRequest{
		Event: pushwire.EventReply, Thread: "check", Badge: 0, EventID: pushwire.Encode(id),
	}, pushwire.Encode(gen), time.Now())
}

func isHex(s string) bool {
	for i := range len(s) {
		if (s[i] < '0' || s[i] > '9') && (s[i] < 'a' || s[i] > 'f') && (s[i] < 'A' || s[i] > 'F') {
			return false
		}
	}
	return true
}
