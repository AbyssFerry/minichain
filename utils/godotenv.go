package utils

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadEnv reads a .env style file and returns parsed key-value pairs.
// It only parses text and does not mutate process environment variables.
func LoadEnv(filePath string) (map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		eqIdx := strings.Index(line, "=")
		if eqIdx == -1 {
			return nil, fmt.Errorf("invalid .env line %d: missing '='", lineNo)
		}

		key := strings.TrimSpace(line[:eqIdx])
		if key == "" {
			return nil, fmt.Errorf("invalid .env line %d: empty key", lineNo)
		}

		valuePart := strings.TrimSpace(line[eqIdx+1:])
		value, err := parseValue(valuePart, lineNo)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func parseValue(valuePart string, lineNo int) (string, error) {
	trimmed := strings.TrimSpace(valuePart)
	if trimmed == "" {
		return "", nil
	}

	if trimmed[0] != '"' {
		value := trimInlineComment(trimmed)
		return strings.TrimSpace(value), nil
	}

	decoded, endIdx, err := decodeDoubleQuoted(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid .env line %d: %w", lineNo, err)
	}

	tail := strings.TrimSpace(trimmed[endIdx+1:])
	if tail == "" || strings.HasPrefix(tail, "#") {
		return decoded, nil
	}

	return "", fmt.Errorf("invalid .env line %d: unexpected trailing content after quoted value", lineNo)
}

func decodeDoubleQuoted(value string) (string, int, error) {
	var b strings.Builder
	for i := 1; i < len(value); i++ {
		ch := value[i]

		if ch == '"' {
			return b.String(), i, nil
		}

		if ch != '\\' {
			b.WriteByte(ch)
			continue
		}

		i++
		if i >= len(value) {
			return "", 0, fmt.Errorf("unfinished escape sequence")
		}

		switch value[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		default:
			return "", 0, fmt.Errorf("unsupported escape sequence \\%c", value[i])
		}
	}

	return "", 0, fmt.Errorf("missing closing quote")
}

func trimInlineComment(value string) string {
	idx := strings.Index(value, "#")
	if idx == -1 {
		return value
	}

	if idx == 0 {
		return ""
	}

	if isSpace(rune(value[idx-1])) {
		return strings.TrimRightFunc(value[:idx], isSpace)
	}

	return value
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}
