---
type: breaking
release: v0.81.0
---

# A write naming an absent `mustExist` referent answers `422 validation`, not `404`

Before v0.81.0, a write whose body held a `mustExist: true` reference to a
record that does not exist answered `404 not_found`, the status a client
reads as "the record I addressed is gone". From v0.81.0 it answers
`422 validation`, with one `problemDetails` entry per dangling reference,
addressed to the property that holds it and listed beside every other
problem in the same write. The message text is unchanged. A missing source
row on a subject hop follows the same rule.

This hits clients and agents that branch on the status of a `PUT`, `PATCH`
or `POST /api/v1/records`:

```http
POST /api/v1/records
{"kind": "ada.example.com/people/team",
 "properties": {"name": "Nobodies",
                "members": ["ada.example.com/people/person/nobody",
                            "ada.example.com/people/person/nobody-either"]}}

before: 404 {"error": {"code": "not_found", "message": "... reference names ada.example.com/people/person/nobody, which does not exist"}}
after:  422 {"error": {"code": "validation", "message": "...",
             "problemDetails": [{"path": "props.members[0]", "message": "..."},
                                {"path": "props.members[1]", "message": "..."}]}}
```

## What to do

1. Handle a dangling referent under `422`: read `problemDetails[].path` to
   find the property, then fix the value or create the referent first.
2. Keep `404` for "the record at this path does not exist".
