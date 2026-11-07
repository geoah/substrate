# The assistant sample

Package `samples.substrate.reamde.dev/assistant`: Moss, the owner's
assistant, seeded into every new repository beside the LLM sample and
imported onto the repository's own authority, so its prompts are the
owner's to read and edit. [The plan](../../docs/plans/moss.md) is the
design, and [the recorded sessions](../../docs/plans/moss-sessions.md) show
it at work.

`bundle.yaml` is the closure: the package and three agents.

- `moss`, the one to chat with: a general assistant that answers a question
  about the world on its own and a question about the owner from their
  records, keeps those records, and changes long text with `edit`.
- `architect`, the one agent holding `vocabulary`: it plans every change to
  the owner's kinds and applies only what the owner asked for. Its writes
  name `core/package` and `core/kind` alone, and it never converts records.
- `librarian`, which Moss calls as a background job for bulk record work.

All three name the provider row `anthropic` (Claude Opus 5.5, and Sonnet 5.5
for the librarian), keyless until you write an `apiKey` onto it.
