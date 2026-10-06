package jm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestManager(t *testing.T) (*SessionManager, string) {
	t.Helper()
	dir := t.TempDir()
	return NewSessionManager(dir), dir
}

// Create → Get 往返;Get 命中不续期(剩余充足时)。
func TestSessionCreateGet(t *testing.T) {
	m, dir := newTestManager(t)
	s := m.Create(map[string]string{"AVS": "x"}, map[string]any{"username": "u"}, "proxy")
	if s.Token == "" {
		t.Fatal("token 为空")
	}
	got := m.Get(s.Token)
	if got == nil || got.Token != s.Token {
		t.Fatalf("Get 未命中: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionFile)); err != nil {
		t.Fatalf("会话未落盘: %v", err)
	}
}

// 过期会话:Get 返回 nil 并从表中清除。
func TestSessionExpired(t *testing.T) {
	m, _ := newTestManager(t)
	s := m.Create(nil, nil, "")
	m.mu.Lock()
	s.ExpiresAt = time.Now().Add(-time.Minute)
	m.mu.Unlock()
	if got := m.Get(s.Token); got != nil {
		t.Fatalf("过期会话应返回 nil,得到 %v", got)
	}
	m.mu.Lock()
	_, ok := m.sessions[s.Token]
	m.mu.Unlock()
	if ok {
		t.Fatal("过期会话未从表中清除")
	}
}

// 滑动续期:剩余不足 TTL-24h 时续满;充足时不动。
func TestSessionSlidingRenewal(t *testing.T) {
	m, _ := newTestManager(t)

	// 剩余 2h(< 6d)→ 续满 7d
	s := m.Create(nil, nil, "")
	m.mu.Lock()
	s.ExpiresAt = time.Now().Add(2 * time.Hour)
	m.mu.Unlock()
	got := m.Get(s.Token)
	if until := time.Until(got.ExpiresAt); until < sessionTTL-renewInterval {
		t.Fatalf("应续满 %v,实际剩余 %v", sessionTTL, until)
	}

	// 剩余 6.5d(≥ TTL-24h)→ 保持
	s2 := m.Create(nil, nil, "")
	m.mu.Lock()
	s2.ExpiresAt = time.Now().Add(65 * 24 * time.Hour / 10)
	keep := s2.ExpiresAt
	m.mu.Unlock()
	if got := m.Get(s2.Token); !got.ExpiresAt.Equal(keep) {
		t.Fatalf("剩余充足不应续期: %v → %v", keep, got.ExpiresAt)
	}
}

// 持久化:新建 manager 加载同目录会话;Delete 同步落盘。
func TestSessionPersistence(t *testing.T) {
	m, dir := newTestManager(t)
	s := m.Create(map[string]string{"AVS": "x"}, map[string]any{"username": "u", "level": 3}, "proxy")

	m2 := NewSessionManager(dir)
	got := m2.Get(s.Token)
	if got == nil || got.Cookies["AVS"] != "x" {
		t.Fatalf("重启后应恢复会话,得到 %v", got)
	}
	if got.UserInfo["username"] != "u" || got.UserInfo["level"] != float64(3) {
		t.Fatalf("UserInfo 往返失真: %v", got.UserInfo)
	}

	m2.Delete(s.Token)
	m3 := NewSessionManager(dir)
	if got := m3.Get(s.Token); got != nil {
		t.Fatal("删除后重启不应恢复会话")
	}
}

// 加载时丢弃过期条目;损坏文件按空表处理不 panic。
func TestSessionLoadSanitize(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(dir)
	s := m.Create(nil, nil, "")
	m.mu.Lock()
	s.ExpiresAt = time.Now().Add(-time.Hour)
	m.save()
	m.mu.Unlock()

	if got := NewSessionManager(dir).Get(s.Token); got != nil {
		t.Fatal("过期会话不应被加载")
	}

	if err := os.WriteFile(filepath.Join(dir, sessionFile), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m2 := NewSessionManager(dir)
	if s := m2.Create(nil, nil, ""); m2.Get(s.Token) == nil {
		t.Fatal("损坏文件后应可正常建会话")
	}
}

// 空 dataDir = 仅内存,不写任何文件。
func TestSessionMemoryOnly(t *testing.T) {
	m := NewSessionManager("")
	s := m.Create(nil, nil, "")
	if m.Get(s.Token) == nil {
		t.Fatal("内存模式 Get 应命中")
	}
}
