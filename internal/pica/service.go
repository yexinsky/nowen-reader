package pica

// Service 聚合哔咔源的全部运行时部件,handler 层单例持有。
// 启动零外呼:所有上游连接在首次 API 调用时惰性建立。
// 与 JM 的差异:哔咔会话是无状态 JWT,无需服务端会话表——客户端按
// (proxy|imageQuality) 键池化,token 由每次请求透传。

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/nowen-reader/nowen-reader/internal/imgcache"
)

type Service struct {
	Store *Store

	// envProxy:PICA_UPSTREAM_PROXY 环境变量(落盘设置缺省 proxy 时回退)
	envProxy string

	apiPool *ClientPool // (proxy|quality) 键控的上游客户端池

	imgMu          sync.Mutex
	imgClient      *Client
	imgClientProxy string

	imgCacheOnce sync.Once
	imgCache     *imgcache.Cache

	downloads *DownloadManager
}

// NewService 创建服务;dataDir 通常为 <DataDir>/pica。
func NewService(dataDir, envProxy string) *Service {
	_ = os.MkdirAll(dataDir, 0o755)
	store := NewStore(dataDir)
	svc := &Service{Store: store, envProxy: envProxy}
	svc.apiPool = NewClientPool(func() (*Client, error) {
		st := svc.Settings()
		return NewClient(st.Proxy, st.ImageQuality)
	})
	svc.downloads = NewDownloadManager(svc, filepath.Join(dataDir, "download-tmp"))
	return svc
}

// Downloads 批量下载任务管理器。
func (s *Service) Downloads() *DownloadManager { return s.downloads }

// Settings 读当前设置(proxy 落盘值缺省时回退 env)。
func (s *Service) Settings() Settings {
	return s.Store.LoadSettings(s.envProxy)
}

// SaveSettings 写设置。
func (s *Service) SaveSettings(st Settings) error {
	st.ImageQuality = normalizeQuality(st.ImageQuality)
	return s.Store.SaveSettings(st)
}

// APIClient 取当前设置下的上游客户端(代理/画质变更自动重建)。
func (s *Service) APIClient() (*Client, error) {
	st := s.Settings()
	return s.apiPool.Get(st.Proxy + "|" + st.ImageQuality)
}

// ImageHTTPClient 图片下载客户端(tls-client Chrome 指纹;代理变更重建;
// 代理口径与 API 客户端一致:落盘值缺省回退 env,否则直连)。
func (s *Service) ImageHTTPClient() (*Client, error) {
	proxy := s.Settings().Proxy
	s.imgMu.Lock()
	defer s.imgMu.Unlock()
	if s.imgClient == nil || s.imgClientProxy != proxy {
		cl, err := NewClient(proxy, "original")
		if err != nil {
			return nil, err
		}
		s.imgClient = cl
		s.imgClientProxy = proxy
	}
	return s.imgClient, nil
}

// ImageCache 图片落盘缓存单例:<DataDir>/pica/cache,文件数上限读
// PICA_IMAGE_CACHE_LIMIT(默认 500,最小 10)。
func (s *Service) ImageCache() *imgcache.Cache {
	s.imgCacheOnce.Do(func() {
		limit := 500
		if v := os.Getenv("PICA_IMAGE_CACHE_LIMIT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 10 {
				limit = n
			}
		}
		s.imgCache = imgcache.NewCache(filepath.Join(s.Store.Dir(), "cache"), limit)
	})
	return s.imgCache
}

// DefaultDataDir 默认数据目录:<nowen DataDir>/pica。
func DefaultDataDir(baseDataDir string) string {
	return filepath.Join(baseDataDir, "pica")
}
