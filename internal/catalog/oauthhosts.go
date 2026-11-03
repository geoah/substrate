package catalog

import (
	"net/url"
	"sort"
)

func oauthHosts(data map[string]any) []string {
	seen := map[string]bool{}
	for _, key := range []string{"authorizationEndpoint", "tokenEndpoint", "revocationEndpoint", "emailEndpoint"} {
		if u, err := url.Parse(mstr(data, key)); err == nil && u.Hostname() != "" {
			seen[u.Hostname()] = true
		}
	}
	out := make([]string, 0, len(seen))
	for host := range seen {
		out = append(out, host)
	}
	sort.Strings(out)
	return out
}
