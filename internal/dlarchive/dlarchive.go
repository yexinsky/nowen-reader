// Package dlarchive 在线源下载归档落盘(JM / Pica 共用)。
//
// 归档口径统一为「目录/名.zip」:同一作品重复下载只保留一份文件,书库按
// relativePath 唯一定位到同一条记录,不再产生「名 (2).zip」这类同内容副本。
// 是否允许覆盖由 HTTP 层先预检(Exists)再显式确认(overwrite),避免静默丢数据。
package dlarchive

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Ext 归档扩展名(书库扫描按 archive 类型识别)。
const Ext = ".zip"

// Zip 归档目标路径:dir/base.zip(不做重名递增)。
func Zip(dir, base string) string {
	return filepath.Join(dir, base+Ext)
}

// Exists 归档是否已存在(建任务前的重复下载预检)。
func Exists(dir, base string) bool {
	info, err := os.Stat(Zip(dir, base))
	return err == nil && !info.IsDir()
}

// Replace 用 src 原子替换 dst:
// 先把 src 拷贝到 dst 同目录的临时文件(跨盘也成立),再 rename 覆盖 dst;
// 成功删除 src,失败清理临时文件并保留 dst 原内容(调用方据错判失败)。
func Replace(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp-" + randomSuffix()
	if err := copyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	// 归档已成:dst 内容即最新,临时源可以丢弃(删不掉不算失败,沙箱随后整体清理)
	_ = os.Remove(src)
	return nil
}

// copyFile 拷贝并落盘(src 句柄在返回前关闭,Windows 下否则无法删除源文件)。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return in.Close()
}

// randomSuffix 临时文件后缀(同目录并发任务互不撞名)。
func randomSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", os.Getpid())
	}
	return hex.EncodeToString(b[:])
}
