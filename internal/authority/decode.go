package authority

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// knownJSON keeps exact known keys and checks their types recursively. Unknown
// keys belong to newer readers; known nulls and duplicate keys are malformed.
func knownJSON(raw []byte, t reflect.Type) ([]byte, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("a known authority document value is null")
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		dec := json.NewDecoder(bytes.NewReader(raw))
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if tok != json.Delim('{') {
			return nil, fmt.Errorf("expected an object")
		}
		fields := map[string]reflect.Type{}
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			fields[name] = f.Type
		}
		out := map[string]json.RawMessage{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("expected an object key")
			}
			var value json.RawMessage
			if err := dec.Decode(&value); err != nil {
				return nil, err
			}
			ft, known := fields[name]
			if !known {
				continue
			}
			if _, exists := out[name]; exists {
				return nil, fmt.Errorf("duplicate field %q", name)
			}
			value, err = knownJSON(value, ft)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			out[name] = value
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		if _, err := dec.Token(); err != io.EOF {
			return nil, fmt.Errorf("unexpected trailing data")
		}
		return json.Marshal(out)
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, err
		}
		for i, v := range values {
			var err error
			values[i], err = knownJSON(v, t.Elem())
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(values)
	default:
		value := reflect.New(t).Interface()
		if err := json.Unmarshal(raw, value); err != nil {
			return nil, err
		}
		return raw, nil
	}
}
