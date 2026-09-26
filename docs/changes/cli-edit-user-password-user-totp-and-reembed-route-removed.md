---
type: breaking
release: v0.72.0
---

# `substratectl edit`, `user password`, `user totp` and `POST /api/v1/embeddings/reembed` are removed

Three `substratectl` commands and one REST route are gone. `substratectl
edit`, `substratectl user password` and `substratectl user totp` are unknown
commands. `POST /api/v1/embeddings/reembed` answers `404`. The auth routes the
two `user` commands called (`POST /password`, `POST /totp/enroll`,
`POST /totp`) are still served; the console's account page uses them.

With the route gone, the `embeddings` entry in
`GET /.well-known/substrate/server.json` lists `"surfaces": ["graphql"]`
instead of `["rest", "graphql"]`. (GraphQL was removed later, and on current
releases the entry lists `["rest"]`.)

## What to do

1. Replace `substratectl edit <kind> <id>` with a get, an edit and an apply:

   ```
   substratectl get ada.example.com/tasks/task t1 -o yaml > t1.yaml
   $EDITOR t1.yaml
   substratectl apply -f t1.yaml
   ```

2. Change a password or a second factor on the console's account page.
3. Replace a call to `POST /api/v1/embeddings/reembed` with the operator
   command on the server's host. It runs beside a live server:

   ```
   substratectl --dsn "$DATABASE_URL" repository reembed <repository>
   ```

   `--all` takes the place of the route's `{"all": true}` body.
