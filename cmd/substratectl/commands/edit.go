package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func (a *app) editCommand() *cobra.Command {
	var pkg string
	cmd := &cobra.Command{
		Use:   "edit <plural> <id>",
		Short: "Edit a record in $EDITOR and apply the result",
		Long: `Open the record's manifest (kind/metadata/data/status) in
$EDITOR, then apply what comes back. The ` + "`status`" + ` block is server-set and
ignored on the way in; emptying the file aborts.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := a.resolveCollection(ctx, args[0], pkg)
			if err != nil {
				return err
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			e, meta, err := cl.get(ctx, col.pkg(), col.Name, args[1])
			if err != nil {
				return err
			}
			// The buffer is the same document `get -o yaml` shows, which for a
			// declaration is its authored shape and not the record projection;
			// status.properties included, status ignored on the way back in.
			before, err := marshalDocument(documentOf(e, meta))
			if err != nil {
				return err
			}
			after, err := a.editBuffer(before, e.ID)
			if err != nil {
				return err
			}
			if bytes.Equal(bytes.TrimSpace(before), bytes.TrimSpace(after)) {
				fmt.Fprintf(a.out, "%s unchanged (edit canceled)\n", col.ref(e.ID))
				return nil
			}
			var node yaml.Node
			if err := yaml.Unmarshal(after, &node); err != nil {
				return fmt.Errorf("parse the edited document: %w", err)
			}
			body := unwrapNode(&node)
			fmt.Fprint(a.out, diffLines(string(before), string(after)))
			// The buffer splits the same two planes `apply -f` does, and on the
			// same test: rendering a declaration in its authored shape and then
			// putting it back through the RECORD verb would write the authored
			// keys on as properties, which is a worse answer than the refusal
			// the nested shape used to earn.
			if isSchemaDocument(body) {
				var raw map[string]any
				if err := body.Decode(&raw); err != nil {
					return fmt.Errorf("parse the edited document: %w", err)
				}
				return a.applySchemaDocuments(ctx, cl, []map[string]any{raw})
			}
			d, err := nodeDocument(body, "the edited document")
			if err != nil {
				return err
			}
			if d.Metadata.ID == "" {
				d.Metadata.ID = e.ID
			}
			return a.applyDocument(ctx, cl, d)
		},
	}
	cmd.Flags().StringVar(&pkg, "package", "", "the package (<authority>/<package>) a bare kind name resolves in")
	return cmd
}

// editBuffer writes the document to a temp file, opens the editor, and reads
// it back.
func (a *app) editBuffer(content []byte, id string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "substrate-edit-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "substrate-"+id+".yaml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	editor := firstNonEmpty(os.Getenv("SUBSTRATE_EDITOR"), os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	cmd := exec.Command("sh", "-c", editor+" "+shellQuote(path)) //nolint:gosec // the editor is the user's own
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.in, a.out, a.errOut
	if f, ok := a.in.(*os.File); ok {
		cmd.Stdin = f
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor %q failed: %w", editor, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read edited file: %w", err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, errors.New("edit aborted: the file was emptied")
	}
	return b, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// diffLines renders a minimal line diff (LCS based) of two documents.
func diffLines(before, after string) string {
	x := strings.Split(strings.TrimRight(before, "\n"), "\n")
	y := strings.Split(strings.TrimRight(after, "\n"), "\n")
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
		}
	}
	var b strings.Builder
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			fmt.Fprintf(&b, "- %s\n", x[i])
			i++
		default:
			fmt.Fprintf(&b, "+ %s\n", y[j])
			j++
		}
	}
	for ; i < len(x); i++ {
		fmt.Fprintf(&b, "- %s\n", x[i])
	}
	for ; j < len(y); j++ {
		fmt.Fprintf(&b, "+ %s\n", y[j])
	}
	return b.String()
}
