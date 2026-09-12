# The Firecrawl sample

Package `samples.substrate.reamde.dev/firecrawl`: web search and page scraping
over the Firecrawl API, as two callables an agent binds as tools. Not a
provider, so no account is connected and nothing syncs: one API key is the
whole of its auth.

`bundle.yaml` is the vocabulary closure: the `webdocument` kind and the two
functions. `settings.yaml` is the bundle's configuration as ordinary records —
a core `secret` at `…/firecrawl/apiKey`, shipped empty and required, and a core
`setting` at `…/firecrawl/baseUrl`, shipped pinned at the Firecrawl origin. The
bodies read them as `config.settings.apiKey` and `config.settings.baseUrl`.
There is no `triggers.yaml`, because both functions are called rather than
delivered. The two files are the contract; this one is not.

What each function does, where the key comes from, the scrape cap and the emit
ceiling an agent binding them needs:
[docs/bundles-catalog.md#firecrawl-sample](../../docs/bundles-catalog.md#firecrawl-sample).
