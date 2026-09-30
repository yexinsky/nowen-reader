package jm

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// md5Hex 返回小写 hex 的 MD5(SDK JmCryptoTool.md5hex)。
func md5Hex(key string) string {
	sum := md5.Sum([]byte(key))
	return hex.EncodeToString(sum[:])
}

// tokenAndTokenparam 生成请求头(SDK jm_toolkit.py:952-979):
//
//	tokenparam = "{ts},{ver}"
//	token     = md5hex("{ts}{secret}")
//
// secret 通常为 appTokenSecret;/chapter_view_template 用 appTokenSecret2。
func tokenAndTokenparam(ts int64, ver, secret string) (string, string) {
	if ver == "" {
		ver = defaultAppVersion
	}
	if secret == "" {
		secret = appTokenSecret
	}
	tokenparam := strconv.FormatInt(ts, 10) + "," + ver
	token := md5Hex(strconv.FormatInt(ts, 10) + secret)
	return token, tokenparam
}

// decodeRespData 解密响应 data 字段(SDK jm_toolkit.py:981-1013):
// base64 → AES-256-ECB(key = md5hex("{tsPrefix}{secret}") 的 32 字节 ASCII)→ 去 PKCS7 → UTF-8。
// 常规响应 tsPrefix = 请求 ts 的十进制串;域名更新服务 tsPrefix = ""(SDK req_api_domain_server)。
// Go 标准库无 ECB,按块逐一解密实现。
func decodeRespData(dataB64 string, tsPrefix, secret string) ([]byte, error) {
	if secret == "" {
		secret = appDataSecret
	}
	cipherB64, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %w", err)
	}
	if len(cipherB64) == 0 || len(cipherB64)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("密文长度非法: %d", len(cipherB64))
	}
	key := []byte(md5Hex(tsPrefix + secret))
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("AES 初始化失败: %w", err)
	}
	plain := make([]byte, len(cipherB64))
	for off := 0; off < len(cipherB64); off += aes.BlockSize {
		block.Decrypt(plain[off:off+aes.BlockSize], cipherB64[off:off+aes.BlockSize])
	}
	// 去 PKCS7 填充(SDK:data[:-data[-1]])
	pad := int(plain[len(plain)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(plain) {
		return nil, fmt.Errorf("PKCS7 填充非法: %d", pad)
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			return nil, fmt.Errorf("PKCS7 填充不一致")
		}
	}
	return plain[:len(plain)-pad], nil
}

// encodeRespData 与 decodeRespData 对偶,仅供测试生成向量(Python 对照用)。
func encodeRespData(plaintext []byte, tsPrefix, secret string) (string, error) {
	if secret == "" {
		secret = appDataSecret
	}
	key := []byte(md5Hex(tsPrefix + secret))
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	pad := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := make([]byte, 0, len(plaintext)+pad)
	padded = append(padded, plaintext...)
	padded = append(padded, bytes.Repeat([]byte{byte(pad)}, pad)...)
	cipher := make([]byte, len(padded))
	for off := 0; off < len(padded); off += aes.BlockSize {
		block.Encrypt(cipher[off:off+aes.BlockSize], padded[off:off+aes.BlockSize])
	}
	return base64.StdEncoding.EncodeToString(cipher), nil
}

// timeStamp 秒级时间戳(SDK common/util/time_util.time_stamp)。
func timeStamp() int64 { return time.Now().Unix() }

// sha256Hex 供会话 token 等场景使用。
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
