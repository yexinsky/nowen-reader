package jm

// 真实上游端到端测试(默认跳过,需显式开启):
//
//	JM_LIVE_TEST=1 go test ./internal/jm/ -run TestLiveDownload -v -timeout 30m
//	可选:JM_LIVE_PROXY=http://127.0.0.1:10809(默认值)、JM_LIVE_AID=123456(默认取最新列表第 1 部)
//
// 覆盖链路:最新列表取 aid → 建下载任务 → 逐章抓图(经代理) → 打包 zip →
// 归档到目标目录 → 清理临时沙箱;校验 zip 内容与目录清理。

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveDownloadPipeline(t *testing.T) {
	if os.Getenv("JM_LIVE_TEST") == "" {
		t.Skip("需要 JM_LIVE_TEST=1 才执行真实上游测试")
	}
	proxy := os.Getenv("JM_LIVE_PROXY")
	if proxy == "" {
		proxy = "http://127.0.0.1:10809"
	}
	dataDir := t.TempDir()
	destDir := filepath.Join(t.TempDir(), "download-test")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	svc := NewService(dataDir, proxy)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	aid := os.Getenv("JM_LIVE_AID")
	title := ""
	if aid == "" {
		page, err := svc.AnonClient().ComicsLatest(ctx, 1)
		if err != nil {
			t.Fatalf("最新列表获取失败(代理 %s): %v", proxy, err)
		}
		list := listOf(dataMap(page)["list"])
		if len(list) == 0 {
			t.Fatal("最新列表为空")
		}
		first := dataMap(list[0])
		aid = fieldStr(first, "aid")
		title = fieldStr(first, "title")
	}
	t.Logf("目标漫画: aid=%s title=%q", aid, title)

	task, err := svc.Downloads().Start(DownloadRequest{
		Aid:       aid,
		Title:     title,
		DestDir:   destDir,
		DestLabel: "测试目录(不入库)",
	})
	if err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	t.Logf("任务 %s 已创建", task.ID)

	deadline := time.Now().Add(20 * time.Minute)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("超时:任务未结束(状态 %s)", svc.Downloads().Get(task.ID).Status)
		}
		time.Sleep(2 * time.Second)
		cur := svc.Downloads().Get(task.ID)
		if cur == nil {
			t.Fatal("任务丢失")
		}
		switch cur.Status {
		case DownloadDone:
			t.Logf("完成:zip=%s 大小=%d 图片=%d/%d 警告=%q",
				cur.ZipPath, cur.ZipSize, cur.DoneImages, cur.TotalImages, cur.Warning)
			verifyZip(t, cur.ZipPath, cur.Title)
			verifySandboxClean(t, svc.Downloads().TempRoot(), task.ID)
			if cur.DoneImages == 0 {
				t.Error("完成但 0 张图片")
			}
			return
		case DownloadFailed, DownloadCanceled:
			t.Fatalf("任务结束于 %s: %s", cur.Status, cur.Error)
		default:
			// 进度日志(便于观察抓图速率)
			if cur.Status == DownloadRunning && cur.DoneImages%10 == 0 {
				t.Logf("进度 %s: %d 章完成 / %d 章,图片 %d 张",
					cur.Status, countDoneChapters(cur), len(cur.Chapters), cur.DoneImages)
			}
		}
	}
}

func countDoneChapters(t *DownloadTask) int {
	n := 0
	for _, c := range t.Chapters {
		if c.State == ChapterDone {
			n++
		}
	}
	return n
}

// verifyZip 校验 zip:可打开、含 jpg 页面、章节目录命名符合「第NN话」。
func verifyZip(t *testing.T, zipPath, title string) {
	t.Helper()
	if zipPath == "" {
		t.Fatal("zipPath 为空")
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("zip 打开失败: %v", err)
	}
	defer zr.Close()

	pages, chapters := 0, map[string]int{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(f.Name), ".jpg") {
			t.Errorf("非 jpg 条目: %s", f.Name)
		}
		parts := strings.Split(f.Name, "/")
		if len(parts) == 2 {
			chapters[parts[0]]++
			pages++
		}
	}
	if pages == 0 {
		t.Fatal("zip 内没有页面文件")
	}
	t.Logf("zip 校验通过: %d 页 / %d 章,示例章节:", pages, len(chapters))
	for name, n := range chapters {
		if !strings.HasPrefix(name, "第") {
			t.Errorf("章节目录命名异常: %q", name)
		}
		t.Logf("  %s → %d 页", name, n)
	}
	if title == "" {
		t.Logf("zip 文件名: %s", filepath.Base(zipPath))
	}
}

// verifySandboxClean 校验临时沙箱已整体清理(需求:打包后清掉下载文件夹)。
func verifySandboxClean(t *testing.T, tempRoot, taskID string) {
	t.Helper()
	sandbox := filepath.Join(tempRoot, taskID)
	if _, err := os.Stat(sandbox); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(sandbox)
		t.Errorf("临时沙箱未清理: %s(剩余 %d 项)", sandbox, len(entries))
	}
}
