package main

import "strings"

// The packages the driver declares, under the repository's own authority
// (AUTH). Every function is python, so an apply that answers 2xx has run each
// body through the release's runner.
const autoVocabulary = `
kind: substrate.reamde.dev/core/package
metadata:
  id: AUTH/auto
data:
  authority: AUTH
  package: auto
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: AUTH/auto/widget
data:
  authority: AUTH
  package: auto
  names:
    singular: widget
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: what the widget is called
    color:
      type: enum
      values: [red, green]
      description: the widget's color
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: AUTH/auto/gadget
data:
  authority: AUTH
  package: auto
  names:
    singular: gadget
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: what the gadget is called
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: AUTH/auto/flag
data:
  authority: AUTH
  package: auto
  names:
    singular: flag
  purpose: internal
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: a gate a function body waits on
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: AUTH/auto/echo
data:
  authority: AUTH
  package: auto
  names:
    singular: echo
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: the request body the fire saw
    fire:
      type: string
      fts: false
      description: the fire id the fire ran under
---
kind: substrate.reamde.dev/core/function
metadata:
  id: AUTH/auto/mirror
data:
  authority: AUTH
  package: auto
  description: mirrors a widget into a task
  runtime: python
  permissions:
    writes: [AUTH/tasks/task]
  source: |
    def main(input, host):
        env = input["envelope"]
        return {"effects": [{"action": "put", "kind": "AUTH/tasks/task",
                             "id": "t-" + env["change"]["id"],
                             "properties": {"name": "mirror of " + env["record"]["properties"]["name"]}}]}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: AUTH/auto/page
data:
  authority: AUTH
  package: auto
  description: a paged backfill that cannot pass page two until a gate opens
  runtime: python
  permissions:
    reads:
      kinds: [AUTH/auto/flag]
    writes: [AUTH/tasks/task]
  source: |
    FLAG = "AUTH/auto/flag"
    TASK = "AUTH/tasks/task"

    def main(input, host):
        page = input.get("resume") or 0
        if page == 2 and host.records.get(FLAG, "page-gate") is None:
            raise RuntimeError("page gate closed")
        effects = [{"action": "put", "kind": TASK, "id": "p-%d" % page,
                    "properties": {"name": "page %d" % page}}]
        if page < 3:
            return {"effects": effects, "more": {"cursor": page + 1}}
        return {"effects": effects}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: AUTH/auto/hook
data:
  authority: AUTH
  package: auto
  description: echoes a webhook request once its gate opens, and fails until then
  runtime: python
  permissions:
    reads:
      kinds: [AUTH/auto/flag]
    writes: [AUTH/auto/echo]
  source: |
    FLAG = "AUTH/auto/flag"
    ECHO = "AUTH/auto/echo"

    def main(input, host):
        if host.records.get(FLAG, "hook-gate") is None:
            raise RuntimeError("hook gate closed")
        env = input.get("envelope") or {}
        req = env.get("request") or {}
        body = (req.get("body") or {}).get("text") or ""
        host.effects.put(ECHO, "hook-echo", properties={
            "name": body,
            "fire": (env.get("fire") or {}).get("id") or "",
        })
        return {"output": {}}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: AUTH/auto/tick
data:
  authority: AUTH
  package: auto
  description: what the schedule fires; it writes nothing
  runtime: python
  source: |
    def main(input, host):
        return {"output": {}}
`

// renameVocabulary edits widget: `name` becomes `label` and the enum value
// `red` becomes `crimson`, both with live records, so the apply writes the
// rename and the remap as patches of every record holding them.
const renameVocabulary = `
kind: substrate.reamde.dev/core/kind
metadata:
  id: AUTH/auto/widget
data:
  authority: AUTH
  package: auto
  names:
    singular: widget
  displayTemplate: "{label}"
  properties:
    label:
      type: string
      required: true
      renamedFrom: name
      description: what the widget is called
    color:
      type: enum
      values:
        - value: crimson
          renamedFrom: red
        - green
      description: the widget's color
`

// attachVocabulary declares the kind that carries a blob; the second form
// adds an optional property, so records written after it carry a newer kind
// version than those before.
func attachVocabulary(withNotes bool) string {
	notes := ""
	if withNotes {
		notes = `
    notes:
      type: string
      description: what the file is about`
	}
	return `
kind: substrate.reamde.dev/core/package
metadata:
  id: AUTH/attach
data:
  authority: AUTH
  package: attach
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: AUTH/attach/file
data:
  authority: AUTH
  package: attach
  names:
    singular: file
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: the file's display name
    data:
      type: blobref
      description: the bytes` + notes + `
`
}

// credsVocabulary carries three sealed values beside a plain one: what the
// sealed store under the directory holds.
const credsVocabulary = `
kind: substrate.reamde.dev/core/package
metadata:
  id: AUTH/creds
data:
  authority: AUTH
  package: creds
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: AUTH/creds/oauthclient
data:
  authority: AUTH
  package: creds
  names:
    singular: oauthclient
  displayTemplate: "{clientId}"
  properties:
    clientId:
      type: string
      required: true
      description: the OAuth app's client id
    clientSecret:
      type: secret
      description: the OAuth app's client secret
    accessToken:
      type: secret
      description: the bearer the provider issued
    expiresAt:
      type: datetime
      description: when the access token expires
`

func authored(doc, authority string) string { return strings.ReplaceAll(doc, "AUTH", authority) }
