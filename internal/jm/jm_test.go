package jm

import (
	"encoding/json"
	"fmt"
	"testing"
)

// 向量由本机 Python 生成(hashlib.md5 + pycryptodome AES-ECB),
// 固定 ts=1700000000,确保 Go 实现与 jmcomic SDK 逐字节一致。
const (
	vectorTS       = 1700000000
	vectorTSPrefix = "1700000000"
	vectorToken    = "1c6fa345eea2e10d5a30880ec2a7e0b3"
	vectorPlain    = `{"code":200,"data":{"uid":123,"username":"tester"}}`
	vectorDataB64  = "ra69+FcchNY4IjR5OfVrP+QV/bfuEy/kRSEHoz5MrTKIX46CB6uQvztWa1pbZNvk9y+7AnDe/PAeMAD4S0e4WA=="
)

func TestTokenAndTokenparam(t *testing.T) {
	token, tokenparam := tokenAndTokenparam(vectorTS, "", "")
	if token != vectorToken {
		t.Fatalf("token = %s, want %s", token, vectorToken)
	}
	if tokenparam != "1700000000,2.0.19" {
		t.Fatalf("tokenparam = %s", tokenparam)
	}
	// /chapter_view_template 专用密钥
	token2, _ := tokenAndTokenparam(vectorTS, "", appTokenSecret2)
	if token2 == vectorToken {
		t.Fatalf("secret2 应产生不同 token")
	}
}

func TestDecodeRespData(t *testing.T) {
	decoded, err := decodeRespData(vectorDataB64, vectorTSPrefix, "")
	if err != nil {
		t.Fatalf("decodeRespData: %v", err)
	}
	if string(decoded) != vectorPlain {
		t.Fatalf("decoded = %s, want %s", decoded, vectorPlain)
	}
}

func TestDecodeEncodeRoundTrip(t *testing.T) {
	plain := []byte(`{"ok":true,"list":[1,2,3],"name":"测试中文"}`)
	b64, err := encodeRespData(plain, vectorTSPrefix, "")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeRespData(b64, vectorTSPrefix, "")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var want, have map[string]any
	_ = json.Unmarshal(plain, &want)
	_ = json.Unmarshal(got, &have)
	if len(want) != len(have) {
		t.Fatalf("round trip mismatch: %s vs %s", plain, got)
	}
}

func TestGetNum(t *testing.T) {
	// aid < scramble_id → 0(无需乱序)
	if got := getNum(220980, 100, "00001.jpg"); got != 0 {
		t.Fatalf("getNum(220980,100) = %d, want 0", got)
	}
	// aid < 268850 → 10
	if got := getNum(0, 268849, "00001.jpg"); got != 10 {
		t.Fatalf("getNum(0,268849) = %d, want 10", got)
	}
	// 高阈值段 x=8:独立按公式推导对照
	ref := func(scrambleID, aid int64, filename string) int {
		s := md5Hex(fmt.Sprintf("%d%s", aid, filename))
		x := 10
		if aid >= 421926 {
			x = 8
		}
		return int(s[len(s)-1]) % x * 2 + 2
	}
	if got := getNum(0, 421925, "00001.jpg"); got != ref(0, 421925, "00001.jpg") {
		t.Fatalf("getNum(0,421925) = %d, want %d", getNum(0, 421925, "00001.jpg"), ref(0, 421925, "00001.jpg"))
	}
	if got := getNum(0, 421926, "00001.jpg"); got != ref(0, 421926, "00001.jpg") {
		t.Fatalf("阈值边界 421926 应取 x=8")
	}
}

func TestBearerToken(t *testing.T) {
	if got := BearerToken("Bearer abc123"); got != "abc123" {
		t.Fatalf("Bearer abc123 -> %q", got)
	}
	if got := BearerToken("bearer abc123"); got != "abc123" {
		t.Fatalf("bearer abc123 -> %q", got)
	}
	if got := BearerToken("Basic abc"); got != "" {
		t.Fatalf("Basic abc -> %q", got)
	}
}

func TestClassifyUpstreamError(t *testing.T) {
	if e := classifyUpstreamError(&APIError{Code: CodeNotFound, Msg: "x"}, ""); e.Code != CodeNotFound {
		t.Fatalf("APIError 直通失败")
	}
	if e := classifyUpstreamError(errNotFound(""), ""); e.Code != CodeNotFound {
		t.Fatalf("NotFound 失败")
	}
	if e := classifyUpstreamError(errNotFound(""), ""); e.Code != CodeNotFound {
		t.Fatal("not found passthrough failed")
	}
	if e := classifyUpstreamError(discardError("Connection refused"), ""); e.Code != CodeNetwork {
		t.Fatalf("网络关键字应映射 2002, got %d", e.Code)
	}
	if e := classifyUpstreamError(discardError("something weird"), ""); e.Code != CodeUpstream {
		t.Fatalf("兜底应映射 2001, got %d", e.Code)
	}
}

type discardError string

func (e discardError) Error() string { return string(e) }
