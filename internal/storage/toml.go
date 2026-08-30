package storage

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// document is deliberately small but supports the TOML constructs emitted by CMS:
// scalar keys, arrays, tables and array-of-tables.
type document struct {
	scalars     map[string]map[string]string
	arrays      map[string]map[string][]string
	arrayTables map[string][]map[string]string
}

func parseDocument(input string) (document, error) {
	d := document{scalars: map[string]map[string]string{"": {}}, arrays: map[string]map[string][]string{"": {}}, arrayTables: map[string][]map[string]string{}}
	section := ""
	var current map[string]string
	s := bufio.NewScanner(strings.NewReader(input))
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(stripComment(s.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			section = strings.TrimSpace(line[2 : len(line)-2])
			current = map[string]string{}
			d.arrayTables[section] = append(d.arrayTables[section], current)
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			// TOML permits a subtable immediately below an array-of-tables
			// element, e.g. [[mcps]] followed by [mcps.tools]. The small
			// parser stores those keys on the most recently opened element so
			// context/manifest loaders can consume both spellings.
			if dot := strings.IndexByte(section, '.'); dot > 0 {
				parent := section[:dot]
				if rows := d.arrayTables[parent]; len(rows) > 0 {
					current = rows[len(rows)-1]
					continue
				}
			}
			if d.scalars[section] == nil {
				d.scalars[section] = map[string]string{}
			}
			if d.arrays[section] == nil {
				d.arrays[section] = map[string][]string{}
			}
			current = nil
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 1 {
			return d, fmt.Errorf("invalid TOML at line %d", lineNo)
		}
		key := strings.TrimSpace(line[:i])
		value := strings.TrimSpace(line[i+1:])
		if current != nil {
			current[key] = value
			continue
		}
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			values, err := parseArray(value)
			if err != nil {
				return d, fmt.Errorf("line %d: %w", lineNo, err)
			}
			d.arrays[section][key] = values
		} else {
			d.scalars[section][key] = value
		}
	}
	if err := s.Err(); err != nil {
		return d, err
	}
	return d, nil
}

func stripComment(s string) string {
	quoted := false
	for i, r := range s {
		if r == '"' && (i == 0 || s[i-1] != '\\') {
			quoted = !quoted
		}
		if r == '#' && !quoted {
			return s[:i]
		}
	}
	return s
}

func parseArray(s string) ([]string, error) {
	s = strings.TrimSpace(s[1 : len(s)-1])
	if s == "" {
		return []string{}, nil
	}
	var out []string
	var part strings.Builder
	quoted := false
	for i, r := range s {
		if r == '"' && (i == 0 || s[i-1] != '\\') {
			quoted = !quoted
		}
		if r == ',' && !quoted {
			out = append(out, strings.TrimSpace(part.String()))
			part.Reset()
			continue
		}
		part.WriteRune(r)
	}
	if quoted {
		return nil, fmt.Errorf("unterminated string in array")
	}
	out = append(out, strings.TrimSpace(part.String()))
	for i := range out {
		v, err := parseString(out[i])
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func parseString(v string) (string, error) {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return strconv.Unquote(v)
	}
	if v == "" {
		return "", nil
	}
	return v, nil
}

func required(d document, section, key string) (string, error) {
	v, ok := d.scalars[section][key]
	if !ok {
		return "", fmt.Errorf("missing %s.%s", section, key)
	}
	return parseString(v)
}

func optional(d document, section, key, fallback string) (string, error) {
	v, ok := d.scalars[section][key]
	if !ok {
		return fallback, nil
	}
	return parseString(v)
}

func quote(s string) string { return strconv.Quote(s) }

func boolValue(v string) bool { return strings.EqualFold(strings.TrimSpace(v), "true") }

func intValue(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

func array(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = quote(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
