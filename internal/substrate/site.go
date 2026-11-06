package substrate

// SiteInfo is the deployer's site as discovery announces it: the brand the
// doors and the landing show, and where the server serves its files. A
// deployment with no SUBSTRATE_SITE_DIR has none, and discovery omits the
// whole object, so a client that finds no `site` shows the substrate's own
// face.
type SiteInfo struct {
	// Name is the brand, always present.
	Name string `json:"name"`
	// Tagline is the line shown under the name, absent where site.yaml sets
	// none.
	Tagline string `json:"tagline,omitempty"`
	// Logo is the logo's server-relative URL under /site/, absent where
	// site.yaml names none.
	Logo string `json:"logo,omitempty"`
	// Icon is the server-relative URL under /site/ of the browser tab's icon,
	// absent where site.yaml names none; a client then uses the logo.
	Icon string `json:"icon,omitempty"`
	// Landing is the server-relative URL under /site/ of the HTML fragment a
	// client renders at / for a signed-out visitor, absent where site.yaml
	// names none.
	Landing string `json:"landing,omitempty"`
}
