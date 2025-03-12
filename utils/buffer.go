package utils

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"unsafe"
)

// Buffer 缓存
type Buffer struct {
	*bytes.Buffer
}

// NewBuffer 新建缓存
func NewBuffer(buf []byte) *Buffer {
	return &Buffer{
		Buffer: bytes.NewBuffer(buf),
	}
}

// NewEmptyBuffer 新建空缓存
func NewEmptyBuffer() *Buffer {
	return &Buffer{
		Buffer: &bytes.Buffer{},
	}
}

// Length 获取长度
func (b Buffer) Length() int {
	return b.Buffer.Len()
}

// Bytes 获取字节片段
func (b Buffer) Bytes() []byte {
	return b.Buffer.Bytes()
}

// ReadN 读取字节片段
func (b *Buffer) ReadN(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := b.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ReadBufferN 读取字节片段到指定缓冲区
func (b *Buffer) ReadBufferN(buf []byte, n int) error {
	if _, err := b.Read(buf); err != nil {
		return err
	}
	return nil
}

// WriteBytes 写入字节片段
func (b *Buffer) WriteBytes(data []byte) (int, error) {
	return b.Write(data)
}

// ReadUint8 读取uint8
func (b *Buffer) ReadUint8() (uint8, error) {
	return b.ReadByte()
}

// WriteUint8 写入uint8
func (b *Buffer) WriteUint8(data uint8) error {
	return b.WriteByte(data)
}

// ReadUint16 读取uint16
func (b *Buffer) ReadUint16() (uint16, error) {
	buf, err := b.ReadN(2)
	if err != nil {
		return 0, err
	}
	u16 := binary.BigEndian.Uint16(buf)
	return u16, nil
}

// ReadLittleEndianUint16 读取LittleEndian uint16
func (b *Buffer) ReadLittleEndianUint16() (uint16, error) {
	buf, err := b.ReadN(2)
	if err != nil {
		return 0, err
	}
	u16 := binary.LittleEndian.Uint16(buf)
	return u16, nil
}

// WriteUint16 写入uint16
func (b *Buffer) WriteUint16(data uint16) (int, error) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, data)
	return b.Write(buf)
}

// ReadUint32 读取uint32
func (b *Buffer) ReadUint32() (uint32, error) {
	buf, err := b.ReadN(4)
	if err != nil {
		return 0, err
	}
	u32 := binary.BigEndian.Uint32(buf)
	return u32, nil
}

// ReadLittleEndianUint32 读取LittleEndian uint32
func (b *Buffer) ReadLittleEndianUint32() (uint32, error) {
	buf, err := b.ReadN(4)
	if err != nil {
		return 0, err
	}
	u32 := binary.LittleEndian.Uint32(buf)
	return u32, nil
}

// WriteUint32 写入uint32
func (b *Buffer) WriteUint32(data uint32) (int, error) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, data)
	return b.Write(buf)
}

// ReadUint64 读取uint64
func (b *Buffer) ReadUint64() (uint64, error) {
	buf, err := b.ReadN(8)
	if err != nil {
		return 0, err
	}
	u64 := binary.BigEndian.Uint64(buf)
	return u64, nil
}

// ReadLittleEndianUint64 读取LittleEndian uint64
func (b *Buffer) ReadLittleEndianUint64() (uint64, error) {
	buf, err := b.ReadN(8)
	if err != nil {
		return 0, err
	}
	u64 := binary.LittleEndian.Uint64(buf)
	return u64, nil
}

// WriteUint64 写入uint64
func (b *Buffer) WriteUint64(data uint64) (int, error) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, data)
	return b.Write(buf)
}

// ReadBCD 读取BCD片段
func (b *Buffer) ReadBCD(n int) (string, error) {
	buf, err := b.ReadN(n)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// WriteBCD 写入BCD
func (b *Buffer) WriteBCD(data string) (int, error) {
	d, err := hex.DecodeString(data)
	if err != nil {
		return 0, err
	}
	return b.Write(d)
}

// ReadNString 读取字符串
func (b *Buffer) ReadNString(n int) (string, error) {
	buf, err := b.ReadN(n)
	if err != nil {
		return "", err
	}
	return *(*string)(unsafe.Pointer(&buf)), nil
}

// WriteString 写字符串
func (b *Buffer) WriteString(str string) (int, error) {
	return b.Write(*(*[]byte)(unsafe.Pointer(&str)))
}

// ReadToSeparator 读取到分割符
func (b *Buffer) ReadToSeparator(separator byte) ([]byte, error) {
	d, err := b.Buffer.ReadBytes(separator)
	if err != nil {
		return nil, err
	}
	return d[:len(d)-1], nil
}

// ReadStringToSeparator 读取字符串到分割符
func (b *Buffer) ReadStringToSeparator(separator byte) (string, error) {
	d, err := b.ReadToSeparator(separator)
	if err != nil {
		return "", err
	}
	return *(*string)(unsafe.Pointer(&d)), nil
} 