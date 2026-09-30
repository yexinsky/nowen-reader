package jm

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeName 长名/非法字符清洗口径(移植 JMComic-qt GetCanSaveName)。
func TestSanitizeName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"非法字符删除", `a/b\c:d*e?f"g<h>i|j`, "abcdefghij"},
		{"控制字符删除", "a\tb\r\nc\x00d", "abcd"},
		{"首尾空格与尾点", "  name...  ", "name"},
		{"保留中间点", "第01话_後編.5", "第01话_後編.5"},
		{"纯非法字符", "///", ""},
		{"空字符串", "", ""},
	}
	for _, tc := range cases {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("%s: SanitizeName(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestSanitizeNameTruncatesTo83Runes 超长名称截断到 254//3-1=83 字符(非字节)。
func TestSanitizeNameTruncatesTo83Runes(t *testing.T) {
	long := strings.Repeat("漫", 200)
	got := SanitizeName(long)
	if n := len([]rune(got)); n != maxNameRunes {
		t.Fatalf("长度 = %d 字符, want %d", n, maxNameRunes)
	}
	if maxNameRunes != 83 {
		t.Fatalf("maxNameRunes = %d, want 83", maxNameRunes)
	}

	// ASCII 名称同样按字符截断(与 Qt 版一致:258 字符 → 83)
	ascii := strings.Repeat("a", 300)
	if n := len([]rune(SanitizeName(ascii))); n != maxNameRunes {
		t.Fatalf("ASCII 截断长度 = %d, want %d", n, maxNameRunes)
	}

	// 截断处落在空格/点上时二次清洗
	mixed := strings.Repeat("漫", 82) + " . " + strings.Repeat("画", 10)
	got2 := SanitizeName(mixed)
	if strings.HasSuffix(got2, " ") || strings.HasSuffix(got2, ".") {
		t.Fatalf("截断后仍以空格/点结尾: %q", got2)
	}
}

// TestSafeNameFallback 空名回退 aid/pid,再回退 untitled。
func TestSafeNameFallback(t *testing.T) {
	if got := SafeName("", "123456"); got != "123456" {
		t.Errorf("SafeName 回退 = %q, want 123456", got)
	}
	if got := SafeName("///", ""); got != "untitled" {
		t.Errorf("SafeName 兜底 = %q, want untitled", got)
	}
	if got := SafeName("正常标题", "123"); got != "正常标题" {
		t.Errorf("SafeName 正常 = %q", got)
	}
}

// TestChapterDirNaming 章节目录命名与超长标题的实际落盘(Windows 路径长度兜底)。
func TestChapterDirNaming(t *testing.T) {
	root := t.TempDir()
	m := &DownloadManager{tempRoot: root}

	// 短标题:第01话_标题(总章数 <100 补零 2 位)
	dir := m.chapterDir(root, 0, 9, downloadEpisode{Pid: "1", Title: "序章"})
	if base := filepath.Base(dir); base != "第01话_序章" {
		t.Errorf("章节目录名 = %q, want 第01话_序章", base)
	}
	// 100 章以上补零 3 位
	dir = m.chapterDir(root, 4, 120, downloadEpisode{Pid: "5", Title: "后篇"})
	if base := filepath.Base(dir); base != "第005话_后篇" {
		t.Errorf("章节目录名 = %q, want 第005话_后篇", base)
	}
	// 标题为空 → 仅第NN话
	dir = m.chapterDir(root, 11, 20, downloadEpisode{Pid: "9", Title: ""})
	if base := filepath.Base(dir); base != "第12话" {
		t.Errorf("空标题章节目录名 = %q, want 第12话", base)
	}

	// 深层父目录:名称不应被过度截断(路径预算只按父目录计算,不含名称自身)
	deep := filepath.Join(t.TempDir(), "a", "b", "c", "d", "e", "f", "g", "h")
	title := "[スタジオ山ロマン] 阴キャの俺がカースト上位女に復讐パコパコ下剋上"
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	dir = m.chapterDir(deep, 0, 3, downloadEpisode{Pid: "1", Title: title})
	if base := filepath.Base(dir); base != "第01话_"+title {
		t.Fatalf("深层目录下章节名被截断: %q", base)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("深层长名目录创建失败: %v", err)
	}

	// 超长 CJK 标题:清洗后必须能真实创建(且不超全路径预算)
	longTitle := strings.Repeat("超长章节标题测试", 30)
	dir = m.chapterDir(root, 0, 3, downloadEpisode{Pid: "1", Title: longTitle})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建长名章节目录失败: %v", err)
	}
	if len(filepath.Base(dir)) > maxFullPathLen {
		t.Errorf("章节目录名过长: %d", len(filepath.Base(dir)))
	}
	if err := os.WriteFile(filepath.Join(dir, "0001.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatalf("长名目录内写文件失败: %v", err)
	}
}

// TestFitPathName 超长名称按目标目录收紧。
func TestFitPathName(t *testing.T) {
	deepDir := filepath.Join(`D:\`, strings.Repeat("verylongsegment/", 12), "target")
	name := strings.Repeat("名", 83)
	got := fitPathName(deepDir, name, len(".zip")+8)
	if n := len([]rune(got)); n >= len([]rune(name)) {
		t.Errorf("未收紧:%d >= %d", n, len([]rune(name)))
	}
	if total := len(deepDir) + 1 + len(got) + len(".zip") + 8; total > maxFullPathLen {
		t.Errorf("全路径仍超预算: %d > %d", total, maxFullPathLen)
	}
}

// TestUniquePath 同名 zip 递增后缀,绝不覆盖。
func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	first := uniquePath(dir, "标题", ".zip")
	if filepath.Base(first) != "标题.zip" {
		t.Fatalf("首个路径 = %q", first)
	}
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := uniquePath(dir, "标题", ".zip")
	if filepath.Base(second) != "标题 (2).zip" {
		t.Fatalf("第二个路径 = %q", second)
	}
	if err := os.WriteFile(second, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	third := uniquePath(dir, "标题", ".zip")
	if filepath.Base(third) != "标题 (3).zip" {
		t.Fatalf("第三个路径 = %q", third)
	}
}

// TestPackZip 打包:章节目录/页面条目、可被标准 zip 读取、png 与 jpg 混合。
func TestPackZip(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{
		"第01话_序章/0001.jpg": "page1",
		"第01话_序章/0002.jpg": "page2",
		"第02话_终章/0001.jpg": "page3",
	}
	for rel, content := range files {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	zipPath := filepath.Join(src, "pack.zip")
	size, err := packZip(context.Background(), src, zipPath)
	if err != nil {
		t.Fatalf("packZip: %v", err)
	}
	if size <= 0 {
		t.Fatalf("zip 大小 = %d", size)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("zip.OpenReader: %v", err)
	}
	defer zr.Close()

	got := map[string]string{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, _ := rc.Read(buf)
		rc.Close()
		got[f.Name] = string(buf[:n])
	}
	if len(got) != len(files) {
		t.Fatalf("zip 条目数 = %d, want %d (%v)", len(got), len(files), got)
	}
	for rel, content := range files {
		if got[rel] != content {
			t.Errorf("条目 %q 内容 = %q, want %q", rel, got[rel], content)
		}
	}
	// zip 自身不得入包
	if _, ok := got["pack.zip"]; ok {
		t.Error("zip 自身被打进包里")
	}
}

// TestPackZipCanceled 取消后打包中止。
func TestPackZipCanceled(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "0001.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := packZip(ctx, src, filepath.Join(src, "pack.zip")); err == nil {
		t.Fatal("取消后 packZip 应返回错误")
	}
}

// TestDownloadRequestValidation 建任务入参校验(白名单由 handler 保证)。
func TestDownloadRequestValidation(t *testing.T) {
	svc := NewService(t.TempDir(), "")
	m := svc.Downloads()

	if _, err := m.Start(DownloadRequest{DestDir: t.TempDir()}); err == nil {
		t.Error("缺少 aid/pids 应报错")
	}
	if _, err := m.Start(DownloadRequest{Aid: "1"}); err == nil {
		t.Error("缺少目录应报错")
	}
	if _, err := m.Start(DownloadRequest{Aid: "1", DestDir: "relative/dir"}); err == nil {
		t.Error("相对路径应报错")
	}
}

// TestMoveFile 归档移动:rename 与跨盘拷贝回退路径都可用。
func TestMoveFile(t *testing.T) {
	srcDir, dstDir := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "a.zip")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstDir, "b.zip")
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "payload" {
		t.Fatalf("目标内容 = %q, err = %v", data, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("源文件未清理: %v", err)
	}
}
