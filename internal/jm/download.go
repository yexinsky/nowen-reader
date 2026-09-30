package jm

// JM 批量下载引擎(离线归档)。
//
// 管线:详情/章节清单 → 逐章抓图(复用 /api/image 同款管线:域名池下载 → 乱序还原
// → 统一 JPEG)→ 临时目录落盘 → 打包 zip → 归档到目标目录 → 清理临时目录 →
// 回调入库扫描(handler 注入)。
//
// 命名口径移植 JMComic-qt(ToolUtil.GetCanSaveName):
// 删 Windows 非法字符 → 去尾点 → 去首尾空格 → 截断 254//3-1=83 字符 → 再去尾点/空格;
// 章节目录为「第NN话[_标题]」,页面文件为 NNNN.jpg(4 位零填充,阅读顺序)。
//
// 安全边界:所有中间产物只写在 <DataDir>/jm/download-tmp/<taskID> 自有沙箱内,
// 结束(成功/失败/取消)一律整体删除;归档只在调用方给定的白名单目录内新建文件,
// 同名时递增后缀,绝不覆盖或删除目标目录中的既有文件。

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

/* ── 名称清洗(JMComic-qt ToolUtil.GetCanSaveName 逐句移植) ── */

// maxNameRunes 单级名称字符数上限:254//3-1 = 83。
// 依据为 UTF-8 下 CJK 单字 3 字节,83×3=249 ≤ 255 的文件名字节上限。
const maxNameRunes = 254/3 - 1

// maxFullPathLen 全路径字节预算:Windows MAX_PATH=260,预留扩展名/同名序号余量。
const maxFullPathLen = 240

// illegalNameChars Windows 非法字符 + \0\t\r\n(JMComic-qt 同款字符类)。
// 目录分隔符一并删除,保证清洗结果恒为单级名称,不会逃出目标目录。
var illegalNameChars = regexp.MustCompile("[\\\\/:*?\"<>|\x00\t\r\n]")

// SanitizeName 清洗单级路径名(移植 JMComic-qt ToolUtil.GetCanSaveName):
// 删非法字符 → 去尾点 → 去首尾空格 → 截断 83 字符 → 再去尾点/首尾空格。
func SanitizeName(name string) string {
	s := illegalNameChars.ReplaceAllString(name, "")
	s = strings.TrimRight(s, ".")
	s = strings.Trim(s, " ")
	if r := []rune(s); len(r) > maxNameRunes {
		s = string(r[:maxNameRunes])
	}
	return strings.Trim(strings.TrimRight(s, "."), " ")
}

// SafeName 清洗并保证非空:清洗结果为空时回退 fallback(通常为 aid/pid),
// 仍为空回退 "untitled"。JMComic-qt 无此兜底,空名会直接导致写盘失败。
func SafeName(name, fallback string) string {
	if s := SanitizeName(name); s != "" {
		return s
	}
	if s := SanitizeName(fallback); s != "" {
		return s
	}
	return "untitled"
}

// fitPathName 按目标目录收紧名称:保证 dir/name 的全路径字节数不超过
// maxFullPathLen(Windows MAX_PATH 兜底;CJK 按 UTF-8 字节累计,估算偏保守)。
func fitPathName(dir, name string, reserveBytes int) string {
	avail := maxFullPathLen - len(dir) - 1 - reserveBytes
	if avail < 16 {
		avail = 16 // 目录本身过长时保底可读性,写盘失败会以「归档失败」显式上报
	}
	if len(name) <= avail {
		return name
	}
	var b strings.Builder
	used := 0
	for _, r := range name {
		size := utf8.RuneLen(r)
		if used+size > avail {
			break
		}
		b.WriteRune(r)
		used += size
	}
	return SafeName(b.String(), "untitled")
}

/* ── 任务模型 ── */

// DownloadStatus 任务状态机:queued → running → packing → done|failed|canceled。
type DownloadStatus string

const (
	DownloadQueued   DownloadStatus = "queued"
	DownloadRunning  DownloadStatus = "running"
	DownloadPacking  DownloadStatus = "packing" // 抓图完成,正在打包/归档
	DownloadDone     DownloadStatus = "done"
	DownloadFailed   DownloadStatus = "failed"
	DownloadCanceled DownloadStatus = "canceled"
)

// DownloadChapterState 章节状态。
type DownloadChapterState string

const (
	ChapterPending DownloadChapterState = "pending"
	ChapterRunning DownloadChapterState = "running"
	ChapterDone    DownloadChapterState = "done"
	ChapterFailed  DownloadChapterState = "failed"
)

// DownloadChapter 章节进度(前端逐章展示)。
type DownloadChapter struct {
	Pid   string               `json:"pid"`
	Title string               `json:"title"`
	Order int                  `json:"order"`
	State DownloadChapterState `json:"state"`
	Total int                  `json:"total"`
	Done  int                  `json:"done"`
	Error string               `json:"error,omitempty"`
}

// DownloadTask 下载任务快照(JSON 契约,/api/jm/downloads 系列端点)。
type DownloadTask struct {
	ID          string            `json:"id"`
	Aid         string            `json:"aid"`
	Title       string            `json:"title"`
	Author      string            `json:"author"`
	DestDir     string            `json:"destDir"`
	DestLabel   string            `json:"destLabel"`
	LibraryID   string            `json:"libraryId,omitempty"`
	Status      DownloadStatus    `json:"status"`
	Error       string            `json:"error,omitempty"`
	Warning     string            `json:"warning,omitempty"`
	Chapters    []DownloadChapter `json:"chapters"`
	TotalImages int               `json:"totalImages"`
	DoneImages  int               `json:"doneImages"`
	ZipName     string            `json:"zipName,omitempty"`
	ZipPath     string            `json:"zipPath,omitempty"`
	ZipSize     int64             `json:"zipSize,omitempty"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
}

// DownloadRequest 建任务入参(白名单目录由 handler 校验后传入)。
type DownloadRequest struct {
	Aid       string
	Title     string
	Author    string
	Pids      []string // 空 = 全部章节;非空 = 只下这些章节(按给定顺序)
	DestDir   string
	DestLabel string
	LibraryID string
}

/* ── 任务管理 ── */

// DownloadManager 内存任务表:进程内单例,保留最近 maxTasks 条(含已结束)。
type DownloadManager struct {
	svc      *Service
	tempRoot string
	maxTasks int

	// slot 任务级并发信号量:批量下载(一次建多个任务)时排队执行,
	// 保证同时对上游的漫画数有上限(LRU 之外的第一道限流)。
	slot chan struct{}

	mu      sync.Mutex
	tasks   map[string]*DownloadTask
	order   []string
	cancels map[string]context.CancelFunc

	onZip func(*DownloadTask) // 归档完成回调(handler 注入:触发书库扫描)
}

// NewDownloadManager 构造;tempRoot 为自有沙箱根(<DataDir>/jm/download-tmp)。
func NewDownloadManager(svc *Service, tempRoot string) *DownloadManager {
	return &DownloadManager{
		svc:      svc,
		tempRoot: tempRoot,
		maxTasks: 60,
		slot:     make(chan struct{}, taskConcurrency()),
		tasks:    make(map[string]*DownloadTask),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// taskConcurrency 同时下载的漫画数(env JM_DOWNLOAD_TASK_CONCURRENCY,默认 2)。
func taskConcurrency() int {
	if v := os.Getenv("JM_DOWNLOAD_TASK_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 8 {
			return n
		}
	}
	return 2
}

// SetOnZip 注册归档完成回调(在任务 goroutine 内同步调用,勿阻塞)。
func (m *DownloadManager) SetOnZip(fn func(*DownloadTask)) {
	m.mu.Lock()
	m.onZip = fn
	m.mu.Unlock()
}

// TempRoot 返回沙箱根目录(测试/诊断用)。
func (m *DownloadManager) TempRoot() string { return m.tempRoot }

// Start 建任务并异步执行;返回任务快照。
func (m *DownloadManager) Start(req DownloadRequest) (*DownloadTask, error) {
	req.Aid = strings.TrimSpace(req.Aid)
	req.DestDir = strings.TrimSpace(req.DestDir)
	if req.Aid == "" && len(req.Pids) == 0 {
		return nil, fmt.Errorf("aid 与 pids 至少提供其一")
	}
	if req.DestDir == "" {
		return nil, fmt.Errorf("下载目录必填")
	}
	if !filepath.IsAbs(req.DestDir) {
		return nil, fmt.Errorf("下载目录必须是绝对路径")
	}

	now := nowStamp()
	t := &DownloadTask{
		ID:        newTaskID(),
		Aid:       req.Aid,
		Title:     strings.TrimSpace(req.Title),
		Author:    strings.TrimSpace(req.Author),
		DestDir:   filepath.Clean(req.DestDir),
		DestLabel: req.DestLabel,
		LibraryID: req.LibraryID,
		Status:    DownloadQueued,
		Chapters:  []DownloadChapter{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.mu.Lock()
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	m.evictLocked()
	m.mu.Unlock()

	go m.run(t, req.Pids)
	return m.Get(t.ID), nil
}

// Get 任务快照(深拷贝,避免与 worker 并发读写)。
func (m *DownloadManager) Get(id string) *DownloadTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		return cloneTask(t)
	}
	return nil
}

// List 任务快照,新→旧。
func (m *DownloadManager) List() []*DownloadTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*DownloadTask, 0, len(m.order))
	for i := len(m.order) - 1; i >= 0; i-- {
		if t, ok := m.tasks[m.order[i]]; ok {
			out = append(out, cloneTask(t))
		}
	}
	return out
}

// Cancel 取消进行中的任务(幂等);返回是否命中活动任务。
func (m *DownloadManager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cancel, ok := m.cancels[id]
	if !ok {
		return false
	}
	cancel()
	return true
}

// Remove 移除任务记录(仅清列表,不动已归档文件;活动任务先取消)。
func (m *DownloadManager) Remove(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[id]; !ok {
		return false
	}
	if cancel, ok := m.cancels[id]; ok {
		cancel()
		delete(m.cancels, id)
	}
	delete(m.tasks, id)
	kept := m.order[:0]
	for _, tid := range m.order {
		if tid != id {
			kept = append(kept, tid)
		}
	}
	m.order = kept
	return true
}

// evictLocked 超出保留上限时丢弃最旧的已结束任务(活动任务不丢)。
func (m *DownloadManager) evictLocked() {
	for len(m.order) > m.maxTasks {
		idx := -1
		for i, id := range m.order {
			if t, ok := m.tasks[id]; ok && !isActive(t.Status) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return
		}
		delete(m.tasks, m.order[idx])
		m.order = append(m.order[:idx], m.order[idx+1:]...)
	}
}

func isActive(s DownloadStatus) bool {
	switch s {
	case DownloadQueued, DownloadRunning, DownloadPacking:
		return true
	}
	return false
}

func newTaskID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func cloneTask(t *DownloadTask) *DownloadTask {
	cp := *t
	cp.Chapters = make([]DownloadChapter, len(t.Chapters))
	copy(cp.Chapters, t.Chapters)
	return &cp
}

/* ── 任务执行 ── */

// downloadEpisode 章节清单项(pid/title/order)。
type downloadEpisode struct {
	Pid   string
	Title string
	Order int
}

// pageWorkers 单章内并发抓图数:JMComic-qt 为「5 章并发 × 章内串行」,
// 服务端改为「章串行 × 章内 3 并发」,并发上限更保守且总吞吐相近。
func pageWorkers() int {
	if v := os.Getenv("JM_DOWNLOAD_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 8 {
			return n
		}
	}
	return 3
}

const (
	pageAttempts    = 3               // 单页网络尝试次数
	chapterAttempts = 2               // 整章补抓轮次(失败页再抓一轮)
	retryBackoff    = 800 * time.Millisecond
	imageTimeout    = 45 * time.Second // 单页整体超时(下载+解码+还原)
)

func (m *DownloadManager) run(t *DownloadTask, pids []string) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[t.ID] = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, t.ID)
		m.mu.Unlock()
	}()

	tempDir := filepath.Join(m.tempRoot, t.ID)
	// 无论成功/失败/取消,自有沙箱整体清理(需求:打包后清掉下载文件夹)
	defer os.RemoveAll(tempDir)

	// 任务级排队:批量下载时按到达顺序依次执行,取消可直接跳出
	select {
	case m.slot <- struct{}{}:
		defer func() { <-m.slot }()
	case <-ctx.Done():
		m.finish(t.ID, DownloadCanceled, "")
		return
	}

	m.update(t.ID, func(tt *DownloadTask) {
		tt.Status = DownloadRunning
		tt.UpdatedAt = nowStamp()
	})

	eps, err := m.resolveEpisodes(ctx, t, pids)
	if err != nil {
		m.finish(t.ID, DownloadFailed, err.Error())
		return
	}
	if ctx.Err() != nil {
		m.finish(t.ID, DownloadCanceled, "")
		return
	}

	m.update(t.ID, func(tt *DownloadTask) {
		tt.Chapters = make([]DownloadChapter, len(eps))
		for i, e := range eps {
			tt.Chapters[i] = DownloadChapter{Pid: e.Pid, Title: e.Title, Order: e.Order, State: ChapterPending}
		}
	})

	failedChapters := make([]string, 0)
	for i := range eps {
		if ctx.Err() != nil {
			m.finish(t.ID, DownloadCanceled, "")
			return
		}
		ep := eps[i]
		chapterDir := m.chapterDir(tempDir, i, len(eps), ep)
		if err := os.MkdirAll(chapterDir, 0o755); err != nil {
			m.update(t.ID, func(tt *DownloadTask) {
				tt.Chapters[i].State = ChapterFailed
				tt.Chapters[i].Error = err.Error()
			})
			failedChapters = append(failedChapters, ep.Pid)
			continue
		}

		m.update(t.ID, func(tt *DownloadTask) {
			tt.Chapters[i].State = ChapterRunning
			tt.UpdatedAt = nowStamp()
		})

		if err := m.fetchChapter(ctx, t.ID, i, chapterDir, ep); err != nil {
			msg := err.Error()
			m.update(t.ID, func(tt *DownloadTask) {
				tt.Chapters[i].State = ChapterFailed
				tt.Chapters[i].Error = msg
			})
			failedChapters = append(failedChapters, ep.Pid)
			continue
		}
		m.update(t.ID, func(tt *DownloadTask) {
			tt.Chapters[i].State = ChapterDone
			tt.Chapters[i].Done = tt.Chapters[i].Total
			tt.UpdatedAt = nowStamp()
		})
	}

	if ctx.Err() != nil {
		m.finish(t.ID, DownloadCanceled, "")
		return
	}

	totalImages, doneImages := 0, 0
	m.mu.Lock()
	if tt, ok := m.tasks[t.ID]; ok {
		for _, ch := range tt.Chapters {
			totalImages += ch.Total
			if ch.State == ChapterDone {
				doneImages += ch.Total
			}
		}
	}
	m.mu.Unlock()
	if totalImages > 0 && doneImages == 0 {
		m.finish(t.ID, DownloadFailed, "全部章节下载失败")
		return
	}

	// 打包 + 归档
	m.update(t.ID, func(tt *DownloadTask) {
		tt.Status = DownloadPacking
		tt.UpdatedAt = nowStamp()
	})
	zipLocal := filepath.Join(tempDir, "pack.zip")
	size, err := packZip(ctx, tempDir, zipLocal)
	if err != nil {
		m.finish(t.ID, DownloadFailed, "打包失败:"+err.Error())
		return
	}
	if err := os.MkdirAll(t.DestDir, 0o755); err != nil {
		m.finish(t.ID, DownloadFailed, "下载目录不可写:"+err.Error())
		return
	}

	m.mu.Lock()
	title := t.Title
	m.mu.Unlock()
	// zip 名 = 漫画标题(默认命名口径,与 JMComic-qt SaveNameType.Default 一致)
	zipBase := SafeName(title, "JM"+t.Aid)
	zipBase = fitPathName(t.DestDir, zipBase, len(".zip")+8)
	finalPath := uniquePath(t.DestDir, zipBase, ".zip")
	if err := moveFile(zipLocal, finalPath); err != nil {
		m.finish(t.ID, DownloadFailed, "归档失败:"+err.Error())
		return
	}

	warn := ""
	if len(failedChapters) > 0 {
		warn = fmt.Sprintf("%d 个章节失败,已跳过:", len(failedChapters))
		for i, pid := range failedChapters {
			if i >= 5 {
				warn += " 等"
				break
			}
			warn += " " + pid
		}
	}
	m.mu.Lock()
	if tt, ok := m.tasks[t.ID]; ok {
		tt.ZipName = filepath.Base(finalPath)
		tt.ZipPath = finalPath
		tt.ZipSize = size
		tt.Warning = warn
		tt.TotalImages = totalImages
		tt.DoneImages = doneImages
	}
	m.mu.Unlock()

	m.update(t.ID, func(tt *DownloadTask) {
		tt.Status = DownloadDone
		tt.UpdatedAt = nowStamp()
	})

	m.mu.Lock()
	cb := m.onZip
	var snap *DownloadTask
	if tt, ok := m.tasks[t.ID]; ok {
		snap = cloneTask(tt)
	}
	m.mu.Unlock()
	if cb != nil && snap != nil {
		cb(snap)
	}
}

// update 在锁内修改任务并刷新更新时间。
func (m *DownloadManager) update(id string, fn func(*DownloadTask)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		fn(t)
		t.UpdatedAt = nowStamp()
	}
}

// finish 置终态(仅当任务仍在活动态;取消优先)。
func (m *DownloadManager) finish(id string, status DownloadStatus, errMsg string) {
	m.update(id, func(tt *DownloadTask) {
		if tt.Status == DownloadCanceled && status != DownloadCanceled {
			return
		}
		tt.Status = status
		if errMsg != "" {
			tt.Error = errMsg
		}
	})
}

// resolveEpisodes 解析章节清单:有 aid 先取详情(拿到标题/作者/章节名),
// 再按请求 pids 过滤(保持请求顺序);详情失败且无 pids 视为失败。
func (m *DownloadManager) resolveEpisodes(ctx context.Context, t *DownloadTask, pids []string) ([]downloadEpisode, error) {
	var eps []downloadEpisode
	if t.Aid != "" {
		if data, err := m.svc.AnonClient().ComicDetail(ctx, t.Aid); err == nil {
			m.mu.Lock()
			if tt, ok := m.tasks[t.ID]; ok {
				if tt.Title == "" {
					tt.Title = fieldStr(dataMap(data), "title")
				}
				if tt.Author == "" {
					tt.Author = fieldStr(dataMap(data), "author")
				}
			}
			m.mu.Unlock()
			for _, it := range listOf(dataMap(data)["episodes"]) {
				mm, ok := it.(map[string]any)
				if !ok {
					continue
				}
				pid := fieldStr(mm, "pid")
				if pid == "" {
					continue
				}
				ord := parseCount(mm["order"])
				if ord <= 0 {
					ord = len(eps) + 1
				}
				eps = append(eps, downloadEpisode{Pid: pid, Title: fieldStr(mm, "title"), Order: ord})
			}
			sort.SliceStable(eps, func(i, j int) bool { return eps[i].Order < eps[j].Order })
		} else if len(pids) == 0 && ctx.Err() == nil {
			return nil, fmt.Errorf("章节清单获取失败:%s", err.Error())
		}
	}

	if len(pids) > 0 {
		byPid := make(map[string]downloadEpisode, len(eps))
		for _, e := range eps {
			byPid[e.Pid] = e
		}
		selected := make([]downloadEpisode, 0, len(pids))
		for i, pid := range pids {
			pid = strings.TrimSpace(pid)
			if pid == "" {
				continue
			}
			if e, ok := byPid[pid]; ok {
				selected = append(selected, e)
				continue
			}
			selected = append(selected, downloadEpisode{Pid: pid, Order: i + 1})
		}
		eps = selected
	}

	if len(eps) == 0 {
		if t.Aid != "" {
			// 单章本子:详情无 series 时以 aid 作为唯一章节
			eps = []downloadEpisode{{Pid: t.Aid, Order: 1}}
		} else {
			return nil, fmt.Errorf("没有可下载的章节")
		}
	}
	return eps, nil
}

// dataMap 宽松取 map(上游数据形态多变,失败回退空 map)。
func dataMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// chapterDir 章节目录名:第NN话[_标题](NN 按总章数补零,便于自然排序)。
func (m *DownloadManager) chapterDir(tempDir string, index, total int, ep downloadEpisode) string {
	width := 2
	if total >= 100 {
		width = 3
	}
	base := fmt.Sprintf("第%0*d话", width, index+1)
	name := base
	// 上游章节标题缺失/等于 pid(单章本子)时不追加后缀,避免出现「第01话_」这类尾缀
	if title := strings.TrimSpace(ep.Title); title != "" {
		if clean := SanitizeName(title); clean != "" && clean != base && clean != ep.Pid {
			name = SafeName(base+"_"+clean, base)
		}
	}
	// 全路径预算按父目录计算(tempDir 本身可能很长,不可把 name 计入 dir)
	name = fitPathName(tempDir, name, 0)
	return filepath.Join(tempDir, name)
}

// fetchChapter 抓取单章全部页面:失败页补抓一轮,仍失败则该章判定失败。
func (m *DownloadManager) fetchChapter(ctx context.Context, taskID string, chIndex int, chapterDir string, ep downloadEpisode) error {
	client := m.svc.AnonClient()
	photosAny, err := client.PhotoDetail(ctx, ep.Pid)
	if err != nil {
		return fmt.Errorf("章节信息获取失败:%s", err.Error())
	}
	photos := dataMap(photosAny)
	scramble := fieldStr(photos, "scramble")
	if scramble == "" {
		scramble = "0"
	}
	aid := fieldStr(photos, "aid")
	if aid == "" {
		aid = ep.Pid
	}
	paths := make([]string, 0, len(listOf(photos["images"])))
	for _, it := range listOf(photos["images"]) {
		if mm, ok := it.(map[string]any); ok {
			if p := fieldStr(mm, "path"); p != "" {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("该章节没有可下载的图片")
	}

	m.update(taskID, func(tt *DownloadTask) {
		tt.Chapters[chIndex].Total = len(paths)
	})

	pending := make([]int, len(paths))
	for i := range pending {
		pending[i] = i
	}
	var lastErr error
	for attempt := 0; attempt < chapterAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		failures := m.downloadPages(ctx, taskID, chIndex, chapterDir, paths, pending, scramble, aid)
		if len(failures) == 0 {
			return nil
		}
		lastErr = failures[len(failures)-1].err
		pending = pending[:0]
		for _, f := range failures {
			pending = append(pending, f.index)
		}
		if attempt+1 < chapterAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBackoff):
			}
		}
	}
	return fmt.Errorf("%d 页下载失败:%s", len(pending), lastErr)
}

// pageFailure 页面失败(index + 错误),用于补抓轮次。
type pageFailure struct {
	index int
	err   error
}

// downloadPages 并发抓取指定页(写入 chapterDir/NNNN.jpg),返回失败页列表。
func (m *DownloadManager) downloadPages(ctx context.Context, taskID string, chIndex int, chapterDir string, paths []string, indices []int, scramble, aid string) []pageFailure {
	workers := pageWorkers()
	if workers > len(indices) {
		workers = len(indices)
	}
	if workers < 1 {
		return nil
	}

	jobs := make(chan int)
	var mu sync.Mutex
	var failures []pageFailure
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx.Err() != nil {
					mu.Lock()
					failures = append(failures, pageFailure{index: idx, err: ctx.Err()})
					mu.Unlock()
					continue
				}
				data, err := m.fetchPage(ctx, paths[idx], scramble, aid)
				if err != nil {
					mu.Lock()
					failures = append(failures, pageFailure{index: idx, err: err})
					mu.Unlock()
					continue
				}
				dst := filepath.Join(chapterDir, fmt.Sprintf("%04d.jpg", idx+1))
				if err := os.WriteFile(dst, data, 0o644); err != nil {
					mu.Lock()
					failures = append(failures, pageFailure{index: idx, err: err})
					mu.Unlock()
					continue
				}
				m.update(taskID, func(tt *DownloadTask) {
					tt.Chapters[chIndex].Done++
					tt.DoneImages++
				})
			}
		}()
	}
	feed:
	for _, idx := range indices {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	return failures
}

// fetchPage 单页抓取(重试 pageAttempts 次,单次 45s 超时)。
func (m *DownloadManager) fetchPage(ctx context.Context, imagePath, scramble, aid string) ([]byte, error) {
	quality := m.svc.Settings().ImageQuality
	httpClient := m.svc.ImageHTTPClient()
	cache := m.svc.ImageCache()

	var lastErr error
	for attempt := 0; attempt < pageAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pageCtx, cancel := context.WithTimeout(ctx, imageTimeout)
		data, _, err := DownloadAndDecode(pageCtx, httpClient, cache, imagePath, scramble, aid, quality)
		cancel()
		if err == nil && len(data) > 0 {
			return data, nil
		}
		if err == nil {
			err = fmt.Errorf("图片内容为空")
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryBackoff):
		}
	}
	return nil, lastErr
}

/* ── 打包与归档 ── */

// packZip 将 srcDir 下全部文件打包为 zip(条目为相对路径,正斜杠分隔),
// 返回 zip 字节数。压缩口径 ZIP_DEFLATED,与 JMComic-qt 打包一致。
func packZip(ctx context.Context, srcDir, zipPath string) (int64, error) {
	f, err := os.Create(zipPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	walkErr := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		if rel == filepath.Base(zipPath) {
			return nil // 跳过 zip 自身
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, src)
		closeErr := src.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if walkErr != nil {
		zw.Close()
		return 0, walkErr
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	stat, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

// uniquePath 目标目录内不重名路径:同名追加 " (2)"…" (99)",极端情况退化为时间戳。
func uniquePath(dir, base, ext string) string {
	candidate := filepath.Join(dir, base+ext)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 2; i <= 99; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, time.Now().Unix(), ext))
}

// moveFile 优先 rename(同盘瞬时);跨盘失败时退化为拷贝后删除源文件。
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}
