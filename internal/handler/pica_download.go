package handler

// PicACG 批量下载端点(私有扩展)。
//
//	GET    /api/pica/downloads/dirs         → 下载目录候选(书库管理目录 + 内置测试目录)
//	GET    /api/pica/downloads              → 任务列表(新→旧)
//	POST   /api/pica/downloads              → 新建下载任务(异步执行)
//	GET    /api/pica/downloads/:id          → 单任务进度
//	POST   /api/pica/downloads/:id/cancel   → 取消任务
//	DELETE /api/pica/downloads/:id          → 移除任务记录(不动已归档文件)
//
// 归档口径(见 internal/pica/download.go):逐章拉图 → 沙箱落盘 → 打包 zip →
// 写入目标目录(同名递增后缀)→ 清理沙箱 → 触发书库扫描入库 + 自动标签。

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/pica"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

var (
	picaDownloadWireOnce sync.Once
	// picaDownloadMu 串行化 Start 时的目录校验与配置落盘
	picaDownloadMu sync.Mutex
)

// picaDownloads 取下载管理器并完成一次性接线(归档回调 → 书库扫描 → 自动标签)。
func picaDownloads() *pica.DownloadManager {
	dl := picaService().Downloads()
	picaDownloadWireOnce.Do(func() {
		dl.SetTagFilter(func(tags []string) []string {
			kept, dropped := jmFilterTagsByBlocklist(tags)
			jmLogDroppedTags("pica下载快照", dropped)
			return kept
		})
		dl.SetOnZip(func(t *pica.DownloadTask) {
			if t.LibraryID == "" {
				return // 测试目录/非书库目录:不入库、不打标
			}
			go func() {
				jmScanLibraryAfterDownload(t.LibraryID)
				picaApplyTagsWhenComicExists(t)
			}()
		})
	})
	return dl
}

// picaApplyTagsWhenComicExists 下载入库自动标签与作者同步(口径同 JM 版):
// 轮询等待归档 zip 对应的 Comic 记录产生,然后挂内容标签 + 作者独立标签 + 作者回填。
// 只记日志、绝不向任务注错。
func picaApplyTagsWhenComicExists(t *pica.DownloadTask) {
	if t.LibraryID == "" || t.ZipName == "" {
		return
	}
	tags := make([]string, 0, len(t.Tags))
	seen := map[string]struct{}{}
	for _, tag := range t.Tags {
		if tag == "" {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		tags = append(tags, tag)
	}
	if kept, dropped := jmFilterTagsByBlocklist(tags); len(dropped) > 0 {
		jmLogDroppedTags("pica自动标签", dropped)
		tags = kept
	}
	author := jmSyncAuthorName(t.Author)
	if author == "" && len(tags) == 0 {
		return
	}
	if !picaService().Settings().DownloadTags {
		return
	}
	comicID := store.PathToID(t.LibraryID, t.ZipName)
	deadline := time.Now().Add(jmTagPollTimeout)
	for {
		exists, err := store.ComicRelativePathExists(t.LibraryID, t.ZipName, "")
		if err == nil && exists {
			if len(tags) > 0 {
				if err := store.AddTagsToComic(comicID, tags); err != nil {
					log.Printf("[pica] 自动标签写入失败(comic=%s): %v", comicID, err)
				} else {
					log.Printf("[pica] 已自动添加 %d 个标签(comic=%s, bookId=%s)", len(tags), comicID, t.BookID)
				}
			}
			if author != "" {
				if err := store.AddAuthorTagToComic(comicID, author); err != nil {
					if errors.Is(err, store.ErrAuthorNameConflictsWithTag) {
						log.Printf("[pica] 作者名与既有内容标签同名,跳过作者标签(comic=%s, author=%s)", comicID, author)
					} else {
						log.Printf("[pica] 作者标签写入失败(comic=%s): %v", comicID, err)
					}
				}
			}
			if author != "" {
				if item, err := store.GetComicByID(comicID); err == nil && item != nil &&
					strings.TrimSpace(item.Author) == "" {
					if err := store.UpdateComicFields(comicID, map[string]interface{}{"author": author}); err != nil {
						log.Printf("[pica] 作者字段回填失败(comic=%s): %v", comicID, err)
					}
				}
			}
			return
		}
		if err != nil {
			log.Printf("[pica] 自动标签:查询入库状态失败(comic=%s): %v", comicID, err)
			return
		}
		if time.Now().After(deadline) {
			log.Printf("[pica] 自动标签:等待入库超时(comic=%s),靠后续补扫兜底", comicID)
			return
		}
		time.Sleep(jmTagPollInterval)
	}
}

func registerPicaDownloadRoutes(g *gin.RouterGroup) {
	// 静态段先注册(与 /:id 同层级共存)
	g.GET("/downloads/dirs", func(c *gin.Context) {
		uid := getUserID(c)
		jmOK(c, gin.H{
			"dirs":    downloadDirCandidates(uid, picaService().Settings().DownloadDir, "pica"),
			"testDir": downloadTestDir("pica"),
		})
	})

	g.GET("/downloads", func(c *gin.Context) {
		jmOK(c, gin.H{"list": picaDownloads().List(), "tempRoot": picaDownloads().TempRoot()})
	})

	g.POST("/downloads", func(c *gin.Context) {
		// 哔咔内容接口一律要求登录:下载异步执行期间持续使用该 token
		token, ok := picaRequireToken(c)
		if !ok {
			return
		}
		var body struct {
			BookID  string `json:"bookId"`
			Title   string `json:"title"`
			Author  string `json:"author"`
			Orders  []int  `json:"orders"`
			DestDir string `json:"destDir"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			picaContent422(c, err.Error())
			return
		}
		if strings.TrimSpace(body.BookID) == "" {
			picaContent422(c, "bookId 必填")
			return
		}
		if strings.TrimSpace(body.DestDir) == "" {
			picaContent422(c, "destDir 必填")
			return
		}

		// 目标目录白名单校验:只允许书库管理中的目录(需管理权限)或内置测试目录
		uid := getUserID(c)
		var target *jmDownloadDir
		for _, dir := range downloadDirCandidates(uid, picaService().Settings().DownloadDir, "pica") {
			if sameCleanPath(dir.Path, body.DestDir) {
				d := dir
				target = &d
				break
			}
		}
		if target == nil {
			picaContent422(c, "下载目录不在候选列表中(请在书库管理中添加)")
			return
		}
		if !target.CanManage {
			c.JSON(403, gin.H{"detail": "Forbidden: 无该书库的管理权限"})
			return
		}

		picaDownloadMu.Lock()
		task, err := picaDownloads().Start(pica.DownloadRequest{
			BookID:    body.BookID,
			Title:     body.Title,
			Author:    body.Author,
			Orders:    body.Orders,
			Token:     token,
			DestDir:   target.Path,
			DestLabel: target.Label,
			LibraryID: target.LibraryID,
		})
		// 记住本次选择(设置读写失败不影响任务)
		if err == nil {
			st := picaService().Settings()
			if !sameCleanPath(st.DownloadDir, target.Path) {
				st.DownloadDir = target.Path
				_ = picaService().SaveSettings(st)
			}
		}
		picaDownloadMu.Unlock()

		if err != nil {
			picaFailErr(c, err, "创建下载任务失败")
			return
		}
		jmOK(c, task)
	})

	g.GET("/downloads/:id", func(c *gin.Context) {
		task := picaDownloads().Get(c.Param("id"))
		if task == nil {
			jmFail(c, pica.CodeNotFound, "任务不存在", nil)
			return
		}
		jmOK(c, task)
	})

	g.POST("/downloads/:id/cancel", func(c *gin.Context) {
		if !picaDownloads().Cancel(c.Param("id")) {
			jmFail(c, pica.CodeNotFound, "任务不存在或已结束", nil)
			return
		}
		jmOK(c, gin.H{"ok": true})
	})

	g.DELETE("/downloads/:id", func(c *gin.Context) {
		if !picaDownloads().Remove(c.Param("id")) {
			jmFail(c, pica.CodeNotFound, "任务不存在", nil)
			return
		}
		jmOK(c, gin.H{"ok": true})
	})
}
