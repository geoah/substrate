package testdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

// The labels every test-owned container carries: which machine started it
// and which process. Ryuk removes a session's containers when the process
// that owns the session goes away, but it can miss some. A killed run's
// ryuk exits while container creates are still in flight in dockerd, and
// those land in `Created` with nothing left to reap them. Measured on
// 2026-09-26: ten pgvector containers created after their session's ryuk
// had gone. SweepOrphans removes a labeled container whose owner process
// is no longer alive on this host, so every run cleans up after the last.
const (
	labelOwned = "substrate.test"
	labelHost  = "substrate.test.host"
	labelPID   = "substrate.test.pid"
)

// sweepOnce runs SweepOrphans before the first container this process starts.
var sweepOnce sync.Once

func ownerLabels() map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		labelOwned: "1",
		labelHost:  host,
		labelPID:   strconv.Itoa(os.Getpid()),
	}
}

// OwnerLabels is the container option that labels a test-owned container
// with this machine and this process, for SweepOrphans and
// .mise/testclean.sh. Every container a test starts carries it.
func OwnerLabels() testcontainers.CustomizeRequestOption {
	return testcontainers.WithLabels(ownerLabels())
}

// SweepOrphans removes the test containers on this host whose owner process
// has exited: a run killed before ryuk could reap, or a create that
// finished after ryuk had gone. A container whose owner is alive, or whose
// owner cannot be read, is left alone, so a run never touches another
// run's containers. Best effort: an error is printed and the run goes on.
func SweepOrphans(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdb: sweep orphaned test containers: %v\n", err)
		return
	}
	defer func() { _ = cli.Close() }()
	host, _ := os.Hostname()
	list, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All: true,
		Filters: client.Filters{}.
			Add("label", labelOwned+"=1").
			Add("label", labelHost+"="+host),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdb: sweep orphaned test containers: %v\n", err)
		return
	}
	var removed []string
	for _, c := range list.Items {
		pid, err := strconv.Atoi(c.Labels[labelPID])
		if err != nil || pid <= 0 || pid == os.Getpid() || processAlive(pid) {
			continue
		}
		if _, err := cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			fmt.Fprintf(os.Stderr, "testdb: remove orphaned test container %.12s: %v\n", c.ID, err)
			continue
		}
		removed = append(removed, fmt.Sprintf("%.12s (pid %d, %s)", c.ID, pid, c.State))
	}
	if len(removed) > 0 {
		fmt.Fprintf(os.Stderr, "testdb: removed %d orphaned test container(s): %s\n", len(removed), strings.Join(removed, ", "))
	}
}

// processAlive reports whether pid names a live process. EPERM means it is
// alive and somebody else's.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
