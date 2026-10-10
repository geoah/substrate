package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/config"
	"github.com/geoah/substrate/internal/engine"
)

// Every delivery of every dispatcher pass holds one of the process's
// TriggerDeliverySlots, so a schedule lane wider than the process could never
// fill, and a negative one is a mistake the server's configuration refuses
// too. Open refuses both before it dials the database; zero is the default
// and passes, as does every width from 1 to the ceiling, and those Opens
// fail only at the unreachable database. The configuration holds the same
// ceiling.
func TestALaneWiderThanTheProcessDeliverySlotsIsRefused(t *testing.T) {
	t.Parallel()
	if config.MaxTriggerLaneWorkers != engine.TriggerDeliverySlots {
		t.Fatalf("config ceiling %d, engine ceiling %d", config.MaxTriggerLaneWorkers, engine.TriggerDeliverySlots)
	}
	open := func(n int) error {
		_, err := engine.OpenForTest(t, context.Background(), "postgres://127.0.0.1:1/unreachable",
			engine.WithDataRoot(t.TempDir()), engine.WithTriggerLaneWorkers(n))
		return err
	}
	for _, n := range []int{-1, engine.TriggerDeliverySlots + 1} {
		if err := open(n); err == nil || !strings.Contains(err.Error(), "WithTriggerLaneWorkers") {
			t.Fatalf("a lane of %d workers: err = %v, want a refusal naming WithTriggerLaneWorkers", n, err)
		}
	}
	for _, n := range []int{0, 1, engine.TriggerDeliverySlots} {
		if err := open(n); err == nil || strings.Contains(err.Error(), "WithTriggerLaneWorkers") {
			t.Fatalf("a lane of %d workers: err = %v, want it past the lane check and refused at the dial", n, err)
		}
	}
}
