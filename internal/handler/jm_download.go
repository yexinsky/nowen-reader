package handler

// JM 批量下载端点(私有扩展,不属于 MOBILE_API.md 的 30 端点契约)。
//
//	GET    /api/jm/downloads/dirs         → 下载目录候选(书库管理目录 + 内置测试目录)
//	GET    /api/jm/downloads              → 任务列表(新→旧)
//	POST   /api/jm/downloads              → 新建下载任务(异步执行)
//	GET    /api/jm/downloads/:id          → 单任务进度
//	POST   /api/jm/downloads/:id/cancel   → 取消任务
//	DELETE /api/jm/downloads/:id          → 移除任务记录(不动已归档文件)
//
// 归档口径(见 internal/jm/download.go):逐章抓图 → 自有沙箱落盘 → 打包 zip →
// 写入目标目录(同名递增后缀,绝不覆盖既有文件)→ 清理沙箱 → 触发书库扫描入库。

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/config"
	"github.com/nowen-reader/nowen-reader/internal/jm"
	"github.com/nowen-reader/nowen-reader/internal/service"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// jmDownloadDir 下载目录候选(前端下拉框数据源)。
type jmDownloadDir struct {
	Label       string `json:"label"`
	Path        string `json:"path"`
	Kind        string `json:"kind"` // library | test
	LibraryID   string `json:"libraryId,omitempty"`
	LibraryType string `json:"libraryType,omitempty"`
	CanManage   bool   `json:"canManage"`
	IsDefault   bool   `json:"isDefault"`
	Exists      bool   `json:"exists"`
}

// jmDownloadTestDir 内置测试目录(不入库):<DataDir>/jm/download-test。
// 首次使用(尚未在设置里选定书库目录)时的默认落点,避免误写书库。
func jmDownloadTestDir() string {
	return filepath.Join(config.DataDir(), "jm", "download-test")
}

func jmDirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// jmDownloadDirCandidates 组装候选目录:启用的漫画/混合书库各根路径 + 内置测试目录。
// 默认项取自设置(命中候选才生效),否则回退测试目录。
func jmDownloadDirCandidates(uid string) []jmDownloadDir {
	out := make([]jmDownloadDir, 0, 8)
	libraries, err := store.GetAllLibraries()
	if err == nil {
		for _, lib := range libraries {
			if !lib.Enabled || lib.Type == "novel" {
				continue
			}
			canManage, _ := store.UserCanManageLibrary(uid, lib.ID)
			multi := len(lib.RootPaths) > 1
			for i, root := range lib.RootPaths {
				root = strings.TrimSpace(root)
				if root == "" {
					continue
				}
				label := lib.Name
				if multi {
					label = lib.Name + " (" + strconv.Itoa(i+1) + ")"
				}
				out = append(out, jmDownloadDir{
					Label:       label,
					Path:        filepath.Clean(root),
					Kind:        "library",
					LibraryID:   lib.ID,
					LibraryType: lib.Type,
					CanManage:   canManage,
					Exists:      jmDirExists(root),
				})
			}
		}
	}

	testDir := jmDownloadTestDir()
	out = append(out, jmDownloadDir{
		Label:     "测试目录(不入库)",
		Path:      testDir,
		Kind:      "test",
		CanManage: true,
		Exists:    jmDirExists(testDir),
	})

	// 默认项:设置命中候选优先,否则测试目录
	preferred := strings.TrimSpace(jmService().Settings().DownloadDir)
	defIdx := -1
	for i := range out {
		if preferred != "" && sameCleanPath(out[i].Path, preferred) {
			defIdx = i
			break
		}
	}
	if defIdx < 0 {
		for i := range out {
			if out[i].Kind == "test" {
				defIdx = i
				break
			}
		}
	}
	if defIdx >= 0 {
		out[defIdx].IsDefault = true
	}
	return out
}

func sameCleanPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	// Windows 盘符大小写不敏感
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

var (
	jmDownloadWireOnce sync.Once
	// jmDownloadMu 串行化 Start 时的目录校验与配置落盘
	jmDownloadMu sync.Mutex
)

// jmDownloads 取下载管理器并完成一次性接线(归档回调 → 书库扫描 → 自动标签)。
func jmDownloads() *jm.DownloadManager {
	dl := jmService().Downloads()
	jmDownloadWireOnce.Do(func() {
		dl.SetOnZip(func(t *jm.DownloadTask) {
			if t.LibraryID == "" {
				return // 测试目录/非书库目录:不入库、不打标
			}
			go func() {
				jmScanLibraryAfterDownload(t.LibraryID)
				jmApplyTagsWhenComicExists(t)
			}()
		})
	})
	return dl
}

// jmScanLibraryAfterDownload 归档后触发书库扫描(全局同一时刻只允许一个扫描,
// 冲突时退避重试,最多 3 次)。
func jmScanLibraryAfterDownload(libraryID string) {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(15 * time.Second)
		}
		if _, _, err := service.SyncLibraryByID(libraryID); err == nil {
			return
		} else if !strings.Contains(err.Error(), "already running") {
			log.Printf("[jm] 下载入库扫描失败(library=%s): %v", libraryID, err)
			return
		}
	}
	log.Printf("[jm] 下载入库扫描放弃(library=%s):扫描任务持续占用", libraryID)
}

// jmTagPollInterval / jmTagPollTimeout 入库打标签的存在性轮询节奏:
// 扫描通常数秒内完成;防抖兜底/重试耗尽时靠 fsnotify 补扫,轮询一并兜住。
const (
	jmTagPollInterval = 2 * time.Second
	jmTagPollTimeout  = 90 * time.Second
)

// jmAuthorPlaceholders 上游作者占位符(如 aid=1475046 实测 author=["N/A"])——
// 占位符不作为入库标签、不回填作者字段。
var jmAuthorPlaceholders = map[string]struct{}{
	"n/a": {}, "na": {}, "-": {}, "未知": {}, "未知作者": {}, "default_author": {}, "unknown": {},
}

// jmSyncAuthorName 作者名归一:trim、剔占位符(不区分大小写);无效返回空串。
func jmSyncAuthorName(author string) string {
	a := strings.TrimSpace(author)
	if a == "" {
		return ""
	}
	if _, ok := jmAuthorPlaceholders[strings.ToLower(a)]; ok {
		return ""
	}
	return a
}

// jmApplyTagsWhenComicExists 下载入库自动标签与作者同步(MOBILE_API.md §6):轮询等待
// 归档 zip 对应的 Comic 记录产生(PathToID 可确定性算出),然后:
// ① 挂 JM 标签(任务快照 tags,不存在自动创建);② 作者同步:有效作者名(非占位符)
// 并入标签清单(书库详情页标签区可点可筛选),并把 Comic.author 元数据回填(仅当为空,
// 不覆盖刮削/手动结果)。只记日志、绝不向任务注错——下载本体已成功,打标是附加增强。
func jmApplyTagsWhenComicExists(t *jm.DownloadTask) {
	if t.LibraryID == "" || t.ZipName == "" {
		return
	}
	// 标签清单 = 快照 tags + 有效作者(去重;占位符作者如 "N/A" 不参与)
	tags := make([]string, 0, len(t.Tags)+1)
	seen := map[string]struct{}{}
	for _, tag := range t.Tags {
		if tag == "" {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	author := jmSyncAuthorName(t.Author)
	if author != "" {
		if _, dup := seen[author]; !dup {
			tags = append(tags, author)
		}
	}
	if len(tags) == 0 {
		return
	}
	if !jmService().Settings().DownloadTags {
		return
	}
	comicID := store.PathToID(t.LibraryID, t.ZipName)
	deadline := time.Now().Add(jmTagPollTimeout)
	for {
		exists, err := store.ComicRelativePathExists(t.LibraryID, t.ZipName, "")
		if err == nil && exists {
			if err := store.AddTagsToComic(comicID, tags); err != nil {
				log.Printf("[jm] 自动标签写入失败(comic=%s): %v", comicID, err)
			} else {
				log.Printf("[jm] 已自动添加 %d 个标签(comic=%s, aid=%s)", len(tags), comicID, t.Aid)
			}
			// 作者元数据回填:仅当书库记录的 author 为空,不覆盖刮削/手动结果
			if author != "" {
				if item, err := store.GetComicByID(comicID); err == nil && item != nil &&
					strings.TrimSpace(item.Author) == "" {
					if err := store.UpdateComicFields(comicID, map[string]interface{}{"author": author}); err != nil {
						log.Printf("[jm] 作者字段回填失败(comic=%s): %v", comicID, err)
					}
				}
			}
			return
		}
		if err != nil {
			log.Printf("[jm] 自动标签:查询入库状态失败(comic=%s): %v", comicID, err)
			return
		}
		if time.Now().After(deadline) {
			log.Printf("[jm] 自动标签放弃(comic=%s):等待入库超时(%s),可手动补标", comicID, jmTagPollTimeout)
			return
		}
		time.Sleep(jmTagPollInterval)
	}
}

func registerJMDownloadRoutes(g *gin.RouterGroup) {
	// 静态段先注册(与 /:id 同层级共存,gin 允许静态优先匹配)
	g.GET("/downloads/dirs", func(c *gin.Context) {
		uid := getUserID(c)
		jmOK(c, gin.H{
			"dirs":    jmDownloadDirCandidates(uid),
			"testDir": jmDownloadTestDir(),
		})
	})

	g.GET("/downloads", func(c *gin.Context) {
		jmOK(c, gin.H{"list": jmDownloads().List(), "tempRoot": jmDownloads().TempRoot()})
	})

	g.POST("/downloads", func(c *gin.Context) {
		var body struct {
			Aid     string   `json:"aid"`
			Title   string   `json:"title"`
			Author  string   `json:"author"`
			Pids    []string `json:"pids"`
			DestDir string   `json:"destDir"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			jmContent422(c, err.Error())
			return
		}
		if strings.TrimSpace(body.Aid) == "" && len(body.Pids) == 0 {
			jmContent422(c, "aid 与 pids 至少提供其一")
			return
		}
		if strings.TrimSpace(body.DestDir) == "" {
			jmContent422(c, "destDir 必填")
			return
		}

		// 目标目录白名单校验:只允许书库管理中的目录(需管理权限)或内置测试目录
		uid := getUserID(c)
		var target *jmDownloadDir
		for _, dir := range jmDownloadDirCandidates(uid) {
			if sameCleanPath(dir.Path, body.DestDir) {
				d := dir
				target = &d
				break
			}
		}
		if target == nil {
			jmContent422(c, "下载目录不在候选列表中(请在书库管理中添加)")
			return
		}
		if !target.CanManage {
			c.JSON(403, gin.H{"detail": "Forbidden: 无该书库的管理权限"})
			return
		}

		jmDownloadMu.Lock()
		task, err := jmDownloads().Start(jm.DownloadRequest{
			Aid:       body.Aid,
			Title:     body.Title,
			Author:    body.Author,
			Pids:      body.Pids,
			DestDir:   target.Path,
			DestLabel: target.Label,
			LibraryID: target.LibraryID,
		})
		// 记住本次选择(设置读写失败不影响任务)
		if err == nil {
			st := jmService().Settings()
			if !sameCleanPath(st.DownloadDir, target.Path) {
				st.DownloadDir = target.Path
				_ = jmService().SaveSettings(st)
			}
		}
		jmDownloadMu.Unlock()

		if err != nil {
			jmFailErr(c, err, "创建下载任务失败")
			return
		}
		jmOK(c, task)
	})

	g.GET("/downloads/:id", func(c *gin.Context) {
		task := jmDownloads().Get(c.Param("id"))
		if task == nil {
			jmFail(c, jm.CodeNotFound, "任务不存在", nil)
			return
		}
		jmOK(c, task)
	})

	g.POST("/downloads/:id/cancel", func(c *gin.Context) {
		if !jmDownloads().Cancel(c.Param("id")) {
			jmFail(c, jm.CodeNotFound, "任务不存在或已结束", nil)
			return
		}
		jmOK(c, gin.H{"ok": true})
	})

	g.DELETE("/downloads/:id", func(c *gin.Context) {
		if !jmDownloads().Remove(c.Param("id")) {
			jmFail(c, jm.CodeNotFound, "任务不存在", nil)
			return
		}
		jmOK(c, gin.H{"ok": true})
	})
}
