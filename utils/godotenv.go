package utils

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
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
		// 统一处理未加引号值、双引号值与行尾注释。
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

// GetEnv 从解析后的环境变量映射中读取键值。
//
// 语义与 Python 的 os.getenv(key, default) 一致：
// 当键不存在时返回 defaultValue；当键存在时返回其原始值（即使为空字符串）。
func GetEnv(envMap map[string]string, key string, defaultValue string) string {
	value, ok := envMap[key]
	if !ok {
		return defaultValue
	}
	return value
}

// GetEnvBool 从解析后的环境变量映射中读取布尔值。
//
// 支持 true/false、1/0、yes/no、on/off（大小写不敏感），
// 解析失败时返回 defaultValue。
func GetEnvBool(envMap map[string]string, key string, defaultValue bool) bool {
	raw := strings.TrimSpace(GetEnv(envMap, key, ""))
	if raw == "" {
		return defaultValue
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return defaultValue
	}
}

// GetEnvInt 从解析后的环境变量映射中读取整数值。
//
// 当键缺失或解析失败时返回 defaultValue。
func GetEnvInt(envMap map[string]string, key string, defaultValue int) int {
	raw := strings.TrimSpace(GetEnv(envMap, key, ""))
	if raw == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// GetEnvIntPtr 从解析后的环境变量映射中读取可空整数值。
//
// 当键缺失或为空时返回 nil；解析失败时也返回 nil。
func GetEnvIntPtr(envMap map[string]string, key string) *int {
	raw := strings.TrimSpace(GetEnv(envMap, key, ""))
	if raw == "" {
		return nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &parsed
}

// GetEnvFloat64Ptr 从解析后的环境变量映射中读取可空浮点值。
//
// 当键缺失或为空时返回 nil；解析失败时也返回 nil。
func GetEnvFloat64Ptr(envMap map[string]string, key string) *float64 {
	raw := strings.TrimSpace(GetEnv(envMap, key, ""))
	if raw == "" {
		return nil
	}

	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

// GetEnvCSV 从解析后的环境变量映射中读取逗号分隔字符串列表。
//
// 读取规则：
// 1. 键缺失时使用 defaultValue；
// 2. 为空字符串时返回 nil；
// 3. 会自动 trim 每个元素并过滤空元素。
func GetEnvCSV(envMap map[string]string, key string, defaultValue string) []string {
	raw := strings.TrimSpace(GetEnv(envMap, key, defaultValue))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// parseValue 解析 .env 的值字段，支持裸值、双引号值和注释尾随内容。
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

// decodeDoubleQuoted 解码双引号包裹的值并返回结束引号索引。
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

// trimInlineComment 移除裸值中的行内注释，仅当 # 前面是空白符时才视为注释。
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

// isSpace 判断字符是否为空格或制表符。
func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}
