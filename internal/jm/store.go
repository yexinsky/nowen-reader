package jm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// 本地持久化(schema 与 Python 版 store.py 一致,history.json/settings.json 可互迁):
// - settings.json: {"proxy": str, "imageQuality": "high|medium|low", ...白名单外键不回显}
// - history.json: {"list": [{aid,title,coverUrl,pid,epTitle,imageIndex,updatedAt}...]}
//   以 aid+pid 为幂等键,updatedAt 倒序。

// Settings 服务端设置(GET/PUT /api/jm/settings 契约)。
type Settings struct {
	Proxy        string `json:"proxy"`
	ImageQuality string `json:"imageQuality"`
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
func (s *Store) LoadSettings(envProxy string) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	def := Settings{Proxy: DefaultProxy, ImageQuality: "high"}
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
