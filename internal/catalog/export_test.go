package catalog

// ShippedDocs are a bundle's documents as the catalog holds them: its
// declarations and the records it carries beside them.
func (b *Bundle) ShippedDocs() (vocabularyDocs, dataDocs []map[string]any) {
	return b.vocabularyDocs, b.dataDocs
}
