package proxyguard

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name  string
		proxy string
		want  error
	}{
		{"空串视为未配置", "", ErrMissing},
		{"纯空白视为未配置", "   ", ErrMissing},
		{"http 合法", "http://127.0.0.1:10809", nil},
		{"socks5 合法", "socks5://192.168.5.196:1070", nil},
		{"socks5h 合法", " socks5h://192.168.5.196:1070 ", nil},
		{"https 合法", "https://proxy.example.com:8443", nil},
		{"缺 scheme", "127.0.0.1:10809", ErrInvalid},
		{"缺 host", "http://", ErrInvalid},
		{"不支持的协议", "ftp://127.0.0.1:21", ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.proxy)
			switch {
			case tc.want == nil:
				if err != nil {
					t.Fatalf("期望通过,实际 %v", err)
				}
			case errors.Is(tc.want, ErrMissing):
				if !errors.Is(err, ErrMissing) {
					t.Fatalf("期望 ErrMissing,实际 %v", err)
				}
			default:
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("期望 ErrInvalid,实际 %v", err)
				}
			}
		})
	}
}

func TestResolveIgnoresBlankAndFallsBack(t *testing.T) {
	// 落盘值为空(旧版「清除直连」留下的空串)不再表示直连,应继续回退
	if got := Resolve("", "", "http://127.0.0.1:10809"); got != "http://127.0.0.1:10809" {
		t.Fatalf("空落盘值应回退默认值,实际 %q", got)
	}
	if got := Resolve("   ", "socks5://192.168.5.196:1070", "http://127.0.0.1:10809"); got != "socks5://192.168.5.196:1070" {
		t.Fatalf("空白落盘值应回退 env,实际 %q", got)
	}
	if got := Resolve("http://a:1", "http://b:2", "http://c:3"); got != "http://a:1" {
		t.Fatalf("落盘值优先,实际 %q", got)
	}
	// 三者皆空:返回空串,由调用方判为未配置(绝不表示直连)
	if got := Resolve("", "", ""); got != "" {
		t.Fatalf("全空应返回空串,实际 %q", got)
	}
}

// TestBlockedTransportSurfacesAsIs 模拟各模块客户端的拦截点:
// transport.Proxy 返回错误时,net/http 会把它包进 *url.Error,必须能被 errors.Is 识别。
func TestBlockedTransportSurfacesAsIs(t *testing.T) {
	transport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, ErrMissing },
	}
	client := &http.Client{Transport: transport}
	req, err := http.NewRequest("GET", "https://example.invalid/x", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatal("未配置代理时必须失败")
	}
	if !IsBlocked(err) {
		t.Fatalf("应被识别为代理拦截,实际 %v", err)
	}
	if msg := Message(err); msg == "" {
		t.Fatal("拦截错误必须有可操作提示")
	}
}

func TestIsBlockedRejectsOrdinaryErrors(t *testing.T) {
	if IsBlocked(errors.New("connection refused")) {
		t.Fatal("普通网络错误不应判为代理拦截")
	}
	if IsBlocked(nil) {
		t.Fatal("nil 不应判为代理拦截")
	}
}
