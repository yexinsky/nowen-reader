package jm

import "strconv"

// ParsePageQuery 解析分页参数(契约 §2.4:page int 默认 1,≥1,非法 → 422)。
// 返回 (page, valid);raw 为空 → (1, true)。
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

// ParsePageQueryInRange 带 range 校验的分页解析(如 day 1..7)。
func ParseIntInRange(raw string, min, max int) (int, bool) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, false
	}
	return n, true
}
