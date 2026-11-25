package handler

// 在线源强制代理的 handler 层守卫(规则见 internal/proxyguard 与 docs/CONFIGURATION.md):
// 设置接口不接受空代理/非法代理,旧版「清除(直连)」语义已取消。

import (
	"strings"
	"testing"
)

// TestValidateProxyInput 两个在线源的设置接口共用同一段校验:
// 空值/非法值一律拒绝,合法值去掉首尾空白后写入。
func TestValidateProxyInput(t *testing.T) {
	bad := []string{"", "   ", "127.0.0.1:10809", "http://", "ftp://127.0.0.1:21"}
	for _, raw := range bad {
		if _, detail := validateProxyInput(raw); detail == "" {
			t.Fatalf("%q 应被拒绝并给出提示", raw)
		}
	}
	good := map[string]string{
		"http://127.0.0.1:10809":        "http://127.0.0.1:10809",
		"  socks5://192.168.5.196:1070 ": "socks5://192.168.5.196:1070",
		"socks5h://192.168.5.196:1070":   "socks5h://192.168.5.196:1070",
	}
	for raw, want := range good {
		got, detail := validateProxyInput(raw)
		if detail != "" {
			t.Fatalf("%q 应通过校验,got %q", raw, detail)
		}
		if got != want {
			t.Fatalf("%q 归一化应为 %q,got %q", raw, want, got)
		}
	}
}

// TestProxyRejectionMessagesAreActionable 拒绝时的文案必须指向「去填代理」,
// 而不是笼统的网络错误 —— 这是不让用户困惑的关键。
func TestProxyRejectionMessagesAreActionable(t *testing.T) {
	for _, raw := range []string{"", "bogus"} {
		_, detail := validateProxyInput(raw)
		if !strings.Contains(detail, "代理") {
			t.Fatalf("%q 的提示不够可操作: %q", raw, detail)
		}
	}
}
