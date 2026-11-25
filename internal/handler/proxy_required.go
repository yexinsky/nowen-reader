package handler

// 在线源强制代理的公共校验(规则见 internal/proxyguard 与 docs/CONFIGURATION.md)。
//
// JM 与哔咔的 PUT /settings 都走这里:不接受空代理,也不接受格式不对的地址。
// 返回归一化后的地址与给终端用户的提示(提示为空表示通过)。

import (
	"strings"

	"github.com/nowen-reader/nowen-reader/internal/proxyguard"
)

// validateProxyInput 校验并归一化用户输入的代理地址。
func validateProxyInput(raw string) (proxy string, detail string) {
	if err := proxyguard.Validate(raw); err != nil {
		return "", proxyguard.Message(err)
	}
	return strings.TrimSpace(raw), ""
}
