package app

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/networks"
	"github.com/geoah/substrate/cmd/chatbridge/internal/signin"
)

// Login links one account of a network from the terminal. It runs that
// network's bridge without waking its other logins, and stops once the new
// login is connected: what it received waits in the outbox for `run`.
func (a *App) Login(ctx context.Context, network, flow string, history networks.History, term signin.Terminal) error {
	spec, ok := networks.Find(network)
	if !ok {
		return fmt.Errorf("this build carries no network %q", network)
	}
	if flow == "" {
		flow = spec.Flows[0]
	}
	if !slices.Contains(spec.Flows, flow) {
		return fmt.Errorf("%s has no login flow %q (it has %v)", spec.DisplayName, flow, spec.Flows)
	}
	if err := a.OpenNetworks(networks.Options{DeviceName: a.Cfg.DeviceName, History: history}, network); err != nil {
		return err
	}
	run := a.Nets[network]
	if err := run.StartConnectorsOnly(ctx); err != nil {
		return err
	}
	defer run.Stop(10 * time.Second)
	owner, err := run.Owner(ctx)
	if err != nil {
		return err
	}
	login, err := signin.Run(ctx, run.Bridge, owner, flow, term)
	if err != nil {
		return err
	}
	fmt.Fprintf(term.Out, "\nLinked %s account %s as %q.\n", spec.DisplayName, login.RemoteName, a.Cfg.DeviceName)
	// Let the first history notifications land in the outbox before the
	// connection closes; anything unacknowledged arrives again under run.
	select {
	case <-ctx.Done():
	case <-time.After(15 * time.Second):
	}
	fmt.Fprintln(term.Out, "Start the bridge (chatbridge run) to deliver it to the server.")
	return nil
}
