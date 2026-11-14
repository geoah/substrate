package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ArchiveStats is how far one repository's archive is behind its changelog
// (decision 0262). Oldest is when the oldest entry not yet archived was
// written, zero when the archive holds every entry; the delay is its age.
type ArchiveStats struct {
	Pending  int64
	Oldest   time.Time
	LastPass time.Time
	// Failures is the passes in a row that did not finish, 0 after one that
	// did.
	Failures int
	// Missing is how many blobs the archive's manifest names as missing:
	// held by the records, their bytes lost on this server. PendingBlobs is
	// how many it names as not copied yet, past a pass's step.
	Missing, PendingBlobs int
	// Stopped is set while the archive is not as the database left it and
	// the passes write nothing until an operator resolves it.
	Stopped bool
}

// archiveCollector publishes one repository's archive, the repository a
// constant label for the reason spoolCollector's is.
type archiveCollector struct {
	stat                                                           func() ArchiveStats
	pending, delay, last, failures, missing, pendingBlobs, stopped *prometheus.Desc
}

func newArchiveCollector(repository string, stat func() ArchiveStats) *archiveCollector {
	repo := prometheus.Labels{"repository": repository}
	return &archiveCollector{
		stat:         stat,
		pending:      prometheus.NewDesc("substrate_archive_pending_entries", "Changelog entries committed and not yet in the archive.", nil, repo),
		delay:        prometheus.NewDesc("substrate_archive_delay_seconds", "Age of the oldest changelog entry not yet in the archive, 0 when the archive holds every entry.", nil, repo),
		last:         prometheus.NewDesc("substrate_archive_last_pass_timestamp_seconds", "When the last archive pass that finished ended, 0 before the first.", nil, repo),
		failures:     prometheus.NewDesc("substrate_archive_failed_passes", "Archive passes in a row that did not finish, 0 after one that did.", nil, repo),
		missing:      prometheus.NewDesc("substrate_archive_missing_blobs", "Blobs the archived records hold whose bytes the archive lacks, because this server had lost them.", nil, repo),
		pendingBlobs: prometheus.NewDesc("substrate_archive_pending_blobs", "Blobs the archived records hold that the archive has not copied yet: a pass copies a bounded amount and publishes.", nil, repo),
		stopped:      prometheus.NewDesc("substrate_archive_stopped", "1 while the archive is not as the database left it and the passes write nothing until an operator resolves it.", nil, repo),
	}
}

func (c *archiveCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pending
	ch <- c.delay
	ch <- c.last
	ch <- c.failures
	ch <- c.missing
	ch <- c.pendingBlobs
	ch <- c.stopped
}

func (c *archiveCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stat()
	delay, last := 0.0, 0.0
	if !s.Oldest.IsZero() {
		delay = max(time.Since(s.Oldest).Seconds(), 0)
	}
	if !s.LastPass.IsZero() {
		last = float64(s.LastPass.UnixNano()) / 1e9
	}
	ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, float64(s.Pending))
	ch <- prometheus.MustNewConstMetric(c.delay, prometheus.GaugeValue, delay)
	ch <- prometheus.MustNewConstMetric(c.last, prometheus.GaugeValue, last)
	ch <- prometheus.MustNewConstMetric(c.failures, prometheus.GaugeValue, float64(s.Failures))
	ch <- prometheus.MustNewConstMetric(c.missing, prometheus.GaugeValue, float64(s.Missing))
	ch <- prometheus.MustNewConstMetric(c.pendingBlobs, prometheus.GaugeValue, float64(s.PendingBlobs))
	stopped := 0.0
	if s.Stopped {
		stopped = 1
	}
	ch <- prometheus.MustNewConstMetric(c.stopped, prometheus.GaugeValue, stopped)
}

// RegisterArchive publishes stat as the substrate_archive_* gauges under
// repository=name, with RegisterDBStats's take-over rule, and returns the
// matching unregister.
func RegisterArchive(name string, stat func() ArchiveStats) (unregister func()) {
	return register("archive:"+name, newArchiveCollector(name, stat))
}
