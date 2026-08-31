package commands

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func (a *app) watchCommand() *cobra.Command {
	var (
		from   int64
		kinds  []string
		ops    []string
		actors []string
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream the changelog",
		Long: `Stream the repository's changelog as one line per committed change.

The stream is resumable: --from takes the sequence number to resume after,
and every printed line starts with the sequence it can be resumed from.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			q := url.Values{}
			q.Set("watch", "1")
			if from > 0 {
				q.Set("from", strconv.FormatInt(from, 10))
			}
			if len(kinds) > 0 {
				q.Set("kinds", strings.Join(kinds, ","))
			}
			if len(ops) > 0 {
				q.Set("ops", strings.Join(ops, ","))
			}
			if len(actors) > 0 {
				q.Set("actors", strings.Join(actors, ","))
			}
			resp, err := cl.send(cmd.Context(), http.MethodGet,
				pathChanges, q, nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			return streamChanges(a.out, resp.Body)
		},
	}
	f := cmd.Flags()
	f.Int64Var(&from, "from", 0, "resume after this changelog sequence")
	f.StringSliceVar(&kinds, "kinds", nil, "only these kind identities")
	f.StringSliceVar(&ops, "ops", nil, "only these ops (put, patch, delete, merge, split, gc)")
	f.StringSliceVar(&actors, "actors", nil, "only these actors")
	return cmd
}
