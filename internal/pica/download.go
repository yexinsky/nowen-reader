package pica

// PicACG 批量下载引擎(离线归档;管线与状态机按 internal/jm/download.go 同款适配)。
//
// 管线:详情 → 章节清单(eps,按 order 升序)→ 逐章分页拉图列表(order/{n}/pages)
// → 图片管线(候选 URL 下载,原样字节)→ 沙箱落盘 → 打包 zip → 归档到目标目录 →
// 清理沙箱 → 回调入库扫描(handler 注入)。
//
// 与 JM 的差异:
//   - 章节选择按 order(哔咔无 pid);图片列表按分页(每页约 40)聚合;
//   - 图片不做乱序还原/重编码,保留原始字节与扩展名(webp/jpg)。
//
// 安全边界(与 JM 同款):中间产物只写 <DataDir>/pica/download-tmp/<taskID> 自有沙箱,
// 结束一律整体删除;归档只在白名单目录新建文件,同名递增后缀,绝不覆盖既有文件。

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nowen-reader/nowen-reader/internal/namesafe"
)

/* ── 任务模型 ── */

// DownloadStatus 任务状态机:queued → running → packing → done|failed|canceled。
type DownloadStatus string

const (
	DownloadQueued   DownloadStatus = "queued"
	DownloadRunning  DownloadStatus = "running"
	DownloadPacking  DownloadStatus = "packing"
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
	Order int                  `json:"order"`
	Title string               `json:"title"`
	State DownloadChapterState `json:"state"`
	Total int                  `json:"total"`
	Done  int                  `json:"done"`
	Error string               `json:"error,omitempty"`
}

// DownloadTask 下载任务快照(JSON 契约,/api/pica/downloads 系列端点)。
type DownloadTask struct {
	ID          string            `json:"id"`
	BookID      string            `json:"bookId"`
	Title       string            `json:"title"`
	Author      string            `json:"author"`
	Tags        []string          `json:"tags,omitempty"`
	DestDir     string            `json:"destDir"`
	DestLabel   string            `json:"destLabel"`
	LibraryID   string            `json:"libraryId,omitempty"`
	Status      DownloadStatus    `json:"status"`
	Error       string            `json:"error,omitempty"`
	Warning     string            `json:"warning,omitempty"`
	Chapters    []DownloadChapter `json:"chapters"`
	token       string            // 上游 JWT(不序列化,run/resolveEpisodes 使用)
	TotalImages int               `json:"totalImages"`
	DoneImages  int               `json:"doneImages"`
	ZipName     string            `json:"zipName,omitempty"`
	ZipPath     string            `json:"zipPath,omitempty"`
	ZipSize     int64             `json:"zipSize,omitempty"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
}

// DownloadRequest 建任务入参(白名单目录由 handler 校验后传入)。
// Orders 空 = 全部章节;非空 = 只下这些 order(按升序)。
// Token 为哔咔上游 JWT(内容接口一律要求登录,任务异步执行期间持续使用;
// 快照 JSON 不含 token)。
type DownloadRequest struct {
	BookID    string
	Title     string
	Author    string
	Orders    []int
	Token     string
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

	slot chan struct{}

	mu      sync.Mutex
	tasks   map[string]*DownloadTask
	order   []string
	cancels map[string]context.CancelFunc

	onZip     func(*DownloadTask)
	tagFilter func([]string) []string
}

// NewDownloadManager 构造;tempRoot 为自有沙箱根。
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

// taskConcurrency 同时下载的漫画数(env PICA_DOWNLOAD_TASK_CONCURRENCY,默认 2)。
func taskConcurrency() int {
	if v := os.Getenv("PICA_DOWNLOAD_TASK_CONCURRENCY"); v != "" {
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

// SetTagFilter 注册标签过滤钩子:上游标签写入任务快照前经此裁剪。
func (m *DownloadManager) SetTagFilter(fn func([]string) []string) {
	m.mu.Lock()
	m.tagFilter = fn
	m.mu.Unlock()
}

func (m *DownloadManager) filterTags(tags []string) []string {
	m.mu.Lock()
	fn := m.tagFilter
	m.mu.Unlock()
	if fn == nil {
		return tags
	}
	return fn(tags)
}

// TempRoot 返回沙箱根目录(测试/诊断用)。
func (m *DownloadManager) TempRoot() string { return m.tempRoot }

// Start 新建下载任务(异步执行)。
func (m *DownloadManager) Start(req DownloadRequest) (*DownloadTask, error) {
	if strings.TrimSpace(req.BookID) == "" {
		return nil, errNotFound("bookId 必填")
	}
	now := nowStamp()
	t := &DownloadTask{
		ID:        newTaskID(),
		BookID:    req.BookID,
		Title:     req.Title,
		Author:    req.Author,
		DestDir:   req.DestDir,
		DestLabel: req.DestLabel,
		LibraryID: req.LibraryID,
		Status:    DownloadQueued,
		Chapters:  []DownloadChapter{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	t.token = req.Token

	m.mu.Lock()
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	m.evictLocked()
	m.mu.Unlock()

	go m.run(t, req.Orders)
	return m.Get(t.ID), nil
}

// Get 任务快照(深拷贝)。
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

// Cancel 取消进行中的任务(幂等)。
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

// Remove 移除任务记录(不动已归档文件;活动任务先取消)。
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

// evictLocked 超出保留上限时丢弃最旧的已结束任务。
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

func nowStamp() string { return time.Now().Format("2006-01-02T15:04:05") }

/* ── 任务执行 ── */

// pageWorkers 单章内并发抓图数(env PICA_DOWNLOAD_CONCURRENCY,默认 3)。
func pageWorkers() int {
	if v := os.Getenv("PICA_DOWNLOAD_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 8 {
			return n
		}
	}
	return 3
}

const (
	pageAttempts    = 3
	chapterAttempts = 2
	retryBackoff    = 800 * time.Millisecond
	imageTimeout    = 45 * time.Second
)

func (m *DownloadManager) run(t *DownloadTask, orders []int) {
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
	defer os.RemoveAll(tempDir)

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

	eps, err := m.resolveEpisodes(ctx, t, orders)
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
			tt.Chapters[i] = DownloadChapter{Order: e.Order, Title: e.Title, State: ChapterPending}
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
			failedChapters = append(failedChapters, strconv.Itoa(ep.Order))
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
			failedChapters = append(failedChapters, strconv.Itoa(ep.Order))
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
	// 一张图都没下成(totalImages=0 为章节清单解析全空,>0 为全部页失败)→ 归档必为空 zip,直接判失败
	if doneImages == 0 {
		m.finish(t.ID, DownloadFailed, "全部章节下载失败")
		return
	}

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
	zipBase := namesafe.SafeName(title, "Pica"+t.BookID)
	zipBase = namesafe.FitPathName(t.DestDir, zipBase, len(".zip")+8)
	finalPath := uniquePath(t.DestDir, zipBase, ".zip")
	if err := moveFile(zipLocal, finalPath); err != nil {
		m.finish(t.ID, DownloadFailed, "归档失败:"+err.Error())
		return
	}

	warn := ""
	if len(failedChapters) > 0 {
		warn = fmt.Sprintf("%d 个章节失败,已跳过:", len(failedChapters))
		for i, order := range failedChapters {
			if i >= 5 {
				warn += " 等"
				break
			}
			warn += " 第" + order + "话"
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

// downloadEpisode 章节清单项。
type downloadEpisode struct {
	Order int
	Title string
}

// resolveEpisodes 解析章节清单:详情补全标题/作者/标签,eps 第一页拿章节总数,
// 按需翻页聚合全部章节;orders 非空时按给定 order 过滤(升序)。
func (m *DownloadManager) resolveEpisodes(ctx context.Context, t *DownloadTask, orders []int) ([]downloadEpisode, error) {
	client, err := m.svc.APIClient()
	if err != nil {
		return nil, err
	}
	detailAny, err := client.ComicDetail(ctx, t.token, t.BookID)
	if err != nil && ctx.Err() == nil {
		return nil, fmt.Errorf("详情获取失败:%s", err.Error())
	}
	detail, _ := detailAny.(map[string]any)
	// 标签过滤在锁外做(filterTags 内部持 m.mu,锁内调用会自死锁)
	var detailTags []string
	var detailTitle, detailAuthor string
	if detail != nil {
		detailTitle, _ = detail["title"].(string)
		detailAuthor, _ = detail["author"].(string)
		if tags, ok := detail["tags"].([]string); ok {
			detailTags = m.filterTags(tags)
		}
	}
	m.mu.Lock()
	if tt, ok := m.tasks[t.ID]; ok {
		if tt.Title == "" {
			tt.Title = detailTitle
		}
		if tt.Author == "" {
			tt.Author = detailAuthor
		}
		tt.Tags = detailTags
	}
	m.mu.Unlock()

	// eps 分页聚合(每页约 40 章)
	var eps []downloadEpisode
	for page := 1; page <= 100; page++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		res, err := client.Eps(ctx, t.token, t.BookID, page)
		if err != nil {
			return nil, fmt.Errorf("章节清单获取失败:%s", err.Error())
		}
		resMap, _ := res.(map[string]any)
		list, _ := resMap["list"].([]any)
		hasNext, _ := resMap["hasNext"].(bool)
		for _, it := range list {
			mm, ok := it.(map[string]any)
			if !ok {
				continue
			}
			ord, _ := mm["order"].(int)
			if ord <= 0 {
				ord = len(eps) + 1
			}
			title, _ := mm["title"].(string)
			eps = append(eps, downloadEpisode{Order: ord, Title: title})
		}
		if !hasNext || len(list) == 0 {
			break
		}
	}
	if len(eps) == 0 {
		return nil, fmt.Errorf("没有可下载的章节")
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Order < eps[j].Order })

	if len(orders) > 0 {
		selected := make([]downloadEpisode, 0, len(orders))
		byOrder := make(map[int]downloadEpisode, len(eps))
		for _, e := range eps {
			byOrder[e.Order] = e
		}
		for _, o := range orders {
			if e, ok := byOrder[o]; ok {
				selected = append(selected, e)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("请求的章节不存在")
		}
		eps = selected
	}
	return eps, nil
}

// chapterDir 章节目录名:第NN话[_标题]。
func (m *DownloadManager) chapterDir(tempDir string, index, total int, ep downloadEpisode) string {
	width := 2
	if total >= 100 {
		width = 3
	}
	base := fmt.Sprintf("第%0*d话", width, index+1)
	name := base
	if title := strings.TrimSpace(ep.Title); title != "" {
		if clean := namesafe.SanitizeName(title); clean != "" && clean != base {
			name = namesafe.SafeName(base+"_"+clean, base)
		}
	}
	name = namesafe.FitPathName(tempDir, name, 0)
	return filepath.Join(tempDir, name)
}

// imgRef 图片引用(fileServer + 相对 path)。
type imgRef struct {
	fs   string
	path string
}

// fetchChapter 抓取单章全部页面:分页聚合图片清单,失败页补抓一轮。
func (m *DownloadManager) fetchChapter(ctx context.Context, taskID string, chIndex int, chapterDir string, ep downloadEpisode) error {
	client, err := m.svc.APIClient()
	if err != nil {
		return err
	}
	var images []imgRef
	for page := 1; page <= 1000; page++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res, err := client.EpsPages(ctx, m.taskToken(taskID), m.taskBookID(taskID), ep.Order, page)
		if err != nil {
			return fmt.Errorf("图片清单获取失败:%s", err.Error())
		}
		resMap, _ := res.(map[string]any)
		list, _ := resMap["list"].([]any)
		hasNext, _ := resMap["hasNext"].(bool)
		for _, it := range list {
			img, ok := it.(PageImage)
			if !ok || img.FileServer == "" || img.Path == "" {
				continue
			}
			images = append(images, imgRef{fs: img.FileServer, path: img.Path})
		}
		if !hasNext || len(list) == 0 {
			break
		}
	}
	if len(images) == 0 {
		return fmt.Errorf("该章节没有可下载的图片")
	}

	m.update(taskID, func(tt *DownloadTask) {
		tt.Chapters[chIndex].Total = len(images)
	})

	pending := make([]int, len(images))
	for i := range pending {
		pending[i] = i
	}
	var lastErr error
	for attempt := 0; attempt < chapterAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		failures := m.downloadPages(ctx, taskID, chIndex, chapterDir, images, pending)
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

// pageFailure 页面失败(index + 错误)。
type pageFailure struct {
	index int
	err   error
}

// downloadPages 并发抓取指定页(写入 chapterDir/NNNN.<ext>),返回失败页列表。
// 扩展名取自上游 path(哔咔无重编码,webp 保留原格式)。
func (m *DownloadManager) downloadPages(ctx context.Context, taskID string, chIndex int, chapterDir string, images []imgRef, indices []int) []pageFailure {
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
				ref := images[idx]
				data, err := m.fetchPage(ctx, ref.fs, ref.path)
				if err != nil {
					mu.Lock()
					failures = append(failures, pageFailure{index: idx, err: err})
					mu.Unlock()
					continue
				}
				dst := filepath.Join(chapterDir, fmt.Sprintf("%04d%s", idx+1, extOf(ref.path)))
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

// extOf 从上游 path 推断扩展名(默认 .jpg)。
func extOf(path string) string {
	lower := strings.ToLower(path)
	for _, ext := range []string{".webp", ".png", ".gif", ".jpeg", ".jpg"} {
		if strings.Contains(lower, ext) {
			return ext
		}
	}
	return ".jpg"
}

// fetchPage 单页抓取(重试 pageAttempts 次)。
func (m *DownloadManager) fetchPage(ctx context.Context, fileServer, path string) ([]byte, error) {
	httpClient, err := m.svc.ImageHTTPClient()
	if err != nil {
		return nil, err
	}
	cache := m.svc.ImageCache()
	var lastErr error
	for attempt := 0; attempt < pageAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pageCtx, cancel := context.WithTimeout(ctx, imageTimeout)
		data, _, _, err := FetchImage(pageCtx, httpClient.hc, cache, fileServer, path, "original")
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

// taskBookID 任务对应的书 ID。
func (m *DownloadManager) taskBookID(taskID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		return t.BookID
	}
	return ""
}

// taskToken 任务携带的上游 JWT。
func (m *DownloadManager) taskToken(taskID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		return t.token
	}
	return ""
}

/* ── 打包与归档(与 JM 同款) ── */

// packZip 将 srcDir 下全部文件打包为 zip,返回 zip 字节数。
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
			return nil
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

// moveFile 优先 rename;跨盘失败退化为拷贝后删除源文件。
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
