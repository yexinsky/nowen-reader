package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// SnapshotHandler handles tag/category domain snapshot API endpoints.
type SnapshotHandler struct{}

// NewSnapshotHandler creates a new SnapshotHandler.
func NewSnapshotHandler() *SnapshotHandler {
	return &SnapshotHandler{}
}

// autoSnapshotForAI 在 AI 批量写操作落库前自动创建快照。
// 返回错误时调用方应拒绝执行写操作（没有安全网不冒险）。
func autoSnapshotForAI(reason string) error {
	_, err := store.CreateSnapshot(store.SnapshotDomainTagCategory, "自动 · "+reason, "auto", reason)
	return err
}

// GET /api/snapshots — 快照列表（标签+分类域）
func (h *SnapshotHandler) List(c *gin.Context) {
	list, err := store.ListSnapshots(store.SnapshotDomainTagCategory)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list snapshots"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list})
}

// POST /api/snapshots — 手动创建快照 { name?: string }
func (h *SnapshotHandler) Create(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&body)

	item, err := store.CreateSnapshot(store.SnapshotDomainTagCategory, body.Name, "manual", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create snapshot"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "snapshot": item})
}

// POST /api/snapshots/:id/restore — 恢复快照（整域替换，恢复前自动保存当前状态）
func (h *SnapshotHandler) Restore(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid snapshot id"})
		return
	}

	result, err := store.RestoreSnapshot(id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrSnapshotNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Snapshot not found"})
		case errors.Is(err, store.ErrSnapshotDomainUnknown):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Unknown snapshot domain"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore snapshot"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "restored": result})
}

// DELETE /api/snapshots/:id — 删除快照
func (h *SnapshotHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid snapshot id"})
		return
	}

	if err := store.DeleteSnapshot(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete snapshot"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
