package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
)

var InitKey string = "thiS2023uDpPw$1921#*&Redsdlfkshg"
var kAesDecryptInputSizeError error = errors.New("crypto/aes: input not full block")

// NewKey 生成新的密钥
func NewKey(requestid string, timestamp int64) string {
	newkey := fmt.Sprintf("%s:#EMM:%d:@2023*leagsoft", requestid, timestamp)
	log.Printf("生成密钥原始字符串: %s", newkey)
	newkey = MD5(newkey)
	log.Printf("生成密钥MD5结果: %s", newkey)
	return newkey
}

// MD5 计算MD5值
func MD5(input string) string {
	data := []byte(input)
	hash := md5.Sum(data)
	result := hex.EncodeToString(hash[:])
	log.Printf("MD5计算 - 输入: %s, 结果: %s", input, result)
	return result
}

// EncryptAES AES加密
func EncryptAES(key []byte, plaintext []byte) ([]byte, error) {
	log.Printf("AES加密 - 密钥长度: %d, 明文长度: %d", len(key), len(plaintext))
	log.Printf("AES加密 - 密钥: %X", key)
	log.Printf("AES加密 - 明文(前50字节): %X", plaintext[:min(50, len(plaintext))])

	ptlen := len(plaintext)
	blocksize := 0
	if ptlen%aes.BlockSize > 0 {
		blocksize = ptlen/aes.BlockSize + 1
	} else {
		blocksize = ptlen / aes.BlockSize
	}
	totallen := blocksize * aes.BlockSize
	log.Printf("AES加密 - 块大小: %d, 总长度: %d", blocksize, totallen)

	plaintextNew := make([]byte, totallen)
	copy(plaintextNew[:], plaintext)
	log.Printf("AES加密 - 填充后明文长度: %d", len(plaintextNew))

	// 使用MD5处理密钥，确保长度为16字节
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		md5Key := md5.Sum(key)
		key = md5Key[:]
		log.Printf("AES加密 - 密钥长度不符合要求，使用MD5处理后: %X", key)
	}

	c, err := aes.NewCipher(key)
	if err != nil {
		log.Printf("AES加密 - 创建Cipher失败: %v", err)
		return nil, err
	}
	log.Printf("AES加密 - 创建Cipher成功，块大小: %d", c.BlockSize())

	iv := make([]byte, aes.BlockSize)
	log.Printf("AES加密 - 使用零IV: %X", iv)

	encrypter := cipher.NewCBCEncrypter(c, iv)
	log.Printf("AES加密 - 创建CBC加密器成功")

	data := make([]byte, len(plaintextNew))
	encrypter.CryptBlocks(data, plaintextNew)
	log.Printf("AES加密 - 加密完成，密文长度: %d", len(data))
	log.Printf("AES加密 - 密文(前50字节): %X", data[:min(50, len(data))])

	return data, nil
}

// DecryptAES AES解密
func DecryptAES(key []byte, ciphertext []byte) ([]byte, error) {
	log.Printf("AES解密 - 密钥长度: %d, 密文长度: %d", len(key), len(ciphertext))
	log.Printf("AES解密 - 密钥: %X", key)
	log.Printf("AES解密 - 密文(前50字节): %X", ciphertext[:min(50, len(ciphertext))])

	// 使用MD5处理密钥，确保长度为16字节
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		md5Key := md5.Sum(key)
		key = md5Key[:]
		log.Printf("AES解密 - 密钥长度不符合要求，使用MD5处理后: %X", key)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		log.Printf("AES解密 - 创建Cipher失败: %v", err)
		return nil, err
	}
	log.Printf("AES解密 - 创建Cipher成功，块大小: %d", block.BlockSize())

	if len(ciphertext) < aes.BlockSize {
		log.Printf("AES解密 - 密文长度小于块大小: %d < %d", len(ciphertext), aes.BlockSize)
		return nil, kAesDecryptInputSizeError
	}

	iv := make([]byte, aes.BlockSize)
	log.Printf("AES解密 - 使用零IV: %X", iv)

	mode := cipher.NewCBCDecrypter(block, iv)
	log.Printf("AES解密 - 创建CBC解密器成功")

	decodeBytes := make([]byte, len(ciphertext))
	mode.CryptBlocks(decodeBytes, ciphertext)
	log.Printf("AES解密 - 解密完成，明文长度: %d", len(decodeBytes))
	log.Printf("AES解密 - 明文(前50字节): %X", decodeBytes[:min(50, len(decodeBytes))])

	// 尝试去除填充
	paddingLen := int(decodeBytes[len(decodeBytes)-1])
	if paddingLen > 0 && paddingLen <= aes.BlockSize {
		log.Printf("AES解密 - 检测到填充长度: %d", paddingLen)
		decodeBytes = decodeBytes[:len(decodeBytes)-paddingLen]
		log.Printf("AES解密 - 去除填充后明文长度: %d", len(decodeBytes))
	}

	return decodeBytes, nil
}

// 辅助函数，返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
