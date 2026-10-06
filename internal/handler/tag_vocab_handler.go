package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// TagVocabHandler handles the curated target vocabulary API endpoints.
type TagVocabHandler struct{}

// NewTagVocabHandler creates a new TagVocabHandler.
func NewTagVocabHandler() *TagVocabHandler {
	return &TagVocabHandler{}
}

// GET /api/tags/vocabulary — 词表（带用量）
func (h *TagVocabHandler) List(c *gin.Context) {
	list, err := store.ListTagVocabulary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vocabulary"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list})
}

// POST /api/tags/vocabulary — 增/删词表项 { add?: [tagId], remove?: [tagId] }
func (h *TagVocabHandler) Update(c *gin.Context) {
	var body struct {
		Add    []int `json:"add"`
		Remove []int `json:"remove"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || (len(body.Add) == 0 && len(body.Remove) == 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "add or remove required"})
		return
	}

	if err := store.RemoveTagsFromVocabulary(body.Remove); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove from vocabulary"})
		return
	}
	if err := store.AddTagsToVocabulary(body.Add); err != nil {
		switch {
		case errors.Is(err, store.ErrTagNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
		case errors.Is(err, store.ErrTagVocabNormKeyConflict):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "词表中已存在同写法（简繁/大小写）的标签，请先移除或换一个"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add to vocabulary"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
