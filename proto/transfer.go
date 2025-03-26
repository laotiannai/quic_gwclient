package proto

import (
	"github.com/laotiannai/quic_gwclient/utils"
)

const REQUEST_HEAD_LEN int = 20
const RESPONSE_HEAD_LEN int = 20

type TransferHeader struct {
	Tag       uint32 // 包头标志："EMM:"ASCII码值，69，77，77，58
	Version   uint16 // 版本号
	Command   uint16 // 命令字
	ProtoType uint8  // 协议类型, ProtoTypeV2枚举定义
	Option    uint8  // 保留字段
	Reserve   uint16 // 可选配置项
	DataLen   uint32 // 数据长度
	Crc       uint32 // crc校验码
}

func (t *TransferHeader) Len() int {
	return REQUEST_HEAD_LEN
}

// 序列化数据包
func (t *TransferHeader) Marshal() ([]byte, error) {
	buf := utils.NewEmptyBuffer()
	_, err := buf.WriteUint32(t.Tag)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint16(t.Version)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint16(t.Command)
	if err != nil {
		return nil, err
	}

	err = buf.WriteUint8(t.ProtoType)
	if err != nil {
		return nil, err
	}

	err = buf.WriteUint8(t.Option)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint16(t.Reserve)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint32(t.DataLen)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint32(t.Crc)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), err
}

// 读取数据包
func (t *TransferHeader) UnMarshal(data []byte) error {
	buf := utils.NewBuffer(data)

	t.Tag, _ = buf.ReadUint32()
	t.Version, _ = buf.ReadUint16()
	t.Command, _ = buf.ReadUint16()
	t.ProtoType, _ = buf.ReadUint8()
	t.Option, _ = buf.ReadUint8()
	t.Reserve, _ = buf.ReadUint16()
	t.DataLen, _ = buf.ReadUint32()
	t.Crc, _ = buf.ReadUint32()

	return nil
}

type UdpMessage struct {
	Head TransferHeader // 消息头
	Body []byte         // 消息体
}

func (a *UdpMessage) ParseHead(buf []byte) error {
	err := a.Head.UnMarshal(buf)
	return err
}

func (a *UdpMessage) ParseBody(buf []byte, bodylen int) error {
	if bodylen <= 0 || len(buf) <= a.Head.Len() {
		return nil
	}

	a.Body = make([]byte, bodylen)
	copy(a.Body, buf[a.Head.Len():])
	return nil
}

func (a *UdpMessage) Marshal() ([]byte, error) {
	msglen := a.Head.Len() + len(a.Body)
	buffer := make([]byte, msglen)
	headbuf, _ := a.Head.Marshal()
	copy(buffer, headbuf)
	if len(a.Body) > 0 {
		copy(buffer[a.Head.Len():], a.Body)
	}
	return buffer, nil
}

func (a *UdpMessage) Len() int {
	return a.Head.Len() + int(a.Head.DataLen)
}

type ResponseHeader struct {
	Tag       uint32 // 包头标志："EMM:"ASCII码值，69，77，77，58
	Version   uint16 // 版本号
	Command   uint16 // 命令字
	Result    uint16 // 结果值
	Option    uint8  // 可选配置
	Reserve   uint8  // 保留字段
	DataLen   uint32 // 数据长度
	OriginLen uint32 // 原始数据长度
}

func (t *ResponseHeader) Len() int {
	return RESPONSE_HEAD_LEN
}

// 序列化数据包
func (t *ResponseHeader) Marshal() ([]byte, error) {
	buf := utils.NewEmptyBuffer()
	_, err := buf.WriteUint32(t.Tag)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint16(t.Version)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint16(t.Command)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint16(t.Result)
	if err != nil {
		return nil, err
	}

	err = buf.WriteUint8(t.Option)
	if err != nil {
		return nil, err
	}

	err = buf.WriteUint8(t.Reserve)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint32(t.DataLen)
	if err != nil {
		return nil, err
	}

	_, err = buf.WriteUint32(t.OriginLen)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), err
}

// 读取数据包
func (t *ResponseHeader) UnMarshal(data []byte) error {
	buf := utils.NewBuffer(data)

	t.Tag, _ = buf.ReadUint32()
	t.Version, _ = buf.ReadUint16()
	t.Command, _ = buf.ReadUint16()
	t.Result, _ = buf.ReadUint16()
	t.Option, _ = buf.ReadUint8()
	t.Reserve, _ = buf.ReadUint8()
	t.DataLen, _ = buf.ReadUint32()
	t.OriginLen, _ = buf.ReadUint32()

	return nil
}

type UdpResponseMessage struct {
	Head ResponseHeader // 消息头
	Body []byte         // 消息体
}

func (a *UdpResponseMessage) ParseHead(buf []byte) error {
	err := a.Head.UnMarshal(buf)
	return err
}

func (a *UdpResponseMessage) ParseBody(buf []byte, bodylen int) error {
	if bodylen <= 0 || len(buf) <= a.Head.Len() {
		return nil
	}

	a.Body = make([]byte, bodylen)
	copy(a.Body, buf[a.Head.Len():])
	return nil
}

func (a *UdpResponseMessage) Marshal() ([]byte, error) {
	msglen := a.Head.Len() + len(a.Body)
	buffer := make([]byte, msglen)
	headbuf, _ := a.Head.Marshal()
	copy(buffer, headbuf)
	if len(a.Body) > 0 {
		copy(buffer[a.Head.Len():], a.Body)
	}
	return buffer, nil
}

func (a *UdpResponseMessage) Len() int {
	return a.Head.Len() + int(a.Head.DataLen)
}
