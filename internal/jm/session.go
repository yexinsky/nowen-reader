package jm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// 会话 TTL(7 天,自创建时刻起算;与 Python 版 core/security.py 一致)。
const sessionTTL = 7 * 24 * time.Hour

// Session 是一个登录会话:Bearer token → 上游 cookies(登录态)+ 用户快照 + 代理快照。
type Session struct {
	Token     string
	Cookies   map[string]string
	UserInfo  map[string]any
	ProxyKey  string
	CreatedAt time.Time
}

// SessionManager 进程内存会话表;重启即全部失效(与 Python 版一致的设计)。
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败属于系统性错误,退化为时间+随机拼接仍需 64 hex
		return hex.EncodeToString([]byte(fmt.Sprintf("%x%d", time.Now().UnixNano(), time.Now().UnixNano())))[:64]
	}
	return hex.EncodeToString(b)
}

// Create 写入新会话并清理过期会话(建会话即清理,与 Python 版一致)。
func (m *SessionManager) Create(cookies map[string]string, userInfo map[string]any, proxyKey string) *Session {
	s := &Session{
		Token:     newToken(),
		Cookies:   cookies,
		UserInfo:  userInfo,
		ProxyKey:  proxyKey,
		CreatedAt: time.Now(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, v := range m.sessions {
		if now.Sub(v.CreatedAt) > sessionTTL {
			delete(m.sessions, k)
		}
	}
	m.sessions[s.Token] = s
	return s
}

// Get 按 token 取会话;不存在或已过期返回 nil(调用方转 1002/401)。
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
	if time.Since(s.CreatedAt) > sessionTTL {
		delete(m.sessions, token)
		return nil
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
	}
}

// Delete 销毁会话(登出)。
func (m *SessionManager) Delete(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
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
