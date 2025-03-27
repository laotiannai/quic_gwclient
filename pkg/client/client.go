package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/laotiannai/quic_gwclient/proto"
	"github.com/laotiannai/quic_gwclient/utils"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
)

// TransferClient QUIC传输客户端
type TransferClient struct {
	conn       quic.Connection
	stream     quic.Stream
	serverAddr string
	config     *Config
	mu         sync.Mutex // 添加互斥锁
}

// Config 客户端配置
type Config struct {
	ServerID   int
	ServerName string
	SessionID  string
	// 重试配置
	MaxRetries    int           // 最大重试次数，默认10次
	RetryDelay    time.Duration // 重试延迟时间，默认500ms
	RetryInterval time.Duration // 重试间隔时间，默认2s
}

// NewTransferClient 创建新的传输客户端
func NewTransferClient(serverAddr string, config *Config) *TransferClient {
	// 设置默认值
	if config.MaxRetries <= 0 {
		config.MaxRetries = 10
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 500 * time.Millisecond
	}
	if config.RetryInterval <= 0 {
		config.RetryInterval = 2 * time.Second
	}

	return &TransferClient{
		serverAddr: serverAddr,
		config:     config,
	}
}

// Connect 连接服务器
func (c *TransferClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 解析服务器地址
	host, port, err := net.SplitHostPort(c.serverAddr)
	if err != nil {
		return fmt.Errorf("解析服务器地址失败: %v", err)
	}

	// 检查网络连接
	if err := checkNetworkConnectivity(host, port); err != nil {
		if err := checkUDPConnectivity(host, port); err != nil {
			return fmt.Errorf("网络连接检查失败，服务器可能不可达: %v", err)
		}
	}

	// 获取本地 IP 地址
	localIP, err := getLocalIPv4()
	if err != nil {
		return fmt.Errorf("获取本地IP地址失败: %v", err)
	}

	// 创建本地 UDP 地址
	laddr := &net.UDPAddr{
		IP:   localIP,
		Port: 0, // 随机端口
	}

	// 创建 UDP 连接
	udpConn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return fmt.Errorf("创建UDP连接失败: %v", err)
	}
	defer func() {
		if err != nil {
			udpConn.Close()
		}
	}()

	// 设置 UDP 连接选项
	if err := udpConn.SetReadBuffer(1024 * 1024); err != nil {
		// 继续执行，即使设置缓冲区失败
	}
	if err := udpConn.SetWriteBuffer(1024 * 1024); err != nil {
		// 继续执行，即使设置缓冲区失败
	}

	// TLS 配置
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos: []string{
			"hq-interop",
			"h3-25",
			"h3-24",
			"h3-23",
			"hq-29",
			"hq-28",
			"hq-27",
			"http/0.9",
		},
		ServerName: host,
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}

	// QUIC 配置
	quicConfig := &quic.Config{
		KeepAlivePeriod:         2 * time.Second,
		MaxIdleTimeout:          30 * time.Second,
		HandshakeIdleTimeout:    10 * time.Second,
		MaxIncomingStreams:      100,
		EnableDatagrams:         true,
		DisablePathMTUDiscovery: false,
		Versions:                []quic.Version{quic.Version1},
	}

	// 定义要尝试的协议组合
	protocolCombinations := [][]string{
		{"hq-interop", "h3-25", "h3-24", "h3-23"},
		{"hq-29", "hq-28", "hq-27"},
		{"h3-25", "h3-24", "h3-23"},
		{"hq-interop"},
		{"http/0.9"},
	}

	// 尝试不同的连接方式和协议组合
	var conn quic.Connection
	var connectionError error

	// 首先尝试使用 quic.DialAddr
	for _, protocols := range protocolCombinations {
		tlsConf.NextProtos = protocols
		conn, err = quic.DialAddr(ctx, c.serverAddr, tlsConf, quicConfig)
		if err == nil {
			break
		}
		connectionError = err
	}

	// 如果所有协议组合都失败，尝试使用 quic.DialAddrEarly
	if conn == nil {
		for _, protocols := range protocolCombinations {
			tlsConf.NextProtos = protocols
			conn, err = quic.DialAddrEarly(ctx, c.serverAddr, tlsConf, quicConfig)
			if err == nil {
				break
			}
			connectionError = err
		}
	}

	// 如果仍然失败，返回错误
	if conn == nil {
		return fmt.Errorf("连接QUIC服务器失败: %v", connectionError)
	}

	// 关闭旧的连接（如果存在）
	if c.conn != nil {
		c.conn.CloseWithError(0, "replacing old connection")
	}

	// 关闭旧的流（如果存在）
	if c.stream != nil {
		c.stream.Close()
	}

	c.conn = conn

	// 尝试打开流
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(0, "failed to open stream")
		return fmt.Errorf("打开QUIC流失败: %v", err)
	}
	c.stream = stream

	return nil
}

// Close 关闭连接
func (c *TransferClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream != nil {
		c.stream.Close()
	}
	if c.conn != nil {
		c.conn.CloseWithError(0, "normal closure")
	}
	return nil
}

// SendInitRequest 发送初始化请求
func (c *TransferClient) SendInitRequest() error {
	reqUUID, _ := uuid.NewUUID()
	initTime := time.Now().Unix()
	initAESKey := utils.NewKey(reqUUID.String(), initTime)

	initBytes := c.transferInitByAES(c.config.ServerID, proto.PROTO_TYPE_HTTP, c.config.ServerName,
		"si:"+c.config.SessionID, reqUUID, initTime, utils.InitKey)

	if _, err := c.stream.Write(initBytes); err != nil {
		return fmt.Errorf("发送初始化请求失败: %v", err)
	}

	// 读取响应
	responseBuffer := make([]byte, 1024)
	n, err := c.stream.Read(responseBuffer)
	if err != nil {
		return fmt.Errorf("读取初始化响应失败: %v", err)
	}

	_, cmd, _, result, _ := c.parseMessageByAES(responseBuffer, n, initAESKey)

	if cmd != proto.EMM_COMMAND_INIT_ACK {
		return fmt.Errorf("收到非预期的响应命令: %d", cmd)
	}
	if result != proto.AUTH_STATUS_CODE_SUCCESS {
		return fmt.Errorf("初始化失败，错误码: %d", result)
	}

	return nil
}

// SendTransferRequest 发送传输请求
func (c *TransferClient) SendTransferRequest(content string) ([]byte, error) {
	reqUUID, _ := uuid.NewUUID()
	initTime := time.Now().Unix()
	initAESKey := utils.NewKey(reqUUID.String(), initTime)

	// 处理消息内容
	fixedContent := strings.Replace(content, "\\r\\n", "\r\n", -1)

	requestInfo := c.transferRequestByAES(fixedContent, initAESKey)

	if _, err := c.stream.Write(requestInfo); err != nil {
		return nil, fmt.Errorf("发送传输请求失败: %v", err)
	}

	// 读取响应
	responseBuffer := make([]byte, 32*1024)
	responseBytes := []byte{}
	currentSize := 0

	for {
		n, err := c.stream.Read(responseBuffer[currentSize:])
		if err != nil {
			break
		}
		if n < 0 {
			break
		}

		currentSize += n

		_, cmd, _, _, body := c.parseMessageByAES(responseBuffer, currentSize, initAESKey)

		if body != nil {
			responseBytes = append(responseBytes, body...)
		}

		if cmd == proto.EMM_COMMAND_LINK_CLOSE {
			break
		}
	}

	return responseBytes, nil
}

// SendInitRequestNoAES 发送不使用AES加密的初始化请求
func (c *TransferClient) SendInitRequestNoAES() (int, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var sentBytes, receivedBytes int

	if c.conn == nil || c.stream == nil {
		return 0, 0, fmt.Errorf("连接未建立或已关闭")
	}

	initBytes := transferInit(c.config.ServerID, proto.PROTO_TYPE_HTTP, c.config.ServerName, "si:"+c.config.SessionID)
	if initBytes == nil {
		return 0, 0, fmt.Errorf("构造初始化请求失败")
	}

	n, err := c.stream.Write(initBytes)
	sentBytes += n
	if err != nil {
		return sentBytes, 0, fmt.Errorf("发送初始化请求失败: %v", err)
	}

	// 设置读取超时
	readTimeout := 10 * time.Second
	readDeadline := time.Now().Add(readTimeout)
	if err := c.stream.SetReadDeadline(readDeadline); err != nil {
	}
	defer func() {
		if err := c.stream.SetReadDeadline(time.Time{}); err != nil {
		}
	}()

	// 读取响应
	responseBuffer := make([]byte, 1024)

	// 使用配置的重试参数
	maxRetries := c.config.MaxRetries
	retryDelay := c.config.RetryDelay
	var respLen int
	var cmd uint16
	var result uint16

	for retry := 0; retry < maxRetries; retry++ {
		if err := c.stream.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		}

		n, readErr := c.stream.Read(responseBuffer)
		if n > 0 {
			receivedBytes += n
		}

		if readErr != nil {
			if readErr == io.EOF {
				if retry < maxRetries-1 {
					newStream, streamErr := c.conn.OpenStreamSync(context.Background())
					if streamErr != nil {
					} else {
						c.stream.Close()
						c.stream = newStream

						written, writeErr := c.stream.Write(initBytes)
						sentBytes += written
						if writeErr != nil {
							continue
						}
						time.Sleep(retryDelay)
						continue
					}
				}
			} else if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
			} else {
			}

			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			return sentBytes, receivedBytes, fmt.Errorf("读取初始化响应失败: %v", readErr)
		}

		if n <= 0 {
			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			return sentBytes, receivedBytes, fmt.Errorf("读取初始化响应失败: 读取到0字节")
		}

		respLen, cmd, _, result, _ = parseMessage(responseBuffer[:n], n)

		if respLen > 0 {
			break
		}

		if retry < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}

	if respLen == 0 {
		return sentBytes, receivedBytes, fmt.Errorf("解析初始化响应失败: 响应长度为0")
	}

	if cmd != proto.EMM_COMMAND_INIT_ACK {
		return sentBytes, receivedBytes, fmt.Errorf("收到非预期的响应命令: %d", cmd)
	}
	if result != proto.AUTH_STATUS_CODE_SUCCESS {
		return sentBytes, receivedBytes, fmt.Errorf("初始化失败，错误码: %d", result)
	}

	return sentBytes, receivedBytes, nil
}

// SendTransferRequestNoAES 发送不使用AES加密的传输请求
func (c *TransferClient) SendTransferRequestNoAES(content string) ([]byte, int, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var sentBytes, receivedBytes int

	if c.conn == nil {
		return nil, 0, 0, fmt.Errorf("连接未建立")
	}

	if c.conn.Context().Err() != nil {
		if err := c.Connect(context.Background()); err != nil {
			return nil, 0, 0, fmt.Errorf("重新建立连接失败: %v", err)
		}
	}

	fixedContent := strings.Replace(content, "\\r\\n", "\r\n", -1)

	requestInfo := transferRequest(fixedContent)

	// 设置读取超时
	readTimeout := 10 * time.Second
	maxRetries := c.config.MaxRetries
	retryDelay := c.config.RetryDelay
	var responseBytes []byte

	for retry := 0; retry < maxRetries; retry++ {
		if c.stream == nil {
			stream, err := c.conn.OpenStreamSync(context.Background())
			if err != nil {
				if retry < maxRetries-1 {
					time.Sleep(retryDelay)
					continue
				}
				return nil, sentBytes, receivedBytes, fmt.Errorf("无法创建流: %v", err)
			}
			c.stream = stream
		}

		if err := c.stream.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		}

		n, err := c.stream.Write(requestInfo)
		sentBytes += n
		if err != nil {
			c.stream.Close()
			c.stream = nil
			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			return nil, sentBytes, receivedBytes, fmt.Errorf("发送请求失败: %v", err)
		}

		responseBuffer := make([]byte, 32*1024)
		readBytes, err := c.stream.Read(responseBuffer)
		if readBytes > 0 {
			receivedBytes += readBytes
		}

		if err != nil {
			if err == io.EOF {
				if readBytes > 0 {
					responseBytes = append(responseBytes, responseBuffer[:readBytes]...)
					break
				}
			}

			c.stream.Close()
			c.stream = nil

			if strings.Contains(err.Error(), "Application error 0x0") {
				if err := c.Connect(context.Background()); err != nil {
				}
			}

			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			if len(responseBytes) == 0 {
				return nil, sentBytes, receivedBytes, fmt.Errorf("读取响应失败: %v", err)
			}
			break
		}

		if readBytes <= 0 {
			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			break
		}

		respLen, cmd, _, _, body := parseMessage(responseBuffer[:readBytes], readBytes)

		if body != "" {
			responseBytes = append(responseBytes, []byte(body)...)
		}

		if cmd == proto.EMM_COMMAND_LINK_CLOSE {
			break
		}

		if respLen > 0 && respLen <= readBytes {
			break
		}
	}

	if c.stream != nil {
		if err := c.stream.SetReadDeadline(time.Time{}); err != nil {
		}
	}

	return responseBytes, sentBytes, receivedBytes, nil
}

func transferInit(serverid int, protocoltype int, appname string, sessionid string) []byte {
	head := proto.TransferHeader{
		Tag:       proto.HEAD_TAG,
		Version:   proto.PROTO_VERSION,
		Command:   proto.EMM_COMMAND_INIT,
		ProtoType: proto.DATA_PROTO_TYPE_JSON,
		Option:    0,
		Reserve:   0,
	}

	reqUUID, _ := uuid.NewUUID()

	initInfo := proto.InitInfo{
		Serverid:     serverid,
		ProtocolType: protocoltype,
		Appname:      appname,
		RequesetId:   reqUUID.String(),
		Sessionid:    sessionid,
	}

	initBytes, err := json.Marshal(initInfo)
	if err != nil {
		return nil
	}

	buf := utils.NewEmptyBuffer()
	head.DataLen = uint32(len(initBytes))

	headBytes, err := head.Marshal()
	if err != nil {
		return nil
	}

	buf.WriteBytes(headBytes)
	buf.WriteBytes(initBytes)

	result := buf.Bytes()

	return result
}

func transferRequest(requestinfo string) []byte {
	head := proto.TransferHeader{
		Tag:       proto.HEAD_TAG,
		Version:   proto.PROTO_VERSION,
		Command:   proto.EMM_COMMAND_TRAN,
		ProtoType: uint8(proto.PROTO_TYPE_HTTP),
		Option:    0,
		Reserve:   0,
	}

	buf := utils.NewEmptyBuffer()
	head.DataLen = uint32(len(requestinfo))

	headBytes, err := head.Marshal()
	if err != nil {
		return nil
	}

	buf.WriteBytes(headBytes)
	buf.WriteBytes([]byte(requestinfo))

	result := buf.Bytes()

	return result
}

func (c *TransferClient) transferInitByAES(serverID int, protocolType int, serverName string,
	sessionID string, reqUUID uuid.UUID, timeStamp int64, initAESKey string) []byte {
	msg := &proto.UdpMessage{
		Head: proto.TransferHeader{
			Tag:       proto.HEAD_TAG,
			Version:   proto.PROTO_VERSION,
			Command:   proto.EMM_COMMAND_INIT,
			ProtoType: uint8(protocolType),
			Option:    0,
			Reserve:   0,
		},
	}

	bodyBuf := utils.NewEmptyBuffer()
	bodyBuf.WriteUint32(uint32(serverID))
	bodyBuf.WriteString(serverName)
	bodyBuf.WriteByte(0)
	bodyBuf.WriteString(sessionID)
	bodyBuf.WriteByte(0)
	bodyBuf.WriteString(reqUUID.String())
	bodyBuf.WriteByte(0)
	bodyBuf.WriteUint64(uint64(timeStamp))

	rawBody := bodyBuf.Bytes()

	encryptedBody, err := utils.EncryptAES([]byte(initAESKey), rawBody)
	if err != nil {
	} else {
	}

	msg.Body = encryptedBody
	msg.Head.DataLen = uint32(len(encryptedBody))

	data, err := msg.Marshal()
	if err != nil {
	} else {
	}
	return data
}

func (c *TransferClient) transferRequestByAES(content string, initAESKey string) []byte {
	msg := &proto.UdpMessage{
		Head: proto.TransferHeader{
			Tag:       proto.HEAD_TAG,
			Version:   proto.PROTO_VERSION,
			Command:   proto.EMM_COMMAND_TRAN,
			ProtoType: uint8(proto.PROTO_TYPE_HTTP),
			Option:    0,
			Reserve:   0,
		},
	}

	rawContent := []byte(content)

	encryptedBody, err := utils.EncryptAES([]byte(initAESKey), rawContent)
	if err != nil {
	} else {
	}

	msg.Body = encryptedBody
	msg.Head.DataLen = uint32(len(encryptedBody))

	data, err := msg.Marshal()
	if err != nil {
	} else {
	}
	return data
}

func parseMessage(message []byte, msgLength int) (int, uint16, uint32, uint16, string) {
	if msgLength < proto.RESPONSE_HEAD_LEN {
		return 0, 0, 0, 0, ""
	}

	msg := new(proto.UdpResponseMessage)
	err := msg.Head.UnMarshal(message[:proto.RESPONSE_HEAD_LEN])
	if err != nil {
		return 0, 0, 0, 0, ""
	}

	if msg.Head.Tag != proto.HEAD_TAG {
		return 0, 0, 0, 0, ""
	}

	msglen := int(msg.Head.DataLen) + proto.RESPONSE_HEAD_LEN

	if msglen > msgLength {
		return 0, 0, 0, 0, ""
	}

	if int(msg.Head.DataLen) > 0 {
		err = msg.ParseBody(message[0:msglen], int(msg.Head.DataLen))
		if err != nil {
			return 0, 0, 0, 0, ""
		}

		bodyLength := len(msg.Body)

		if bodyLength > 0 {
			bodyStr := string(msg.Body)
			bodyStrLen := len(bodyStr)
			if bodyStrLen > 0 {
				return msglen, msg.Head.Command, msg.Head.DataLen, msg.Head.Result, bodyStr
			}
		}
	}

	return msglen, msg.Head.Command, msg.Head.DataLen, msg.Head.Result, ""
}

func (c *TransferClient) parseMessageByAES(message []byte, length int, initAESKey string) (int, uint16, uint32, uint16, []byte) {
	resp := &proto.UdpResponseMessage{}
	resp.ParseHead(message[:proto.RESPONSE_HEAD_LEN])

	if resp.Head.DataLen > 0 {
		resp.ParseBody(message, int(resp.Head.DataLen))
		if len(resp.Body) > 0 {
			decryptedBody, err := utils.DecryptAES([]byte(initAESKey), resp.Body)
			if err != nil {
				return resp.Head.Len(), resp.Head.Command, resp.Head.DataLen, resp.Head.Result, nil
			}

			return resp.Head.Len(), resp.Head.Command, resp.Head.DataLen, resp.Head.Result, decryptedBody
		}
	}

	return resp.Head.Len(), resp.Head.Command, resp.Head.DataLen, resp.Head.Result, nil
}

func getLocalIPv4() (net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("获取网络接口失败: %v", err)
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}

			if ip[0] == 127 {
				continue
			}

			if ip[0] == 169 && ip[1] == 254 {
				continue
			}

			return ip, nil
		}
	}

	return nil, fmt.Errorf("未找到合适的IPv4地址")
}

func checkNetworkConnectivity(host string, port string) error {
	address := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("网络连接检查失败: %v", err)
	}
	conn.Close()
	return nil
}

func checkUDPConnectivity(host string, port string) error {
	address := net.JoinHostPort(host, port)
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("解析UDP地址失败: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("创建UDP连接失败: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("test"))
	if err != nil {
		return fmt.Errorf("发送UDP测试数据失败: %v", err)
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
