package config

import (
	"fmt"
	"time"
)

// Compaction is the compaction pass's settings (decision 0300): how often
// each repository's pass runs, and how long a deletion is remembered before
// the pass forgets the record.
type Compaction struct {
	// Interval is how often each repository's pass runs; `off` (or 0) runs
	// none, and nothing compacts by itself.
	Interval string `envconfig:"SUBSTRATE_COMPACTION_INTERVAL" default:"1h"`
	// DeletionsKept is how long a deletion is remembered: a client that
	// resumes from before a forgotten one is told `compacted`. 0 keeps every
	// deletion.
	DeletionsKept time.Duration `envconfig:"SUBSTRATE_DELETIONS_KEPT" default:"720h"`
}

// Every is the pass's interval, 0 when it is off.
func (c Compaction) Every() (time.Duration, error) {
	switch c.Interval {
	case "off", "0":
		return 0, nil
	case "":
		return time.Hour, nil
	}
	d, err := time.ParseDuration(c.Interval)
	if err != nil || d < time.Minute {
		return 0, fmt.Errorf("SUBSTRATE_COMPACTION_INTERVAL is %q: it is how often each repository's compaction pass runs, a duration of a minute or more, or off (1h is the default)", c.Interval)
	}
	return d, nil
}

// Validate refuses a setting the pass could not run with, naming it.
func (c Compaction) Validate() error {
	if _, err := c.Every(); err != nil {
		return err
	}
	if c.DeletionsKept < 0 {
		return fmt.Errorf("SUBSTRATE_DELETIONS_KEPT is %s: it is how long a deletion is remembered before the record is forgotten, 0 (forever) or a positive duration (720h is the default)", c.DeletionsKept)
	}
	return nil
}
