package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// TagFilterHandler handles the user-defined tag blocklist API endpoints.
//
//	GET    /api/tags/filters      → 名单（含与标签库的关联信息）
//	POST   /api/tags/filters      → 批量添加 { names: [...] }
//	DELETE /api/tags/filters/:id  → 移除一条
type TagFilterHandler struct{}

// NewTagFilterHandler creates a new TagFilterHandler.
func NewTagFilterHandler() *TagFilterHandler {
	return &TagFilterHandler{}
}

// maxTagFilterBatch 单次添加条数上限（名单量级为几十条，超量多半是脚本误用）。
const maxTagFilterBatch = 200

// GET /api/tags/filters — 过滤名单
func (h *TagFilterHandler) List(c *gin.Context) {
	list, err := store.ListTagFilters()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tag filters"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list})
}

// POST /api/tags/filters — 批量添加（幂等，同 normKey 已存在则跳过）
func (h *TagFilterHandler) Add(c *gin.Context) {
	var body struct {
		Names []string `json:"names"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "names required"})
		return
	}
	// 去空白后校验：全空或超量直接拒绝，避免落库后才让用户发现
	cleaned := make([]string, 0, len(body.Names))
	for _, name := range body.Names {
		if name = strings.TrimSpace(name); name != "" {
			cleaned = append(cleaned, name)
		}
	}
	if len(cleaned) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "names required"})
		return
	}
	if len(cleaned) > maxTagFilterBatch {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "单次最多添加 " + strconv.Itoa(maxTagFilterBatch) + " 个标签"})
		return
	}

	added, skipped, err := store.AddTagFilters(cleaned)
	if err != nil {
		if errors.Is(err, store.ErrTagFilterNameEmpty) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "标签名过长（上限 100 字符）"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add tag filters"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "added": added, "skipped": skipped})
}

// DELETE /api/tags/filters/:id — 移除一条（幂等）
func (h *TagFilterHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := store.RemoveTagFilter(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove tag filter"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
