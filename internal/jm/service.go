package jm

import (
	"os"
	"path/filepath"
)

// Service 聚合 JM 源的全部运行时部件,handler 层单例持有。
// 启动零外呼:所有上游连接在首次 API 调用时惰性建立(满足"只在进入模块时调用 api")。
type Service struct {
	Store    *Store
	Sessions *SessionManager
	backend  *Backend

	// envProxy:JM_UPSTREAM_PROXY 环境变量(> settings.json 落盘值 > 默认 10809 的次序中最高优先)
	envProxy string
}

// NewService 创建服务;dataDir 通常为 <DataDir>/jm。
func NewService(dataDir, envProxy string) *Service {
	_ = os.MkdirAll(dataDir, 0o755)
	store := NewStore(dataDir)
	svc := &Service{
		Store:    store,
		Sessions: NewSessionManager(),
		envProxy: envProxy,
	}
	svc.backend = NewBackend(store, svc.Sessions)
	return svc
}

// EnvProxy 返回环境变量代理(可为空)。
func (s *Service) EnvProxy() string { return s.envProxy }

// AnonClient 匿名浏览客户端(代理变更自动重建)。
func (s *Service) AnonClient() *Client { return s.backend.AnonClient(s.envProxy) }

// NewSessionClient 为登录创建独立客户端(live.py new_session_client)。
func (s *Service) NewSessionClient() *Client { return s.backend.NewSessionClient(s.envProxy, nil) }

// SessionClient 取会话上游客户端;代理与建会话时不一致则惰性重建并搬运 cookies
// (live.py ensure_session_client 语义)。token 无效返回 nil。
func (s *Service) SessionClient(token string) (*Client, *Session) {
	sess := s.Sessions.Get(token)
	if sess == nil {
		return nil, nil
	}
	proxyKey := s.currentProxyKey()
	if sess.ProxyKey == proxyKey {
		// 会话客户端不常驻,按需重建(cookies 保持)
		return s.backend.NewSessionClient(s.envProxy, sess.Cookies), sess
	}
	_ = s.backend // 重建
	client := s.backend.NewSessionClient(s.envProxy, sess.Cookies)
	s.Sessions.UpdateCookies(token, sess.Cookies, proxyKey)
	return client, sess
}

// CurrentProxyKey 当前生效代理(落盘设置优先于 env 语义与 Python 版一致:
// settings.json 里显式存过 proxy 就用它,否则回退 env,再回退默认)。
func (s *Service) currentProxyKey() string { return s.Store.LoadSettings(s.envProxy).Proxy }

// Settings 读当前设置(契约 #27)。
func (s *Service) Settings() Settings {
	st := s.Store.LoadSettings(s.envProxy)
	if st.ImageQuality == "" {
		st.ImageQuality = "high"
	}
	return st
}

// SaveSettings 写设置并按 live.py 语义失效匿名客户端(代理变更即时生效)。
func (s *Service) SaveSettings(st Settings) error {
	st.ImageQuality = normalizeQuality(st.ImageQuality)
	if err := s.Store.SaveSettings(st); err != nil {
		return err
	}
	s.backend.Invalidate()
	return nil
}

// DefaultDataDir 默认数据目录:<nowen DataDir>/jm(由 handler 层传入 config.DataDir())。
func DefaultDataDir(baseDataDir string) string {
	return filepath.Join(baseDataDir, "jm")
}
