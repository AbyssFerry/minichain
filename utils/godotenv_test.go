package utils

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempEnvFile 在临时目录创建 .env 文件并返回其路径。
func writeTempEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp env file: %v", err)
	}
	return path
}

// TestLoadEnv_BasicAndDuplicateKey 验证基础解析以及重复键覆盖行为。
func TestLoadEnv_BasicAndDuplicateKey(t *testing.T) {
	path := writeTempEnvFile(t, "A=1\nB=2\nA=3\n")

	got, err := LoadEnv(path)
	if err != nil {
		t.Fatalf("LoadEnv returned error: %v", err)
	}

	if got["A"] != "3" {
		t.Fatalf("expected A=3, got %q", got["A"])
	}
	if got["B"] != "2" {
		t.Fatalf("expected B=2, got %q", got["B"])
	}
}

// TestLoadEnv_IgnoreBlankAndComments 验证空行与注释行会被忽略。
func TestLoadEnv_IgnoreBlankAndComments(t *testing.T) {
	path := writeTempEnvFile(t, "\n# comment\nX=ok\n\n# another\n")

	got, err := LoadEnv(path)
	if err != nil {
		t.Fatalf("LoadEnv returned error: %v", err)
	}

	if len(got) != 1 || got["X"] != "ok" {
		t.Fatalf("unexpected parsed result: %#v", got)
	}
}

// TestLoadEnv_InlineComment 验证裸值行内注释与 # 字符边界规则。
func TestLoadEnv_InlineComment(t *testing.T) {
	path := writeTempEnvFile(t, "A=hello # comment\nB=a#b\nC=# only comment\n")

	got, err := LoadEnv(path)
	if err != nil {
		t.Fatalf("LoadEnv returned error: %v", err)
	}

	if got["A"] != "hello" {
		t.Fatalf("expected A=hello, got %q", got["A"])
	}
	if got["B"] != "a#b" {
		t.Fatalf("expected B=a#b, got %q", got["B"])
	}
	if got["C"] != "" {
		t.Fatalf("expected C empty, got %q", got["C"])
	}
}

// TestLoadEnv_DoubleQuotedValues 验证双引号值及转义序列解码。
func TestLoadEnv_DoubleQuotedValues(t *testing.T) {
	path := writeTempEnvFile(t, "A=\"hello\"\nB=\"a#b\"\nC=\"line1\\nline2\"\nD=\"x\\\"y\\\\z\"\nE=\"ok\"   # trailing comment\n")

	got, err := LoadEnv(path)
	if err != nil {
		t.Fatalf("LoadEnv returned error: %v", err)
	}

	if got["A"] != "hello" {
		t.Fatalf("expected A=hello, got %q", got["A"])
	}
	if got["B"] != "a#b" {
		t.Fatalf("expected B=a#b, got %q", got["B"])
	}
	if got["C"] != "line1\nline2" {
		t.Fatalf("expected decoded newline for C, got %q", got["C"])
	}
	if got["D"] != "x\"y\\z" {
		t.Fatalf("expected escaped quote and slash for D, got %q", got["D"])
	}
	if got["E"] != "ok" {
		t.Fatalf("expected E=ok, got %q", got["E"])
	}
}

// TestLoadEnv_InvalidLine 验证非法行格式会返回错误。
func TestLoadEnv_InvalidLine(t *testing.T) {
	path := writeTempEnvFile(t, "A=1\nINVALID\n")

	_, err := LoadEnv(path)
	if err == nil {
		t.Fatal("expected error for invalid line, got nil")
	}
}

// TestLoadEnv_InvalidQuotedValue 验证非法双引号值会返回错误。
func TestLoadEnv_InvalidQuotedValue(t *testing.T) {
	tests := []string{
		"A=\"abc\n",
		"A=\"a\\q\"\n",
		"A=\"ok\"trailing\n",
	}

	for _, tc := range tests {
		path := writeTempEnvFile(t, tc)
		_, err := LoadEnv(path)
		if err == nil {
			t.Fatalf("expected error for input %q, got nil", tc)
		}
	}
}

// TestLoadEnv_MissingFile 验证缺失文件时返回错误。
func TestLoadEnv_MissingFile(t *testing.T) {
	_, err := LoadEnv("not-exists-file.env")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestGetEnv_DefaultBehavior 验证 GetEnv 与 Python os.getenv 一致的默认值行为。
func TestGetEnv_DefaultBehavior(t *testing.T) {
	envMap := map[string]string{
		"MODEL": "gpt-4.1",
		"EMPTY": "",
	}

	if got := GetEnv(envMap, "MODEL", "fallback-model"); got != "gpt-4.1" {
		t.Fatalf("expected existing key value, got %q", got)
	}

	if got := GetEnv(envMap, "MISSING", "fallback"); got != "fallback" {
		t.Fatalf("expected default for missing key, got %q", got)
	}

	if got := GetEnv(envMap, "EMPTY", "fallback-empty"); got != "" {
		t.Fatalf("expected empty string for existing empty key, got %q", got)
	}
}

// TestGetEnvBool 验证布尔读取函数的解析与默认值行为。
func TestGetEnvBool(t *testing.T) {
	envMap := map[string]string{
		"TRUE_1":  "true",
		"TRUE_2":  "YES",
		"FALSE_1": "0",
		"INVALID": "not-bool",
	}

	if !GetEnvBool(envMap, "TRUE_1", false) {
		t.Fatal("expected TRUE_1 to parse as true")
	}
	if !GetEnvBool(envMap, "TRUE_2", false) {
		t.Fatal("expected TRUE_2 to parse as true")
	}
	if GetEnvBool(envMap, "FALSE_1", true) {
		t.Fatal("expected FALSE_1 to parse as false")
	}
	if !GetEnvBool(envMap, "INVALID", true) {
		t.Fatal("expected INVALID to fallback to default true")
	}
	if GetEnvBool(envMap, "MISSING", false) {
		t.Fatal("expected MISSING to fallback to default false")
	}
}

// TestGetEnvIntAndPtr 验证整数读取函数与可空指针行为。
func TestGetEnvIntAndPtr(t *testing.T) {
	envMap := map[string]string{
		"OK":       "42",
		"BAD":      "x",
		"EMPTY":    "",
		"NEGATIVE": "-7",
	}

	if got := GetEnvInt(envMap, "OK", 9); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := GetEnvInt(envMap, "BAD", 9); got != 9 {
		t.Fatalf("expected default 9 for invalid int, got %d", got)
	}
	if got := GetEnvInt(envMap, "MISSING", 11); got != 11 {
		t.Fatalf("expected default 11 for missing key, got %d", got)
	}

	if got := GetEnvIntPtr(envMap, "OK"); got == nil || *got != 42 {
		t.Fatalf("expected int pointer 42, got %#v", got)
	}
	if got := GetEnvIntPtr(envMap, "NEGATIVE"); got == nil || *got != -7 {
		t.Fatalf("expected int pointer -7, got %#v", got)
	}
	if got := GetEnvIntPtr(envMap, "BAD"); got != nil {
		t.Fatalf("expected nil for invalid int pointer, got %#v", got)
	}
	if got := GetEnvIntPtr(envMap, "EMPTY"); got != nil {
		t.Fatalf("expected nil for empty int pointer, got %#v", got)
	}
}

// TestGetEnvFloat64Ptr 验证浮点读取函数行为。
func TestGetEnvFloat64Ptr(t *testing.T) {
	envMap := map[string]string{
		"OK":    "0.25",
		"BAD":   "bad",
		"EMPTY": "",
	}

	if got := GetEnvFloat64Ptr(envMap, "OK"); got == nil || *got != 0.25 {
		t.Fatalf("expected float pointer 0.25, got %#v", got)
	}
	if got := GetEnvFloat64Ptr(envMap, "BAD"); got != nil {
		t.Fatalf("expected nil for invalid float pointer, got %#v", got)
	}
	if got := GetEnvFloat64Ptr(envMap, "EMPTY"); got != nil {
		t.Fatalf("expected nil for empty float pointer, got %#v", got)
	}
}

// TestGetEnvCSV 验证 CSV 列表读取与空值过滤行为。
func TestGetEnvCSV(t *testing.T) {
	envMap := map[string]string{
		"STOP":       " END, STOP ,,DONE ",
		"ONLY_EMPTY": " , , ",
	}

	got := GetEnvCSV(envMap, "STOP", "")
	if len(got) != 3 {
		t.Fatalf("expected 3 stop items, got %d (%#v)", len(got), got)
	}
	if got[0] != "END" || got[1] != "STOP" || got[2] != "DONE" {
		t.Fatalf("unexpected stop list: %#v", got)
	}

	if got := GetEnvCSV(envMap, "ONLY_EMPTY", ""); got != nil {
		t.Fatalf("expected nil for only-empty csv, got %#v", got)
	}
	if got := GetEnvCSV(envMap, "MISSING", ""); got != nil {
		t.Fatalf("expected nil for missing csv, got %#v", got)
	}
	if got := GetEnvCSV(envMap, "MISSING", "A,B"); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("expected default csv fallback [A B], got %#v", got)
	}
}
