package applet

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// The official A2UI v1.0 basic catalog, read once, so the check names the
// component set, the props, the enums and the functions the protocol's own
// renderers draw, and a catalog update is a copy of the published file.
//
//go:embed a2ui/basic_catalog.json
var basicCatalogJSON []byte

// propRole is what a component prop holds, as far as the check reads it.
type propRole int

const (
	roleOther     propRole = iota
	roleDynamic            // a literal, a binding or a function call
	roleChild              // one component id
	roleChildList          // ids, or a template over a data list
	roleAction             // an event or a renderer function call
	roleTabs               // [{title, child}]
	roleOptions            // [{label, value}]
	roleChecks             // [{condition, message}]
	roleEnum               // one of a closed set of strings
)

type propSpec struct {
	role   propRole
	values []string
}

type componentSpec struct {
	props    map[string]propSpec
	required []string
}

type catalog struct {
	components map[string]componentSpec
	functions  map[string]bool
	// functionArgs are each function's required arguments.
	functionArgs map[string][]string
}

// commonProps are the envelope every component carries, from the common
// types rather than each component's own schema.
var commonProps = map[string]propSpec{
	"id":            {role: roleOther},
	"component":     {role: roleOther},
	"catalogId":     {role: roleOther},
	"accessibility": {role: roleOther},
	"metadata":      {role: roleOther},
}

var (
	catalogOnce sync.Once
	theCatalog  *catalog
	catalogErr  error
)

func basicCatalog() *catalog {
	catalogOnce.Do(func() { theCatalog, catalogErr = parseCatalog(basicCatalogJSON) })
	if catalogErr != nil {
		panic(fmt.Sprintf("applet: the embedded A2UI catalog does not parse: %v", catalogErr))
	}
	return theCatalog
}

func parseCatalog(raw []byte) (*catalog, error) {
	var doc struct {
		CatalogID  string                    `json:"catalogId"`
		Components map[string]map[string]any `json:"components"`
		Functions  map[string]any            `json:"functions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	c := &catalog{components: map[string]componentSpec{}, functions: map[string]bool{}, functionArgs: map[string][]string{}}
	for name, schema := range doc.Components {
		spec := componentSpec{props: map[string]propSpec{}}
		collectProps(schema, &spec)
		c.components[name] = spec
	}
	for name, f := range doc.Functions {
		c.functions[name] = true
		fm, _ := f.(map[string]any)
		props, _ := fm["properties"].(map[string]any)
		args, _ := props["args"].(map[string]any)
		req, _ := args["required"].([]any)
		for _, r := range req {
			if s, ok := r.(string); ok {
				c.functionArgs[name] = append(c.functionArgs[name], s)
			}
		}
	}
	if len(c.components) == 0 {
		return nil, fmt.Errorf("catalog %s declares no components", doc.CatalogID)
	}
	return c, nil
}

// collectProps walks a component schema's object and allOf arms.
func collectProps(schema map[string]any, spec *componentSpec) {
	if all, ok := schema["allOf"].([]any); ok {
		for _, arm := range all {
			m, _ := arm.(map[string]any)
			if ref, _ := m["$ref"].(string); strings.HasSuffix(ref, "/Checkable") {
				spec.props["checks"] = propSpec{role: roleChecks}
				continue
			}
			collectProps(m, spec)
		}
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		for name, p := range props {
			if name == "component" {
				continue
			}
			pm, _ := p.(map[string]any)
			spec.props[name] = propSpecOf(name, pm)
		}
	}
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if s, _ := r.(string); s != "" && s != "component" && !slices.Contains(spec.required, s) {
				spec.required = append(spec.required, s)
			}
		}
	}
}

func propSpecOf(name string, p map[string]any) propSpec {
	if ref, _ := p["$ref"].(string); ref != "" {
		switch {
		case strings.HasSuffix(ref, "/ChildList"):
			return propSpec{role: roleChildList}
		case strings.HasSuffix(ref, "/Child"), strings.HasSuffix(ref, "/ComponentId"):
			return propSpec{role: roleChild}
		case strings.HasSuffix(ref, "/Action"):
			return propSpec{role: roleAction}
		case strings.Contains(ref, "/Dynamic"):
			return propSpec{role: roleDynamic}
		}
		return propSpec{role: roleOther}
	}
	if enum, ok := p["enum"].([]any); ok {
		var values []string
		for _, v := range enum {
			if s, ok := v.(string); ok {
				values = append(values, s)
			}
		}
		return propSpec{role: roleEnum, values: values}
	}
	switch name {
	case "tabs":
		return propSpec{role: roleTabs}
	case "options":
		return propSpec{role: roleOptions}
	case "name":
		// Icon's name: a catalog icon, an svgPath object or a binding.
		return propSpec{role: roleDynamic}
	}
	return propSpec{role: roleOther}
}

// inputValueProps are the props a renderer writes back as the owner types:
// each must bind a /ui path, or what was typed is never kept or sent.
var inputValueProps = map[string]bool{
	"TextField": true, "CheckBox": true, "ChoicePicker": true, "Slider": true, "DateTimeInput": true,
}
