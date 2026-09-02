# pebble: voice capture from the Pebble Index 01 ring

The Pebble app on the phone POSTs each voice capture from the ring to a
webhook. This bundle receives those requests on one public URL, saves every
capture as a `recording`, and, when the capture came from a press-and-hold,
also writes an `instruction` that an agent turns into tasks.

```
ring app  --POST (multipart)-->  pebble-webhook trigger  -->  ingest (function)
                                                               ├── recording        every capture
                                                               └── instruction     X-Pebble-Mode: agent
instruction (create)  -->  pebble-on-instruction trigger  -->  assistant (agent)  -->  task
```

The app sends `multipart/form-data` with four parts: `transcription` (text),
`audio` (audio/mp4), `recordedAt` (milliseconds since the Unix epoch, as
text) and `client` (the text `ring`). The host parses the parts and stores the
audio in the repository's blob store before the function runs; the function
reads the blob digest off the envelope and writes it to the recording's
`audio` property.

The two gestures are told apart by a custom header, not by URL. Single press
sends `X-Pebble-Mode: note`; press-and-hold sends `X-Pebble-Mode: agent`. A
request with no header, or an unknown value, is saved as a note.

## Install

From this directory:

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
```

The Registry page in the console installs the same closure and the same two
triggers. The bundle requires `tasks.substrate.reamde.dev`, which the assistant
writes; install that first if the repository does not carry it.

## The URL

The endpoint is `POST https://<your substrate host>/webhooks/<username>/pebble-webhook`.
`substratectl trigger status` prints the path in its `WEBHOOK` column.

The endpoint is open as shipped. To require a credential, set
`source.webhook.key` on the `pebble-webhook` trigger (16 to 128 characters of
`[A-Za-z0-9_-]`); the server then accepts the key as a trailing path segment,
as `?key=<key>`, or as `Authorization: Bearer <key>`.

## Ring app setup

In the Pebble app, open the Index settings and add the webhook twice, both
times against the URL above:

1. Single press: custom header `X-Pebble-Mode` with value `note`.
2. Press-and-hold: custom header `X-Pebble-Mode` with value `agent`.

Set "Send" to transcription and recording on both. Transcription only also
works; the recording then has no `audio` property.

## What lands

- One `pebble.bundles.substrate.reamde.dev/recording` per capture, titled by
  its transcription, with `recordedAt`, `mode`, `client` and `audio`. Its id
  is derived from the webhook fire id, so a retried delivery updates the same
  record.
- In agent mode, one `pebble.bundles.substrate.reamde.dev/instruction` at the
  same id, pointing back at the recording through its `recording` property.
- The `pebble-on-instruction` trigger delivers each new instruction to the
  `assistant` agent, which checks open tasks through the `graphql` host
  function and creates `tasks.substrate.reamde.dev/task` records through
  `mutate`. The agent names the `default` provider, so the repository needs an
  `llmprovider` record at that id; the notes example's README shows how to
  write one.

```sh
substratectl get recording
substratectl get instruction
substratectl get task
```

## Mimicking the ring with curl

```sh
printf '' > empty.m4a
curl -s -X POST "https://<your substrate host>/webhooks/<username>/pebble-webhook" \
  -H "X-Pebble-Mode: agent" \
  -F "transcription=call the dentist tomorrow morning" \
  -F "recordedAt=$(( $(date +%s) * 1000 ))" \
  -F "client=ring" \
  -F "audio=@empty.m4a;type=audio/mp4;filename=recording.m4a"
```

Change the header to `note` to save a recording without an instruction.
