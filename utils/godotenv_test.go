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
