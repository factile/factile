package conceptschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// decodeJSON rejects duplicate keys, non-JSON input and excessive nesting.
func decodeJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 128 {
			return nil, &ResourceError{"schema_nesting"}
		}
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch token {
		case json.Delim('{'):
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("non-string key")
				}
				if _, exists := m[name]; exists {
					return nil, fmt.Errorf("duplicate key")
				}
				v, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				m[name] = v
			}
			_, err = d.Token()
			return m, err
		case json.Delim('['):
			list := []any{}
			for d.More() {
				v, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err = d.Token()
			return list, err
		default:
			return token, nil
		}
	}
	v, err := read(1)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON input")
	}
	return v, nil
}

func pointer(root any, ref string) (any, string, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, "", false
	}
	p, err := url.PathUnescape(ref[1:])
	if err != nil || !utf8.ValidString(p) {
		return nil, "", false
	}
	v := root
	for _, part := range strings.Split(p[1:], "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				if i+1 == len(part) || part[i+1] != '0' && part[i+1] != '1' {
					return nil, "", false
				}
				i++
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch current := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = current[part]
			if !ok {
				return nil, "", false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || strconv.Itoa(index) != part || index >= len(current) {
				return nil, "", false
			}
			v = current[index]
		default:
			return nil, "", false
		}
	}
	return v, p, true
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
