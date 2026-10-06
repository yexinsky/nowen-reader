package jm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 会话有效期:7 天滑动窗口。与 Python 版(自创建起算、重启即失效)的差异:
// - 滑动续期:鉴权命中且距上次续期超过 renewInterval 时续满 TTL,活跃用户不过期;
// - 落盘持久化:sessions.json(0600,含上游登录 cookies),服务重启不失效。
// 真正的过期条件是「连续 7 天不活跃」(续期粒度 24h,实际过期时刻最多晚 24h)。
const (
	sessionTTL    = 7 * 24 * time.Hour
	renewInterval = 24 * time.Hour
	sessionFile   = "sessions.json"
)

// Session 是一个登录会话:Bearer token → 上游 cookies(登录态)+ 用户快照 + 代理快照。
type Session struct {
	Token     string            `json:"token"`
	Cookies   map[string]string `json:"cookies"`
	UserInfo  map[string]any    `json:"userInfo"`
	ProxyKey  string            `json:"proxyKey"`
	CreatedAt time.Time         `json:"createdAt"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// SessionManager 会话表:内存为主 + sessions.json 落盘(重启存活)。
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	path     string // 落盘文件;空 = 仅内存(测试用)
}

func NewSessionManager(dataDir string) *SessionManager {
	m := &SessionManager{sessions: make(map[string]*Session)}
	if dataDir != "" {
		m.path = filepath.Join(dataDir, sessionFile)
		m.load()
	}
	return m
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败属于系统性错误,退化为时间+随机拼接仍需 64 hex
		return hex.EncodeToString([]byte(fmt.Sprintf("%x%d", time.Now().UnixNano(), time.Now().UnixNano())))[:64]
	}
	return hex.EncodeToString(b)
}

// Create 写入新会话(有效期 sessionTTL)并清理过期会话。
func (m *SessionManager) Create(cookies map[string]string, userInfo map[string]any, proxyKey string) *Session {
	now := time.Now()
	s := &Session{
		Token:     newToken(),
		Cookies:   cookies,
		UserInfo:  userInfo,
		ProxyKey:  proxyKey,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.sessions {
		if !v.ExpiresAt.After(now) {
			delete(m.sessions, k)
		}
	}
	m.sessions[s.Token] = s
	m.save()
	return s
}

// Get 按 token 取会话;不存在或已过期返回 nil(调用方转 1002/401)。
// 命中且距上次续期超过 renewInterval 时续满 TTL(活跃用户不过期,每天最多落盘一次)。
func (m *SessionManager) Get(token string) *Session {
	if token == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[token]
	if !ok {
		return nil
	}
	now := time.Now()
	if !s.ExpiresAt.After(now) {
		delete(m.sessions, token)
		m.save()
		return nil
	}
	if time.Until(s.ExpiresAt) < sessionTTL-renewInterval {
		s.ExpiresAt = now.Add(sessionTTL)
		m.save()
	}
	return s
}

// UpdateCookies 搬运会话 cookies(代理变更重建客户端后保持登录态)。
func (m *SessionManager) UpdateCookies(token string, cookies map[string]string, proxyKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[token]; ok {
		s.Cookies = cookies
		s.ProxyKey = proxyKey
		m.save()
	}
}

// Delete 销毁会话(登出)。
func (m *SessionManager) Delete(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[token]; !ok {
		return
	}
	delete(m.sessions, token)
	m.save()
}

// load 启动时从 sessions.json 恢复会话,丢弃已过期条目;文件缺失/损坏视为空表。
func (m *SessionManager) load() {
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return // 首次启动无文件
	}
	var disk struct {
		Sessions map[string]*Session `json:"sessions"`
	}
	if json.Unmarshal(raw, &disk) != nil || disk.Sessions == nil {
		log.Printf("[jm] %s 解析失败,忽略已有会话", m.path)
		return
	}
	now := time.Now()
	for token, s := range disk.Sessions {
		if s == nil || !s.ExpiresAt.After(now) {
			continue
		}
		m.sessions[token] = s
	}
}

// save 落盘会话表(tmp 原子替换,0600——内容含上游登录 cookies);调用方须持有 m.mu。
func (m *SessionManager) save() {
	if m.path == "" {
		return
	}
	data, err := json.Marshal(map[string]any{"sessions": m.sessions})
	if err != nil {
		// 当前字段均可序列化,不应到达;一旦到达意味着重启后会话悄悄全丢,必须留痕
		log.Printf("[jm] 会话序列化失败,跳过本次落盘: %v", err)
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("[jm] 会话落盘失败: %v", err)
		return
	}
	if err := os.Rename(tmp, m.path); err != nil {
		log.Printf("[jm] 会话落盘失败: %v", err)
	}
}

// BearerToken 从 Authorization 头提取 Bearer token(大小写不敏感,与 Python 版 deps 一致)。
func BearerToken(authHeader string) string {
	const prefix = "bearer "
	if len(authHeader) >= len(prefix) && equalFold(authHeader[:len(prefix)], prefix) {
		return authHeader[len(prefix):]
	}
	return ""
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

var _ = context.Background
