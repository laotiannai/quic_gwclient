package proto

const PROTO_VERSION uint16 = 1
const MAX_TAG_LEN int = 4
const MAX_SESSION_LEN int = 40
const HEAD_TAG uint32 = 1162693946

// 消息命令字
const (
	EMM_COMMAND_HEART_BEAT uint16 = 1 // 心跳消息，tcp情况下使用, 目前udp版本保留

	EMM_COMMAND_INIT     uint16 = 2 // 链路初始化消息
	EMM_COMMAND_INIT_ACK uint16 = 3 // 链路初始化应答
	EMM_COMMAND_AUTH     uint16 = 4 // 网关认证请求，拉取转发配置
	EMM_COMMAND_AUTH_ACK uint16 = 5 // 认证请求应答
	EMM_COMMAND_TRAN     uint16 = 6 // 透传请求
	EMM_COMMAND_TRAN_ACK uint16 = 7 // 透传应答

	EMM_COMMAND_LINK_CLOSE          uint16 = 200 // 断开链路消息
	EMM_COMMAND_LINK_CLOSE_ACK      uint16 = 201 // 断开链路消息
	EMM_COMMAND_LINK_HEART_BEAT     uint16 = 202 // 链路心跳消息
	EMM_COMMAND_LINK_HEART_BEAT_ACK uint16 = 203 // 链路心跳应答消息
)

// 协议类型
const (
	PROTO_TYPE_TCP   int = 0x01 //TCP协议
	PROTO_TYPE_HTTP  int = 0x02 //HTTP协议
	PROTO_TYPE_HTTPS int = 0x03 //HTTPS协议
	PROTO_TYPE_RTSP  int = 0x04 //RTSP协议
	PROTO_TYPE_SIP   int = 0x05 //SIP协议
	PROTO_TYPE_RTP   int = 0x06 //RTP协议
	PROTO_TYPE_RTCP  int = 0x07 //RTCP协议
	PROTO_TYPE_H263  int = 0x08 //H.263协议
	PROTO_TYPE_UDP   int = 0x20 //UDP协议
	PROTO_TYPE_TEST  int = 0x30 //用于压测
)

// 数据协议类型
const (
	DATA_PROTO_TYPE_BINARY uint8 = 0x00 // 二进制协议
	DATA_PROTO_TYPE_JSON   uint8 = 0x01 // JSON协议
)

// 认证状态码
const (
	AUTH_STATUS_CODE_SUCCESS               uint16 = 8002
	AUTH_STATUS_CODE_ERR_TENNEL_FORBIDDEN  uint16 = 8001
	AUTH_STATUS_CODE_ERR_SESSION_NOT_EXIST uint16 = 8003
	AUTH_STATUS_CODE_ERR_OVER_FLOW_LIMIT   uint16 = 8004
	AUTH_STATUS_CODE_ERR_USER_FORBIDDEN    uint16 = 8007
	AUTH_STATUS_CODE_ERR_DEVICE_FORBIDDEN  uint16 = 8008
	AUTH_STATUS_CODE_ERR_CONN_FAILED       uint16 = 8020
	AUTH_STATUS_CODE_ERR_UNKNOW            uint16 = 8099
)

// InitInfo 初始化信息结构体
type InitInfo struct {
	Serverid     int    `json:"serverid"`     // 服务器ID
	ProtocolType int    `json:"protocolType"` // 协议类型
	Appname      string `json:"appname"`      // 应用名称
	RequesetId   string `json:"requestId"`    // 请求ID
	Sessionid    string `json:"sessionId"`    // 会话ID
}
