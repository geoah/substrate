# A2UI v1.0, as published

`basic_catalog.json` and `common_types.json` are the A2UI v1.0 basic catalog
and its common types, copied unchanged from the A2UI project
(https://github.com/a2ui-project/a2ui, `specification/v1_0/json`, as
packaged in `@a2ui/web_core` 0.15.0) under the Apache License 2.0 in
`LICENSE`. `internal/applet` reads the catalog to check an applet's components
against the official component set, props, enums and functions, so a newer
catalog is a copy of the newer files and a run of `go test ./internal/applet/`.
