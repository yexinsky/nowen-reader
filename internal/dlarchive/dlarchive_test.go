package dlarchive

import (
	"os"
	"path/filepath"
	"testing"
)

// TestZip 归档路径固定为 dir/base.zip(不生成 " (2)" 副本)。
func TestZip(t *testing.T) {
	dir := t.TempDir()
	got := Zip(dir, "标题")
	if filepath.Base(got) != "标题.zip" {
		t.Fatalf("归档路径 = %q", got)
	}
	if again := Zip(dir, "标题"); again != got {
		t.Fatalf("同作品应复用同一路径: %q != %q", again, got)
	}
}

// TestExists 预检能识别已归档文件,目录同名不算命中。
func TestExists(t *testing.T) {
	dir := t.TempDir()
	if Exists(dir, "标题") {
		t.Fatal("空目录不应命中")
	}
	if err := os.WriteFile(Zip(dir, "标题"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Exists(dir, "标题") {
		t.Fatal("已存在的归档应命中")
	}
	if Exists(dir, "其他") {
		t.Fatal("不同作品不应命中")
	}
}

// TestReplace 同名覆盖:旧内容被替换、源文件清理、无临时文件残留。
func TestReplace(t *testing.T) {
	srcDir, dstDir := t.TempDir(), t.TempDir()
	dst := Zip(dstDir, "标题")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(srcDir, "pack.zip")
	if err := os.WriteFile(src, []byte("new-payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Replace(src, dst); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-payload" {
		t.Fatalf("目标内容 = %q, 期望 new-payload", data)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("源文件未清理: %v", err)
	}
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "标题.zip" {
		t.Errorf("目录应只剩归档文件,实际: %v", []string{entries[0].Name()})
	}
}

// TestReplaceCreatesDir 目标目录不存在时自动创建(下载目录首次使用)。
func TestReplaceCreatesDir(t *testing.T) {
	src := filepath.Join(t.TempDir(), "pack.zip")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := Zip(filepath.Join(t.TempDir(), "nested", "deeper"), "标题")
	if err := Replace(src, dst); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "payload" {
		t.Fatalf("归档内容 = %q, err = %v", data, err)
	}
}
