# @substrate/testing

The web kit's test doubles, for tests only.

`FakeSubstrate` is a substrate in memory behind `fetch`: records with
versions, `ifVersion` held as the server holds it (a 409 otherwise), a plain
delete, the list read without a head (as an app token's is) a `first` at a
time with a cursor to the rest, the authorization check, the change feed's
ndjson watch, which stays open and carries each later write, and
`/oauth/revoke`, after which a request with the token is refused with a 401
(`revoked` lists them). `seed` and `change` write as another device
would, `fail` answers the next matching requests with a refusal, `offline`
fails every request as `fetch` does without a network, `compact` moves the
history on so every cursor is stale, and `writes()` lists what it took.

```ts
const fake = new FakeSubstrate()
vi.stubGlobal("fetch", fake.fetch)
fake.fail({ method: "PUT" }, 503, undefined, { "Retry-After": "30" })
await until(() => outbox.getSnapshot().waiting === 0)
```

`setup.ts` installs fake-indexeddb for the workspace's vitest run.
