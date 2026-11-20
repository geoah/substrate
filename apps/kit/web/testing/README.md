# @substrate/testing

The web kit's test doubles, for tests only.

`FakeSubstrate` is a substrate in memory behind `fetch`: records with
versions, `ifVersion` held as the server holds it (a 409 otherwise), a plain
delete, the list read without a head (as an app token's is) a `first` at a
time with a cursor to the rest, the authorization check, the change feed's
ndjson watch, which stays open and carries each later write, the aggregate
read, answered from what `answerAggregate(match, answer)` registers (a read
nothing matches is a 422) and kept in `aggregates`, the sign-in's
`/oauth/token` (minting the tokens `mint` names, for `repository`), and
`/oauth/revoke`, after which a request with the token is refused with a 401
(`revoked` lists them), as one with no token is. `seed` and `change` write as another device
would, `fail` answers the next matching requests with a refusal, `offline`
fails every request as `fetch` does without a network, `compact` moves the
history on so every cursor is stale, `undeclared` holds the kinds a list, a
watch and an aggregate read refuse with a 404, as the server refuses a kind
the repository never declared, and `writes()` lists what it took.

```ts
const fake = new FakeSubstrate()
vi.stubGlobal("fetch", fake.fetch)
fake.fail({ method: "PUT" }, 503, undefined, { "Retry-After": "30" })
await until(() => outbox.getSnapshot().waiting === 0)

fake.answerAggregate((q) => q.aggregate.metric?.ref === steps, {
  exact: true,
  rows: [],
  head: { changelog: 0, generation: "" },
})
```

`FakeLocks` is Web Locks in memory, which jsdom lacks: one exclusive lock
per name, granted in the order asked. `hold(name)` takes a lock until the
answer lets it go, so a test lines up two tabs' session changes behind it
and lets them run in the order it asked them; `install()` puts it at
`navigator.locks`.

```ts
const locks = new FakeLocks()
const uninstall = locks.install()
const release = await locks.hold("substrate.session.template")
const out = tabB.signOut(a) // waits for the lock
release()
```

`setup.ts` installs fake-indexeddb for the workspace's vitest run.
