// Package imgcache 在线源图片磁盘 LRU 缓存(JM/Pica 等在线源共用)。
// 从 internal/jm 原样抽移(2026-10,PicACG 流接入),行为保持不变。
package imgcache

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache 落盘缓存:键为调用方给定的任意字符串(通常是 md5 摘要),文件 {key}.jpg。
// 命中即刷新 mtime(真 LRU);写入为 .tmp 原子替换 + 写前二次命中检查(并发同 key 复用);
// 按文件数 LRU 淘汰(超限删最旧 mtime)。
type Cache struct {
	dir   string
	limit int
	mu    sync.Mutex
}

// NewCache 构造缓存;dir 不存在时在写入时惰性创建。limit 为文件数上限。
func NewCache(dir string, limit int) *Cache {
	if limit < 1 {
		limit = 1
	}
	return &Cache{dir: dir, limit: limit}
}

// Dir 返回缓存目录(测试/诊断用)。
func (c *Cache) Dir() string { return c.dir }

func (c *Cache) keyPath(key string) string {
	return filepath.Join(c.dir, key+".jpg")
}

// Get 读取缓存;命中刷新 mtime。
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.keyPath(key)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	_ = os.Chtimes(p, time.Now(), time.Now())
	return data, true
}

// Put 写入缓存:.tmp 原子 os.Rename + 写前二次命中检查(并发同 key 复用先写入者)。
// 返回实际生效的字节与是否复用既有文件;写入失败返回错误。
func (c *Cache) Put(key string, data []byte) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return nil, false, err
	}
	p := c.keyPath(key)
	// 写前二次命中检查:并发同 key 时另一个请求可能已落盘,直接复用
	if exist, err := os.ReadFile(p); err == nil {
		_ = os.Chtimes(p, time.Now(), time.Now())
		return exist, true, nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return nil, false, err
	}
	c.evictLocked()
	return data, false, nil
}

// evictLocked 按文件数 LRU 淘汰:扫描 *.jpg,超限删最旧 mtime。调用方需持有 mu。
func (c *Cache) evictLocked() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type item struct {
		path string
		mod  time.Time
	}
	items := make([]item, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jpg") {
			continue // 排除 .tmp 中转与目录
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{filepath.Join(c.dir, e.Name()), info.ModTime()})
	}
	if len(items) <= c.limit {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.Before(items[j].mod) })
	for i := 0; i < len(items)-c.limit; i++ {
		_ = os.Remove(items[i].path)
	}
}
