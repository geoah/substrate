# The notes sample

Package `samples.substrate.reamde.dev/notes`: the smallest bundle that shows
the whole agent loop, and the one to read first. `notekeeper` calls `titler` (a
sub-agent with no tools and no writes), then `stats` (pure Python, no network),
then `savenote`, which writes the one kind the bundle declares. It needs no
network, no credentials and no other bundle's vocabulary.

`bundle.yaml` is the closure, and the whole bundle: the `note` kind, two
functions and two agents. There is nothing to wire, so there is no
`triggers.yaml`. It is the contract; this file is not.

How to install it, call it, key the provider it names and read the threads it
leaves:
[docs/bundles-catalog.md#notes-sample](../../docs/bundles-catalog.md#notes-sample).
