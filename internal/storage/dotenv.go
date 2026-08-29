package storage

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadDotEnvValue reads one value from a dotenv file without exporting or
// logging it. Process environment variables should be checked first by the
// caller so explicit shell configuration always wins.
func LoadDotEnvValue(path, key string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for lineNo, raw := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			return "", fmt.Errorf("invalid .env entry at line %d", lineNo+1)
		}
		name := strings.TrimSpace(line[:i])
		if name != key {
			continue
		}
		value := stripDotEnvComment(strings.TrimSpace(line[i+1:]))
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			return strconv.Unquote(value)
		}
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1], nil
		}
		return value, nil
	}
	return "", nil
}

func stripDotEnvComment(value string) string {
	double, single := false, false
	for i, r := range value {
		switch r {
		case '"':
			if !single {
				double = !double
			}
		case '\'':
			if !double {
				single = !single
			}
		case '#':
			if !double && !single && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
				return strings.TrimSpace(value[:i])
			}
		}
	}
	return strings.TrimSpace(value)
}
