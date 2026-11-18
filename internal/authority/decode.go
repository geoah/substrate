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
// A field tagged `authority:"lenient"` is the exception: a value of it that
// does not hold to its type is dropped, as an unknown key is, and the
// document is read without it.
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
		lenient := map[string]bool{}
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			fields[name] = f.Type
			lenient[name] = f.Tag.Get("authority") == "lenient"
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
			held, err := knownJSON(value, ft)
			if err == nil && lenient[name] {
				// The strict pass checks the type, not that it unmarshals
				// into the field: a lenient value must do both or go.
				err = json.Unmarshal(held, reflect.New(ft).Interface())
			}
			switch {
			case err != nil && lenient[name]:
				// Kept as present and empty so a second copy of the key is
				// still a duplicate.
				out[name] = nil
				continue
			case err != nil:
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			out[name] = held
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		if _, err := dec.Token(); err != io.EOF {
			return nil, fmt.Errorf("unexpected trailing data")
		}
		for name, value := range out {
			if value == nil {
				delete(out, name)
			}
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
