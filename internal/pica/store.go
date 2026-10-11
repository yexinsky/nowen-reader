package pica

// 本地持久化(与 JM 流 store 同构):JSON 文件,原子替换。
// - settings.json: {"proxy": str, "imageQuality": "original|low|medium|high", "downloadDir": str, "downloadTags": bool}
// - history.json:  {"list": [{id,title,coverUrl,order,epTitle,imageIndex,updatedAt}...]}
//   以 id+order 为幂等键,updatedAt 倒序。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Settings 服务端设置(GET/PUT /api/pica/settings 契约)。
// downloadDir:批量下载默认归档目录(书库管理中的目录,空 = 用内置测试目录)。
// downloadTags:下载入库后自动把哔咔标签挂到书库 Comic(默认开)。
type Settings struct {
	Proxy        string `json:"proxy"`
	ImageQuality string `json:"imageQuality"`
	DownloadDir  string `json:"downloadDir"`
	DownloadTags bool   `json:"downloadTags"`
}

// normalizeQuality 画质归一(original 为哔咔 app 默认)。
func normalizeQuality(q string) string {
	if imageQualityValues[q] {
		return q
	}
	return "original"
}

// Store 管理本地 JSON 持久化。
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore 构造(dataDir 通常为 <DataDir>/pica)。
func NewStore(dataDir string) *Store {
	return &Store{dir: dataDir}
}

func (s *Store) settingsPath() string { return filepath.Join(s.dir, "settings.json") }
func (s *Store) historyPath() string  { return filepath.Join(s.dir, "history.json") }

// Dir 数据目录(诊断用)。
func (s *Store) Dir() string { return s.dir }

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadSettings 读设置;proxy 缺省为空串(直连),env 覆盖由 Service 层处理。
// downloadTags 缺省(旧 settings.json 无该键)为 true。
// envProxy:PICA_UPSTREAM_PROXY 环境变量(落盘值缺省时回退,与 JM 流语义一致)。
func (s *Store) LoadSettings(envProxy string) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Settings{Proxy: envProxy, ImageQuality: "original", DownloadTags: true}
	raw, err := os.ReadFile(s.settingsPath())
	if err != nil {
		return out
	}
	var disk map[string]any
	if json.Unmarshal(raw, &disk) != nil {
		return out
	}
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
	data, err := json.Marshal(map[string]any{
		"proxy":        st.Proxy,
		"imageQuality": normalizeQuality(st.ImageQuality),
		"downloadDir":  st.DownloadDir,
		"downloadTags": st.DownloadTags,
	})
	if err != nil {
		return err
	}
	return atomicWrite(s.settingsPath(), data)
}

/* ── 阅读历史 ── */

// HistoryItem 源内阅读历史(阅读单元 = 章节 order)。
type HistoryItem struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	CoverURL   string  `json:"coverUrl"`
	Order      int     `json:"order"`
	EpTitle    *string `json:"epTitle"`
	ImageIndex int     `json:"imageIndex"`
	UpdatedAt  string  `json:"updatedAt"`
}

func historyKey(id string, order int) string { return id + "#" + itoaHistory(order) }

func itoaHistory(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ListHistory 历史列表(updatedAt 倒序,分页)。
func (s *Store) ListHistory(page int) ([]HistoryItem, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.loadHistory()
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	const pageSize = 30
	start := (page - 1) * pageSize
	if start > len(items) {
		start = len(items)
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], len(items), end < len(items)
}

// ReportHistory 上报历史(幂等键 id+order,存在即更新置顶)。
func (s *Store) ReportHistory(item HistoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.loadHistory()
	key := historyKey(item.ID, item.Order)
	kept := items[:0]
	for _, it := range items {
		if historyKey(it.ID, it.Order) != key {
			kept = append(kept, it)
		}
	}
	items = append(kept, item)
	if len(items) > 500 {
		items = items[len(items)-500:]
	}
	return s.saveHistory(items)
}

// ClearHistory 清空历史。
func (s *Store) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveHistory([]HistoryItem{})
}

// loadHistory 读取历史(调用方持锁)。
func (s *Store) loadHistory() []HistoryItem {
	raw, err := os.ReadFile(s.historyPath())
	if err != nil {
		return []HistoryItem{}
	}
	var disk struct {
		List []HistoryItem `json:"list"`
	}
	if json.Unmarshal(raw, &disk) != nil || disk.List == nil {
		return []HistoryItem{}
	}
	return disk.List
}

// saveHistory 写历史(调用方持锁)。
func (s *Store) saveHistory(items []HistoryItem) error {
	data, err := json.Marshal(map[string]any{"list": items})
	if err != nil {
		return err
	}
	return atomicWrite(s.historyPath(), data)
}
