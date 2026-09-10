# The Firecrawl sample

Package `samples.substrate.reamde.dev/firecrawl`: web search and page scraping
over the Firecrawl API, as two callables an agent binds as tools. Not a
provider, so no account is connected and nothing syncs: one API key on the
bundle's config record is the whole of its auth.

`bundle.yaml` is the closure, and the whole bundle: the config kind, the
`webdocument` kind and the two functions. There is no `triggers.yaml`, because
both functions are called rather than delivered. It is the contract; this file
is not.

What each function does, where keys come from, the scrape cap and the emit
ceiling an agent binding them needs:
[docs/bundles-catalog.md#firecrawl-sample](../../docs/bundles-catalog.md#firecrawl-sample).
