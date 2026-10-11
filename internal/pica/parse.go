package pica

// 查询参数解析(handler 层入口校验)。

import "strconv"

// ParsePageQuery 分页解析:空 → 1;非正整数 → 非法。
func ParsePageQuery(raw string) (int, bool) {
	if raw == "" {
		return 1, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// ParseIntInRange 带 range 校验的整数解析。
func ParseIntInRange(raw string, min, max int) (int, bool) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, false
	}
	return n, true
}

// ValidImageQuality 画质设置白名单。
func ValidImageQuality(q string) bool { return imageQualityValues[q] }
