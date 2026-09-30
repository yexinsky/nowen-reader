package jm

import (
	"sync"
)

// Backend 等价于 live.py 的 LiveBackend:
// - 匿名客户端进程内共享,代理(settings.proxy)变更后重建
// - 会话客户端独立构建(cookies 隔离),代理变更后由 SessionManager 侧惰性重建
// - 重建即丢弃旧客户端:Go 的 Client 是轻量对象(共享进程级 ts)
type Backend struct {
	mu       sync.Mutex
	store    *Store
	anon     *Client
	proxyKey string
	sessions *SessionManager
}

func NewBackend(store *Store, sessions *SessionManager) *Backend {
	return &Backend{store: store, sessions: sessions}
}

// currentProxyKey 读当前 settings.proxy(None 语义:未落盘时回退 env/默认,空串=直连)。
func (b *Backend) currentProxyKey(envProxy string) string {
	return b.store.LoadSettings(envProxy).Proxy
}

// AnonClient 匿名浏览客户端(共享)。
func (b *Backend) AnonClient(envProxy string) *Client {
	b.mu.Lock()
	defer b.mu.Unlock()
	proxyKey := b.currentProxyKey(envProxy)
	if b.anon == nil || b.proxyKey != proxyKey {
		b.anon = NewClient(proxyKey, nil)
		b.proxyKey = proxyKey
	}
	return b.anon
}

// NewSessionClient 为登录会话创建独立客户端(cookies 隔离)。
func (b *Backend) NewSessionClient(envProxy string, cookies map[string]string) *Client {
	return NewClient(b.currentProxyKey(envProxy), cookies)
}

// Invalidate 代理设置变更后调用(live.py BACKEND.invalidate)。
func (b *Backend) Invalidate() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.anon = nil
}

// API 封装:调用方(handler)通过 Backend 拿到正确作用的 Client 再调 API。
// Setting / Login / CaptchaBytes 等见 api.go。
