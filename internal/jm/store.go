package jm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 本地持久化(schema 与 Python 版 store.py 一致,history.json/settings.json 可互迁):
// - settings.json: {"proxy": str, "imageQuality": "high|medium|low", "downloadTags": bool, ...白名单外键不回显}
// - history.json: {"list": [{aid,title,coverUrl,pid,epTitle,imageIndex,updatedAt}...]}
//   以 aid+pid 为幂等键,updatedAt 倒序。
// - tag-favorites.json: {"list": [{tag,createdAt}...]}(nowen-reader 私有扩展,
//   设备级共享,见 MOBILE_API.md §7);以 tag 为幂等键,createdAt 倒序。

// Settings 服务端设置(GET/PUT /api/jm/settings 契约)。
// downloadDir:批量下载默认归档目录(书库管理中的目录,空 = 用内置测试目录)。
// downloadTags:下载入库后自动把 JM 标签挂到书库 Comic(MOBILE_API.md §6,默认开)。
type Settings struct {
	Proxy        string `json:"proxy"`
	ImageQuality string `json:"imageQuality"`
	DownloadDir  string `json:"downloadDir"`
	DownloadTags bool   `json:"downloadTags"`
}

// DefaultProxy 默认上游代理(Python 版 JM_PROXY 默认值)。
const DefaultProxy = "http://127.0.0.1:10809"

func normalizeQuality(q string) string {
	switch q {
	case "medium", "low":
		return q
	default:
		// high 与旧版遗留值(如 original)统一归一化为 high
		return "high"
	}
}

// Store 管理本地 JSON 持久化(history 与 settings,mock/live 语义下两模式共用同一份)。
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dataDir string) *Store {
	return &Store{dir: dataDir}
}

func (s *Store) settingsPath() string { return filepath.Join(s.dir, "settings.json") }
func (s *Store) historyPath() string  { return filepath.Join(s.dir, "history.json") }

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadSettings 读设置;proxy 为 null/缺省时回退 JM_PROXY(env)再回退默认值。
// downloadTags 缺省(旧 settings.json 无该键)为 true。
func (s *Store) LoadSettings(envProxy string) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	def := Settings{Proxy: DefaultProxy, ImageQuality: "high", DownloadTags: true}
	if envProxy != "" {
		def.Proxy = envProxy
	}
	raw, err := os.ReadFile(s.settingsPath())
	if err != nil {
		return def
	}
	var disk map[string]any
	if json.Unmarshal(raw, &disk) != nil {
		return def
	}
	out := def
	if v, ok := disk["proxy"].(string); ok {
		out.Proxy = v // 空串 = 直连(显式清除)
	}
	if v, ok := disk["imageQuality"].(string); ok {
		out.ImageQuality = normalizeQuality(v)
	}
	if v, ok := disk["downloadDir"].(string); ok {
		out.DownloadDir = v
	}
	if v, ok := disk["downloadTags"].(bool); ok {
		out.DownloadTags = v
	}
	return out
}

// SaveSettings 写设置(仅白名单键),tmp 原子替换。
func (s *Store) SaveSettings(st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.ImageQuality = normalizeQuality(st.ImageQuality)
	data, err := json.Marshal(map[string]any{
		"proxy":        st.Proxy,
		"imageQuality": st.ImageQuality,
		"downloadDir":  st.DownloadDir,
		"downloadTags": st.DownloadTags,
	})
	if err != nil {
		return err
	}
	return atomicWrite(s.settingsPath(), data)
}

// HistoryItem 单条阅读历史(契约 #24/#25)。
type HistoryItem struct {
	Aid        string `json:"aid"`
	Title      string `json:"title"`
	CoverURL   string `json:"coverUrl"`
	Pid        string `json:"pid"`
	EpTitle    any    `json:"epTitle"` // string|null
	ImageIndex int    `json:"imageIndex"`
	UpdatedAt  string `json:"updatedAt"` // 本地时区 ISO8601 秒精度
}

// ReportHistory 上报进度;同 aid+pid 幂等覆盖并刷新时间。
func (s *Store) ReportHistory(item HistoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadHistoryLocked()
	key := item.Aid + "+" + item.Pid
	replaced := false
	for i := range list {
		if list[i].Aid+"+"+list[i].Pid == key {
			list[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, item)
	}
	return s.saveHistoryLocked(list)
}

func (s *Store) loadHistoryLocked() []HistoryItem {
	raw, err := os.ReadFile(s.historyPath())
	if err != nil {
		return nil
	}
	var disk struct {
		List []HistoryItem `json:"list"`
	}
	if json.Unmarshal(raw, &disk) != nil {
		return nil
	}
	return disk.List
}

func (s *Store) saveHistoryLocked(list []HistoryItem) error {
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt > list[j].UpdatedAt
	})
	data, err := json.Marshal(map[string]any{"list": list})
	if err != nil {
		return err
	}
	return atomicWrite(s.historyPath(), data)
}

// ListHistory 按 updatedAt 倒序分页(页大小 20,契约 #24)。
func (s *Store) ListHistory(page int) (list []HistoryItem, total int, hasNext bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.loadHistoryLocked()
	sort.Slice(all, func(i, j int) bool {
		return all[i].UpdatedAt > all[j].UpdatedAt
	})
	const pageSize = 20
	total = len(all)
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	if start >= total {
		return []HistoryItem{}, total, false
	}
	if end > total {
		end = total
	}
	return all[start:end], total, end < total
}

// ClearHistory 清空全部(契约 #26)。
func (s *Store) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveHistoryLocked([]HistoryItem{})
}

// nowStamp 本地时区 ISO8601 秲精度(Python datetime.now().isoformat(timespec="seconds") 对齐)。
func nowStamp() string { return time.Now().Format("2006-01-02T15:04:05") }

/* ── 标签收藏(私有扩展,设备级共享;tag-favorites.json) ── */

// TagFavorite 单条标签收藏(私有扩展端点 GET/POST/DELETE /api/jm/tag-favorites)。
type TagFavorite struct {
	Tag       string `json:"tag"`
	CreatedAt string `json:"createdAt"` // nowStamp 格式,同 history
}

func (s *Store) tagFavoritesPath() string { return filepath.Join(s.dir, "tag-favorites.json") }

// AddTagFavorite 收藏标签;同 tag 幂等(已存在不重复加、不刷新时间),tag 首尾空白剔除。
func (s *Store) AddTagFavorite(tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadTagFavoritesLocked()
	for i := range list {
		if list[i].Tag == tag {
			return nil
		}
	}
	list = append(list, TagFavorite{Tag: tag, CreatedAt: nowStamp()})
	return s.saveTagFavoritesLocked(list)
}

// RemoveTagFavorite 取消收藏;tag 不存在同样幂等成功。
func (s *Store) RemoveTagFavorite(tag string) error {
	tag = strings.TrimSpace(tag)
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadTagFavoritesLocked()
	kept := list[:0]
	for _, it := range list {
		if it.Tag != tag {
			kept = append(kept, it)
		}
	}
	return s.saveTagFavoritesLocked(kept)
}

// ListTagFavorites 全量返回(设备级列表小,不分页),createdAt 倒序(最新收藏在前);
// 同秒并列时保持入库顺序(稳定排序)。
func (s *Store) ListTagFavorites() ([]TagFavorite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadTagFavoritesLocked()
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].CreatedAt > list[j].CreatedAt
	})
	return list, nil
}

func (s *Store) loadTagFavoritesLocked() []TagFavorite {
	raw, err := os.ReadFile(s.tagFavoritesPath())
	if err != nil {
		return nil
	}
	var disk struct {
		List []TagFavorite `json:"list"`
	}
	if json.Unmarshal(raw, &disk) != nil {
		return nil
	}
	return disk.List
}

func (s *Store) saveTagFavoritesLocked(list []TagFavorite) error {
	data, err := json.Marshal(map[string]any{"list": list})
	if err != nil {
		return err
	}
	return atomicWrite(s.tagFavoritesPath(), data)
}
