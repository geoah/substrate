package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

const alertKindName = "alert"

// alertsPageSize and alertsPageLimit bound the walk: alerts are one per
// problem, so a repository holds few, and the cap keeps a runaway read from
// paging forever.
const (
	alertsPageSize  = 100
	alertsPageLimit = 50
)

func (a *app) alertsCommand() *cobra.Command {
	var (
		output string
		all    bool
	)
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "List the open alerts: one per ongoing problem the substrate found",
		Long: `The substrate raises one substrate.reamde.dev/core/alert record per
ongoing problem, such as a trigger whose deliveries keep parking
(trigger.parked/<trigger id>), and resolves it when the problem clears.
This lists the open ones, newest first; --all adds the resolved ones.
An alert is an ordinary record: resolve one by hand by putting its state
to resolved (get -o yaml, edit, apply -f).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			alerts, err := cl.alerts(cmd.Context(), all)
			if err != nil {
				return err
			}
			// The rows as apply-able documents, the ones `get -o json` prints,
			// so one resolved by hand goes back through `apply`.
			docs := make([]any, 0, len(alerts))
			for _, e := range alerts {
				docs = append(docs, documentOf(e, nil))
			}
			return printList(a, output, docs, func() error { return a.printAlertsTable(alerts) })
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json|yaml")
	cmd.Flags().BoolVar(&all, "all", false, "include resolved alerts")
	return cmd
}

func (c *client) alerts(ctx context.Context, all bool) ([]*substrate.Record, error) {
	q := url.Values{
		"first":   []string{strconv.Itoa(alertsPageSize)},
		"orderBy": []string{"updatedAt:desc"},
	}
	if !all {
		if err := editFilter(q, func(f *substrate.Filter) {
			f.Properties = map[string]substrate.Cond{"state": {Eq: "open"}}
		}); err != nil {
			return nil, err
		}
	}
	var out []*substrate.Record
	for range alertsPageLimit {
		page, err := c.list(ctx, corePackage, alertKindName, q)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Records...)
		if page.Cursor == "" {
			return out, nil
		}
		q.Set("after", page.Cursor)
	}
	return nil, fmt.Errorf("more than %d alerts: read them with get %s/%s", alertsPageSize*alertsPageLimit, corePackage, alertKindName)
}

func (a *app) printAlertsTable(alerts []*substrate.Record) error {
	tw := newTable(a.out)
	fmt.Fprintln(tw, "KEY\tLEVEL\tSTATE\tCOUNT\tFIRST SEEN\tLAST SEEN\tSUMMARY")
	for _, e := range alerts {
		p := e.Properties
		key, _ := p["key"].(string)
		level, _ := p["level"].(string)
		state, _ := p["state"].(string)
		summary, _ := p["summary"].(string)
		summary, _, _ = strings.Cut(summary, "\n")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			dash(key), dash(level), dash(state), alertCountCell(p["count"]),
			a.alertAge(p["firstSeenAt"]), a.alertAge(p["lastSeenAt"]), dash(truncate(summary, 80)))
	}
	return tw.Flush()
}

func (a *app) alertAge(v any) string {
	s, _ := v.(string)
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return "-"
	}
	return humanAge(a.now(), at)
}

// alertCountCell renders the count property: a json.Number off the wire (the
// response decoder is UseNumber), a float from any other decode.
func alertCountCell(v any) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	case int:
		return strconv.Itoa(n)
	}
	return "-"
}
