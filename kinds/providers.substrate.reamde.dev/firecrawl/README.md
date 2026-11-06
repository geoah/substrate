# The Firecrawl bundle

Package `providers.substrate.reamde.dev/firecrawl`: web search and page reading
over the Firecrawl API, as two functions an agent calls as tools. No account is
connected and nothing syncs; one API key is the whole of its auth.

`bundle.yaml` is the closure: the `webdocument` kind, the two functions
(`websearch` and `webscrape`, offered to agents as `web_search` and
`web_scrape`) and the `firecrawl_api` module both import. `settings.yaml` is
the bundle's configuration as ordinary records: a core `secret` at
`…/firecrawl/apiKey`, shipped empty and required, and a core `setting` at
`…/firecrawl/baseUrl`, shipped at the pinned Firecrawl origin. There is no
`triggers.yaml`, because both functions are called rather than delivered. The
two files are the contract; this one is not.

Where the key comes from (a person's own, or the operator's), where it may be
sent, the scrape cap and the writes an agent needs:
[docs/bundles-catalog.md#firecrawl](../../../docs/bundles-catalog.md#firecrawl).
