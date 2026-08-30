package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ikts/cms/internal/model"
)

// patchJSONCMCP changes only CMS-owned properties. In particular, comments,
// ordering and formatting outside those properties remain byte-for-byte intact.
func patchJSONCMCP(current []byte, configKey string, servers, tools map[string]any, actions []MCPAction, managed, entries []model.MCPStateEntry) ([]byte, error) {
	data := append([]byte(nil), current...)
	for _, action := range actions {
		switch action.Kind {
		case MCPCreate, MCPUpdate:
			var err error
			data, err = jsoncEnsureObject(data, nil, configKey)
			if err == nil {
				data, err = jsoncSetProperty(data, []string{configKey}, action.Name, servers[action.Name])
			}
			if err != nil {
				return nil, err
			}
		case MCPRemove:
			var err error
			data, err = jsoncRemoveProperty(data, []string{configKey}, action.Name)
			if err != nil {
				return nil, err
			}
		}
	}

	oldToolKeys := managedToolKeys(managed)
	newToolKeys := managedToolKeys(entries)
	for key := range newToolKeys {
		var err error
		data, err = jsoncEnsureObject(data, nil, "tools")
		if err == nil {
			data, err = jsoncSetProperty(data, []string{"tools"}, key, tools[key])
		}
		if err != nil {
			return nil, err
		}
	}
	for key := range oldToolKeys {
		if newToolKeys[key] {
			continue
		}
		var err error
		data, err = jsoncRemoveProperty(data, []string{"tools"}, key)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func managedToolKeys(entries []model.MCPStateEntry) map[string]bool {
	out := map[string]bool{}
	for _, entry := range entries {
		for _, key := range entry.ManagedKeys {
			if strings.HasPrefix(key, "tools.") {
				out[strings.TrimPrefix(key, "tools.")] = true
			}
		}
	}
	return out
}

type jsoncProperty struct {
	key                            string
	keyStart, valueStart, valueEnd int
	comma                          int
}

type jsoncObject struct {
	open, close int
	properties  []jsoncProperty
}

func parseJSONCObject(data []byte, open int) (jsoncObject, error) {
	if open < 0 || open >= len(data) || data[open] != '{' {
		return jsoncObject{}, fmt.Errorf("JSONC value is not an object")
	}
	obj := jsoncObject{open: open, close: -1}
	pos := open + 1
	for {
		pos = skipJSONCTrivia(data, pos)
		if pos >= len(data) {
			return obj, fmt.Errorf("unterminated JSONC object")
		}
		if data[pos] == '}' {
			obj.close = pos
			return obj, nil
		}
		if data[pos] != '"' {
			return obj, fmt.Errorf("expected JSONC object key at byte %d", pos)
		}
		keyStart := pos
		keyEnd, err := scanJSONString(data, pos)
		if err != nil {
			return obj, err
		}
		var key string
		if err := json.Unmarshal(data[pos:keyEnd], &key); err != nil {
			return obj, err
		}
		pos = skipJSONCTrivia(data, keyEnd)
		if pos >= len(data) || data[pos] != ':' {
			return obj, fmt.Errorf("expected ':' after JSONC key %q", key)
		}
		valueStart := skipJSONCTrivia(data, pos+1)
		valueEnd, err := scanJSONCValue(data, valueStart)
		if err != nil {
			return obj, err
		}
		pos = skipJSONCTrivia(data, valueEnd)
		comma := -1
		if pos < len(data) && data[pos] == ',' {
			comma = pos
			pos++
		} else if pos >= len(data) || data[pos] != '}' {
			return obj, fmt.Errorf("expected ',' or '}' after JSONC key %q", key)
		}
		obj.properties = append(obj.properties, jsoncProperty{key: key, keyStart: keyStart, valueStart: valueStart, valueEnd: valueEnd, comma: comma})
	}
}

func scanJSONString(data []byte, start int) (int, error) {
	for i := start + 1; i < len(data); i++ {
		if data[i] == '\\' {
			i++
			continue
		}
		if data[i] == '"' {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated JSON string")
}

func scanJSONCValue(data []byte, start int) (int, error) {
	if start >= len(data) {
		return 0, fmt.Errorf("missing JSONC value")
	}
	if data[start] == '"' {
		return scanJSONString(data, start)
	}
	if data[start] != '{' && data[start] != '[' {
		i := start
		for i < len(data) && data[i] != ',' && data[i] != '}' && data[i] != ']' && data[i] != '/' && data[i] != '\n' && data[i] != '\r' {
			i++
		}
		trimmed := bytes.TrimRight(data[start:i], " \t")
		if len(trimmed) == 0 {
			return 0, fmt.Errorf("missing JSONC value")
		}
		return start + len(trimmed), nil
	}
	stack := []byte{data[start]}
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '"':
			end, err := scanJSONString(data, i)
			if err != nil {
				return 0, err
			}
			i = end - 1
		case '/':
			if i+1 < len(data) && data[i+1] == '/' {
				i += 2
				for i < len(data) && data[i] != '\n' {
					i++
				}
			} else if i+1 < len(data) && data[i+1] == '*' {
				end := bytes.Index(data[i+2:], []byte("*/"))
				if end < 0 {
					return 0, fmt.Errorf("unterminated JSONC comment")
				}
				i += end + 3
			}
		case '{', '[':
			stack = append(stack, data[i])
		case '}', ']':
			want := byte('{')
			if data[i] == ']' {
				want = '['
			}
			if len(stack) == 0 || stack[len(stack)-1] != want {
				return 0, fmt.Errorf("mismatched JSONC delimiter")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated JSONC value")
}

func skipJSONCTrivia(data []byte, pos int) int {
	for pos < len(data) {
		if strings.ContainsRune(" \t\r\n", rune(data[pos])) {
			pos++
			continue
		}
		if pos+1 < len(data) && data[pos] == '/' && data[pos+1] == '/' {
			pos += 2
			for pos < len(data) && data[pos] != '\n' {
				pos++
			}
			continue
		}
		if pos+1 < len(data) && data[pos] == '/' && data[pos+1] == '*' {
			end := bytes.Index(data[pos+2:], []byte("*/"))
			if end < 0 {
				return len(data)
			}
			pos += end + 4
			continue
		}
		break
	}
	return pos
}

func locateJSONCObject(data []byte, path []string) (jsoncObject, error) {
	start := skipJSONCTrivia(data, 0)
	obj, err := parseJSONCObject(data, start)
	if err != nil {
		return obj, err
	}
	for _, key := range path {
		found := false
		for _, prop := range obj.properties {
			if prop.key == key {
				obj, err = parseJSONCObject(data, skipJSONCTrivia(data, prop.valueStart))
				if err != nil {
					return obj, fmt.Errorf("JSONC field %q is not an object", key)
				}
				found = true
				break
			}
		}
		if !found {
			return obj, fmt.Errorf("JSONC object path %q does not exist", strings.Join(path, "."))
		}
	}
	return obj, nil
}

func jsoncEnsureObject(data []byte, path []string, key string) ([]byte, error) {
	obj, err := locateJSONCObject(data, path)
	if err != nil {
		return nil, err
	}
	for _, prop := range obj.properties {
		if prop.key == key {
			if _, err := parseJSONCObject(data, prop.valueStart); err != nil {
				return nil, fmt.Errorf("JSONC field %q is not an object", key)
			}
			return data, nil
		}
	}
	return jsoncSetProperty(data, path, key, map[string]any{})
}

func jsoncSetProperty(data []byte, path []string, key string, value any) ([]byte, error) {
	obj, err := locateJSONCObject(data, path)
	if err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	for _, prop := range obj.properties {
		if prop.key == key {
			encoded = indentJSONValue(encoded, lineIndent(data, prop.keyStart)+"  ")
			return replaceJSONCRange(data, prop.valueStart, prop.valueEnd, encoded), nil
		}
	}
	indent := lineIndent(data, obj.close)
	childIndent := indent + "  "
	encoded = indentJSONValue(encoded, childIndent)
	property := []byte(childIndent + strconv.Quote(key) + ": " + string(encoded))
	if len(obj.properties) == 0 {
		insert := append([]byte("\n"), property...)
		return replaceJSONCRange(data, obj.open+1, obj.open+1, insert), nil
	}
	last := obj.properties[len(obj.properties)-1]
	trailingComma := last.comma >= 0
	if last.comma < 0 {
		data = replaceJSONCRange(data, last.valueEnd, last.valueEnd, []byte(","))
		obj.close++
	}
	insertAt := jsoncClosingLineStart(data, obj.close)
	if trailingComma {
		property = append(property, ',')
	}
	property = append(property, '\n')
	if insertAt == obj.close {
		property = append([]byte("\n"), property...)
	}
	return replaceJSONCRange(data, insertAt, insertAt, property), nil
}

func jsoncRemoveProperty(data []byte, path []string, key string) ([]byte, error) {
	obj, err := locateJSONCObject(data, path)
	if err != nil {
		// Removing an already absent parent is idempotent.
		return data, nil
	}
	for i, prop := range obj.properties {
		if prop.key != key {
			continue
		}
		if prop.comma >= 0 {
			return replaceJSONCRange(data, prop.keyStart, prop.comma+1, nil), nil
		}
		if i > 0 && obj.properties[i-1].comma >= 0 {
			return replaceJSONCRange(data, obj.properties[i-1].comma, prop.valueEnd, nil), nil
		}
		return replaceJSONCRange(data, prop.keyStart, prop.valueEnd, nil), nil
	}
	return data, nil
}

func indentJSONValue(value []byte, continuationIndent string) []byte {
	return bytes.ReplaceAll(value, []byte("\n"), []byte("\n"+continuationIndent))
}

func lineIndent(data []byte, pos int) string {
	start := bytes.LastIndexByte(data[:pos], '\n') + 1
	i := start
	for i < pos && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	return string(data[start:i])
}

func jsoncClosingLineStart(data []byte, close int) int {
	start := bytes.LastIndexByte(data[:close], '\n') + 1
	if len(bytes.TrimSpace(data[start:close])) == 0 {
		return start
	}
	return close
}

func replaceJSONCRange(data []byte, start, end int, replacement []byte) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(replacement))
	out = append(out, data[:start]...)
	out = append(out, replacement...)
	out = append(out, data[end:]...)
	return out
}
