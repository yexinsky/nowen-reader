// Package namesafe 在线源下载文件名清洗(JM/Pica 等在线源共用)。
// 口径移植 JMComic-qt ToolUtil.GetCanSaveName,从 internal/jm 原样抽移(2026-10)。
package namesafe

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxNameRunes 单级名称字符数上限:254//3-1 = 83。
// 依据为 UTF-8 下 CJK 单字 3 字节,83×3=249 ≤ 255 的文件名字节上限。
const MaxNameRunes = 254/3 - 1

// MaxFullPathLen 全路径字节预算:Windows MAX_PATH=260,预留扩展名/同名序号余量。
const MaxFullPathLen = 240

// illegalNameChars Windows 非法字符 + \0\t\r\n(JMComic-qt 同款字符类)。
// 目录分隔符一并删除,保证清洗结果恒为单级名称,不会逃出目标目录。
var illegalNameChars = regexp.MustCompile("[\\\\/:*?\"<>|\x00\t\r\n]")

// SanitizeName 清洗单级路径名(移植 JMComic-qt ToolUtil.GetCanSaveName):
// 删非法字符 → 去尾点 → 去首尾空格 → 截断 83 字符 → 再去尾点/首尾空格。
func SanitizeName(name string) string {
	s := illegalNameChars.ReplaceAllString(name, "")
	s = strings.TrimRight(s, ".")
	s = strings.Trim(s, " ")
	if r := []rune(s); len(r) > MaxNameRunes {
		s = string(r[:MaxNameRunes])
	}
	return strings.Trim(strings.TrimRight(s, "."), " ")
}

// SafeName 清洗并保证非空:清洗结果为空时回退 fallback(通常为 aid/pid),
// 仍为空回退 "untitled"。JMComic-qt 无此兜底,空名会直接导致写盘失败。
func SafeName(name, fallback string) string {
	if s := SanitizeName(name); s != "" {
		return s
	}
	if s := SanitizeName(fallback); s != "" {
		return s
	}
	return "untitled"
}

// FitPathName 按目标目录收紧名称:保证 dir/name 的全路径字节数不超过
// MaxFullPathLen(Windows MAX_PATH 兜底;CJK 按 UTF-8 字节累计,估算偏保守)。
func FitPathName(dir, name string, reserveBytes int) string {
	avail := MaxFullPathLen - len(dir) - 1 - reserveBytes
	if avail < 16 {
		avail = 16 // 目录本身过长时保底可读性,写盘失败会以「归档失败」显式上报
	}
	if len(name) <= avail {
		return name
	}
	var b strings.Builder
	used := 0
	for _, r := range name {
		size := utf8.RuneLen(r)
		if used+size > avail {
			break
		}
		b.WriteRune(r)
		used += size
	}
	return SafeName(b.String(), "untitled")
}
