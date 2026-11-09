// Command chatbridge connects the owner's chat networks to a substrate as
// the owner, never as a bot. It runs mautrix bridgev2 with each network's
// connector against a local Matrix side, pushes what happens on the
// networks to the substrate's webhook door one ordered batch at a time, and
// carries out the chat actions the owner asks for through the change feed
// (docs/plans/chat-bridge.md). It talks to the substrate only over its
// public HTTP API.
//
// Copyright (C) 2026 the substrate authors. Licensed under the GNU Affero
// General Public License, version 3 (LICENSE), because the network
// connectors it links are.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/term"

	"github.com/geoah/substrate/cmd/chatbridge/internal/app"
	"github.com/geoah/substrate/cmd/chatbridge/internal/config"
	"github.com/geoah/substrate/cmd/chatbridge/internal/networks"
	"github.com/geoah/substrate/cmd/chatbridge/internal/server"
	"github.com/geoah/substrate/cmd/chatbridge/internal/signin"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// version is stamped at build time with -ldflags "-X main.version=…".
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

const usage = `chatbridge connects your chat networks to your substrate as you.

Usage:
  chatbridge run                          run the bridge
  chatbridge authorize --server URL       get the bridge's token through the approval page
  chatbridge login whatsapp [--phone N]   link a WhatsApp account (stop the bridge first)
  chatbridge login telegram [--qr]        link a Telegram account (needs CHATBRIDGE_TELEGRAM_API_ID and _HASH)
  chatbridge status                       show what the store holds
  chatbridge version                      print the version

The store is CHATBRIDGE_DATA (default ./data). See README.md for the rest.
`

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "run":
		err = runCmd(args)
	case "authorize":
		err = authorizeCmd(args)
	case "login":
		err = loginCmd(args)
	case "status":
		err = statusCmd(args)
	case "version", "--version":
		fmt.Println(buildVersion())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "chatbridge: no command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		slog.Error("chatbridge stopped", "error", err)
		os.Exit(1)
	}
}

// loggers builds the bridge's slog logger and the zerolog logger bridgev2
// and the connectors take. Debug includes message text.
func loggers(cfg config.Config) (*slog.Logger, zerolog.Logger) {
	level := slog.LevelInfo
	zlevel := zerolog.InfoLevel
	if cfg.Debug {
		level, zlevel = slog.LevelDebug, zerolog.DebugLevel
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	zlog := zerolog.New(os.Stdout).Level(zlevel).With().Timestamp().Logger()
	return log, zlog
}

func dataFlag(fs *flag.FlagSet, cfg *config.Config) {
	fs.StringVar(&cfg.DataDir, "data", cfg.DataDir, "the bridge store (CHATBRIDGE_DATA)")
}

func runCmd(args []string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dataFlag(fs, &cfg)
	_ = fs.Parse(args)
	log, zlog := loggers(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, err := app.Open(ctx, cfg, buildVersion(), log, zlog)
	if err != nil {
		return err
	}
	defer a.Close()
	err = a.Run(ctx)
	if ctx.Err() != nil {
		log.Info("stopped")
		return nil
	}
	return err
}

func authorizeCmd(args []string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("authorize", flag.ExitOnError)
	dataFlag(fs, &cfg)
	serverURL := fs.String("server", "", "the substrate server, https://…")
	listen := fs.String("listen", "127.0.0.1:0", "the loopback address the approval comes back to; fix the port to forward it")
	printURL := fs.Bool("print-url", false, "print the approval URL and do not open a browser, for a headless host")
	_ = fs.Parse(args)
	if *serverURL == "" {
		return errors.New("authorize needs --server")
	}
	loggers(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// No lock: authorize writes one key, which a running bridge reads
	// again within half a minute.
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	auth, err := server.Authorize(ctx, server.AuthorizeOptions{
		Server:          *serverURL,
		Listen:          *listen,
		SoftwareVersion: buildVersion(),
		Show: func(u string) {
			fmt.Fprintf(os.Stderr, "Open this page to approve the bridge, signed in as the owner:\n\n  %s\n\n", u)
			if !*printURL {
				openBrowser(u)
			}
			fmt.Fprintln(os.Stderr, "Waiting for the approval…")
		},
	})
	if err != nil {
		return err
	}
	if err := app.SaveAuth(ctx, st, auth); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Authorized for %s.", auth.Repository)
	if auth.ExpiresAt != nil {
		fmt.Fprintf(os.Stderr, " The token ends %s; authorize again before then.", auth.ExpiresAt.Format(time.RFC1123))
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func loginCmd(args []string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("login needs a network: chatbridge login whatsapp, or telegram")
	}
	network := args[0]
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	dataFlag(fs, &cfg)
	phone := fs.String("phone", "", "the phone number, in international format: WhatsApp links with a pairing code typed on the phone instead of a QR; Telegram skips asking for it")
	qr := fs.Bool("qr", false, "Telegram: link by scanning a QR in the app instead of a code sent to the phone")
	history := fs.String("history", "", "how much history to ask for: recent, year or all; asked when absent")
	invert := fs.Bool("invert-qr", false, "draw the QR for a terminal with a light background")
	_ = fs.Parse(args[1:])
	log, zlog := loggers(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	in := bufio.NewReader(os.Stdin)
	depth := *history
	if depth == "" {
		depth = askHistory(network, in)
	}
	h, err := networks.ParseHistory(depth)
	if err != nil {
		return err
	}
	a, err := app.Open(ctx, cfg, buildVersion(), log, zlog)
	if err != nil {
		return err
	}
	defer a.Close()
	flow := ""
	answers := map[string]string{}
	switch {
	case *qr:
		flow = "qr"
	case *phone != "" && network == "whatsapp":
		flow = "phone"
	}
	if *phone != "" {
		answers["phone_number"] = *phone
	}
	return a.Login(ctx, network, flow, h, signin.Terminal{
		In: in, Out: os.Stderr, Answers: answers, InvertQR: *invert,
		Secret: func(prompt string) (string, error) { return readSecret(prompt, in) },
	})
}

// askHistory asks how far back a new link's history goes. WhatsApp fixes it
// when the device links; Telegram serves everything, and the answer bounds
// how much of it the first backfill fetches.
func askHistory(network string, in *bufio.Reader) string {
	switch network {
	case "telegram":
		fmt.Fprintln(os.Stderr, "How much history should the first backfill fetch? Telegram keeps everything; this bounds each chat.")
		fmt.Fprintln(os.Stderr, "  recent  the latest 1,000 messages of each chat, from the 50 most recent chats")
		fmt.Fprintln(os.Stderr, "  year    the latest 5,000 messages of each chat, from the 200 most recent chats")
		fmt.Fprintln(os.Stderr, "  all     every message of every chat")
	default:
		fmt.Fprintln(os.Stderr, "How much history should the phone send? The network fixes this when the device links; changing it later means linking again.")
		fmt.Fprintln(os.Stderr, "  recent  about the last three months (the network's default)")
		fmt.Fprintln(os.Stderr, "  year    about a year")
		fmt.Fprintln(os.Stderr, "  all     everything the phone holds, up to about three years")
	}
	fmt.Fprint(os.Stderr, "History [recent]: ")
	line, _ := in.ReadString('\n')
	if depth := strings.TrimSpace(line); depth != "" {
		return depth
	}
	return string(networks.HistoryRecent)
}

// readSecret reads a password or a token without echoing it on a terminal,
// and as a plain line from a pipe.
func readSecret(prompt string, in *bufio.Reader) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func statusCmd(args []string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dataFlag(fs, &cfg)
	_ = fs.Parse(args)
	loggers(cfg)
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	return app.PrintStatus(ctx, st, cfg.DataDir, os.Stdout)
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	default:
		return
	}
	_ = cmd.Start()
}
