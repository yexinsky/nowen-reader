package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// TagHandler handles tag-related API endpoints.
type TagHandler struct{}

// NewTagHandler creates a new TagHandler.
func NewTagHandler() *TagHandler {
	return &TagHandler{}
}

// GET /api/tags — List all tags
func (h *TagHandler) ListTags(c *gin.Context) {
	tags, err := store.GetAllTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// PUT /api/tags/color — Update tag color
func (h *TagHandler) UpdateTagColor(c *gin.Context) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" || body.Color == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and color required"})
		return
	}

	if err := store.UpdateTagColor(body.Name, body.Color); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tag color"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// PUT /api/tags/rename — Rename (or merge) a tag
func (h *TagHandler) RenameTag(c *gin.Context) {
	var body struct {
		OldName string `json:"oldName"`
		NewName string `json:"newName"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.OldName == "" || body.NewName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "oldName and newName required"})
		return
	}

	if err := store.RenameTag(body.OldName, body.NewName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rename tag"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DELETE /api/tags — Delete a tag entirely (removes from all comics)
func (h *TagHandler) DeleteTag(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}

	if err := store.DeleteTag(body.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tag"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// POST /api/tags/merge — Merge multiple tags into one
func (h *TagHandler) MergeTags(c *gin.Context) {
	var body struct {
		SourceNames []string `json:"sourceNames"`
		TargetName  string   `json:"targetName"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.SourceNames) == 0 || body.TargetName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sourceNames and targetName required"})
		return
	}

	for _, src := range body.SourceNames {
		if src != body.TargetName {
			if err := store.RenameTag(src, body.TargetName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to merge tags"})
				return
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================
// 标签归一管理 M1
// ============================================================

// pageParams 解析分页参数（默认 page=1, pageSize=20）。
func pageParams(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	return page, pageSize
}

// GET /api/tags/normalization/preview — 归一预览（同 normKey 的标签簇）
func (h *TagHandler) PreviewTagNormalization(c *gin.Context) {
	page, pageSize := pageParams(c)
	clusters, total, err := store.PreviewTagNormalization(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to preview tag normalization"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"clusters": clusters,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// GET /api/tags/normalization/operations — 合并操作日志
func (h *TagHandler) ListTagOperations(c *gin.Context) {
	page, pageSize := pageParams(c)
	list, total, err := store.ListTagOperations(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list tag operations"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"list":     list,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// POST /api/tags/normalization/apply — 执行合并
func (h *TagHandler) ApplyTagNormalization(c *gin.Context) {
	var body struct {
		TargetTagID  int   `json:"targetTagId"`
		SourceTagIDs []int `json:"sourceTagIds"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.TargetTagID <= 0 || len(body.SourceTagIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "targetTagId and sourceTagIds required"})
		return
	}

	comicCount, err := store.ApplyTagMerge(body.TargetTagID, body.SourceTagIDs)
	if err != nil {
		if errors.Is(err, store.ErrTagNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to merge tags"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "comicCount": comicCount})
}

// POST /api/tags/normalization/undo — 撤销合并
func (h *TagHandler) UndoTagNormalization(c *gin.Context) {
	var body struct {
		OperationID int64 `json:"operationId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.OperationID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operationId required"})
		return
	}

	if err := store.UndoTagOperation(body.OperationID); err != nil {
		switch {
		case errors.Is(err, store.ErrTagOperationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Operation not found"})
		case errors.Is(err, store.ErrTagOperationUndone):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Operation already undone"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to undo operation"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/tags/normalization/ignore — 忽略一个 normKey 簇
func (h *TagHandler) IgnoreTagNormKey(c *gin.Context) {
	var body struct {
		NormKey string `json:"normKey"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.NormKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "normKey required"})
		return
	}

	if err := store.IgnoreTagNormKey(body.NormKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to ignore normKey"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/tags/normalization/unignore — 取消忽略
func (h *TagHandler) UnignoreTagNormKey(c *gin.Context) {
	var body struct {
		NormKey string `json:"normKey"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.NormKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "normKey required"})
		return
	}

	if err := store.UnignoreTagNormKey(body.NormKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unignore normKey"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/tags/normalization/ignores — 忽略列表
func (h *TagHandler) ListTagNormIgnores(c *gin.Context) {
	list, err := store.ListTagNormIgnores()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list ignores"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list})
}

// GET /api/tags/aliases — 别名列表
func (h *TagHandler) ListTagAliases(c *gin.Context) {
	list, err := store.ListTagAliases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list aliases"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list})
}

// POST /api/tags/aliases — 新增别名（与现有标签名重复 → 422）
func (h *TagHandler) AddTagAlias(c *gin.Context) {
	var body struct {
		Alias string `json:"alias"`
		TagID int    `json:"tagId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Alias == "" || body.TagID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alias and tagId required"})
		return
	}

	if err := store.AddTagAlias(body.Alias, body.TagID); err != nil {
		switch {
		case errors.Is(err, store.ErrTagNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
		case errors.Is(err, store.ErrAliasConflictsWithTagName):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Alias conflicts with existing tag name"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add alias"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /api/tags/aliases?alias=xx — 删除别名
func (h *TagHandler) DeleteTagAlias(c *gin.Context) {
	alias := c.Query("alias")
	if alias == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alias required"})
		return
	}

	if err := store.DeleteTagAlias(alias); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete alias"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
