package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
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
}

// Config 客户端配置
type Config struct {
	ServerID   int
	ServerName string
	SessionID  string
}

// NewTransferClient 创建新的传输客户端
func NewTransferClient(serverAddr string, config *Config) *TransferClient {
	// log.Printf("创建新的QUIC客户端 - 服务器地址: %s, 服务器ID: %d, 服务器名称: %s",
	// 	serverAddr, config.ServerID, config.ServerName)
	return &TransferClient{
		serverAddr: serverAddr,
		config:     config,
	}
}

// Connect 连接服务器
func (c *TransferClient) Connect(ctx context.Context) error {
	// log.Printf("正在连接QUIC服务器 %s...", c.serverAddr)
	// log.Printf("连接配置信息 - ServerID: %d, ServerName: %s, SessionID: %s",
	// 	c.config.ServerID, c.config.ServerName, c.config.SessionID)

	// 解析服务器地址
	host, port, err := net.SplitHostPort(c.serverAddr)
	if err != nil {
		return fmt.Errorf("解析服务器地址失败: %v", err)
	}
	// log.Printf("解析服务器地址 - 主机: %s, 端口: %s", host, port)

	// 检查网络连接
	// log.Printf("检查网络连接...")
	if err := checkNetworkConnectivity(host, port); err != nil {
		// log.Printf("TCP连接检查失败: %v", err)
		// log.Printf("尝试检查UDP连接...")
		if err := checkUDPConnectivity(host, port); err != nil {
			// log.Printf("UDP连接检查也失败: %v", err)
			return fmt.Errorf("网络连接检查失败，服务器可能不可达: %v", err)
		}
	}
	// log.Printf("网络连接检查通过")

	// 获取本地 IP 地址
	localIP, err := getLocalIPv4()
	if err != nil {
		return fmt.Errorf("获取本地IP地址失败: %v", err)
	}
	// log.Printf("本地IPv4地址: %s", localIP.String())

	// 创建本地 UDP 地址
	laddr := &net.UDPAddr{
		IP:   localIP,
		Port: 0, // 随机端口
	}
	// log.Printf("本地UDP地址: %s", laddr.String())

	// 注释掉未使用的raddr变量
	// raddr := &net.UDPAddr{
	// 	IP:   ipAddr.IP,
	// 	Port: 8002, // 强制使用 8002 端口
	// }
	// log.Printf("远程UDP地址: %s", raddr.String())

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
	// log.Printf("创建UDP连接成功 - 本地地址: %s", udpConn.LocalAddr().String())

	// 设置 UDP 连接选项
	if err := udpConn.SetReadBuffer(1024 * 1024); err != nil {
		// log.Printf("警告：设置UDP读取缓冲区大小失败: %v", err)
	}
	if err := udpConn.SetWriteBuffer(1024 * 1024); err != nil {
		// log.Printf("警告：设置UDP写入缓冲区大小失败: %v", err)
	}

	// TLS 配置
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos: []string{
			"hq-interop",
			"h3-23",
			"h3-24",
			"h3-25",
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
	// log.Printf("TLS配置 - ServerName: %s, Protocols: %v", tlsConf.ServerName, tlsConf.NextProtos)

	// QUIC 配置
	quicConfig := &quic.Config{
		KeepAlivePeriod:         2 * time.Second,  // 减少保活间隔
		MaxIdleTimeout:          30 * time.Second, // 增加空闲超时
		HandshakeIdleTimeout:    10 * time.Second, // 增加握手超时
		MaxIncomingStreams:      100,
		EnableDatagrams:         true,
		DisablePathMTUDiscovery: false,
		Versions:                []quic.Version{quic.Version1}, // 只使用 QUIC 版本 1
	}
	// log.Printf("QUIC配置 - KeepAlive: %v, MaxIdle: %v, HandshakeTimeout: %v",
	// 	quicConfig.KeepAlivePeriod, quicConfig.MaxIdleTimeout, quicConfig.HandshakeIdleTimeout)

	// 尝试建立 QUIC 连接
	// log.Printf("正在建立QUIC连接到 %s...", c.serverAddr)

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
		// log.Printf("尝试使用协议: %v", protocols)

		conn, err = quic.DialAddr(ctx, c.serverAddr, tlsConf, quicConfig)
		if err == nil {
			// log.Printf("使用协议 %v 连接成功", protocols)
			break
		}

		// log.Printf("使用协议 %v 连接失败: %v", protocols, err)
		connectionError = err
	}

	// 如果所有协议组合都失败，尝试使用 quic.DialAddrEarly
	if conn == nil {
		// log.Printf("所有协议组合都失败，尝试使用 quic.DialAddrEarly")

		for _, protocols := range protocolCombinations {
			tlsConf.NextProtos = protocols
			// log.Printf("尝试使用协议: %v (DialAddrEarly)", protocols)

			conn, err = quic.DialAddrEarly(ctx, c.serverAddr, tlsConf, quicConfig)
			if err == nil {
				// log.Printf("使用协议 %v (DialAddrEarly) 连接成功", protocols)
				break
			}

			// log.Printf("使用协议 %v (DialAddrEarly) 连接失败: %v", protocols, err)
			connectionError = err
		}
	}

	// 如果仍然失败，返回错误
	if conn == nil {
		// 注释掉未使用的netErr变量
		// if netErr, ok := connectionError.(net.Error); ok {
		// 	// log.Printf("网络错误 - Timeout: %v, Temporary: %v", netErr.Timeout(), netErr.Temporary())
		// }
		return fmt.Errorf("连接QUIC服务器失败: %v", connectionError)
	}

	c.conn = conn
	// log.Printf("QUIC连接建立成功，远程地址: %s", conn.RemoteAddr().String())

	// 尝试打开流
	// log.Printf("正在打开QUIC流...")
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(0, "failed to open stream")
		return fmt.Errorf("打开QUIC流失败: %v", err)
	}
	c.stream = stream
	// log.Printf("QUIC流打开成功")

	// log.Printf("成功连接到QUIC服务器 %s", c.serverAddr)
	return nil
}

// Close 关闭连接
func (c *TransferClient) Close() error {
	if c.stream != nil {
		c.stream.Close()
	}
	if c.conn != nil {
		c.conn.CloseWithError(0, "normal closure")
	}
	// log.Printf("关闭QUIC连接")
	return nil
}

// SendInitRequest 发送初始化请求
func (c *TransferClient) SendInitRequest() error {
	reqUUID, _ := uuid.NewUUID()
	initTime := time.Now().Unix()
	initAESKey := utils.NewKey(reqUUID.String(), initTime)

	// log.Printf("发送初始化请求 - UUID: %s, 时间戳: %d", reqUUID.String(), initTime)
	// log.Printf("初始化AES密钥: %s (长度: %d)", initAESKey, len(initAESKey))
	// log.Printf("初始化参数 - ServerID: %d, ProtoType: %d, ServerName: %s, SessionID: %s",
	// 	c.config.ServerID, proto.PROTO_TYPE_HTTP, c.config.ServerName, "si:"+c.config.SessionID)

	initBytes := c.transferInitByAES(c.config.ServerID, proto.PROTO_TYPE_HTTP, c.config.ServerName,
		"si:"+c.config.SessionID, reqUUID, initTime, utils.InitKey)

	// log.Printf("初始化请求数据长度: %d 字节", len(initBytes))
	// log.Printf("初始化请求头部数据: %X", initBytes[:proto.REQUEST_HEAD_LEN])
	// if len(initBytes) > proto.REQUEST_HEAD_LEN {
	// 	log.Printf("初始化请求体数据(前50字节): %X", initBytes[proto.REQUEST_HEAD_LEN:min(proto.REQUEST_HEAD_LEN+50, len(initBytes))])
	// }

	if _, err := c.stream.Write(initBytes); err != nil {
		return fmt.Errorf("发送初始化请求失败: %v", err)
	}
	// log.Printf("初始化请求已发送")

	// 读取响应
	responseBuffer := make([]byte, 1024)
	n, err := c.stream.Read(responseBuffer)
	if err != nil {
		return fmt.Errorf("读取初始化响应失败: %v", err)
	}
	// log.Printf("收到初始化响应，长度: %d 字节", n)
	// log.Printf("响应原始数据: %X", responseBuffer[:n])

	// 解析响应，使用_忽略未使用的变量
	_, cmd, _, result, _ := c.parseMessageByAES(responseBuffer, n, initAESKey)
	// log.Printf("解析响应 - 长度: %d, 命令: %d, 数据长度: %d, 结果: %d", respLen, cmd, dataLen, result)
	// if body != nil {
	// 	log.Printf("响应体解密后数据(前50字节): %X", body[:min(50, len(body))])
	// }

	if cmd != proto.EMM_COMMAND_INIT_ACK {
		return fmt.Errorf("收到非预期的响应命令: %d", cmd)
	}
	if result != proto.AUTH_STATUS_CODE_SUCCESS {
		return fmt.Errorf("初始化失败，错误码: %d", result)
	}

	// log.Printf("初始化请求成功")
	return nil
}

// SendTransferRequest 发送传输请求
func (c *TransferClient) SendTransferRequest(content string) ([]byte, error) {
	reqUUID, _ := uuid.NewUUID()
	initTime := time.Now().Unix()
	initAESKey := utils.NewKey(reqUUID.String(), initTime)

	// log.Printf("发送传输请求 - UUID: %s, 时间戳: %d", reqUUID.String(), initTime)
	// log.Printf("传输请求AES密钥: %s (长度: %d)", initAESKey, len(initAESKey))
	// log.Printf("请求内容: %s", content)

	// 处理消息内容
	fixedContent := strings.Replace(content, "\\r\\n", "\r\n", -1)
	// log.Printf("处理后的请求内容(长度: %d): %X", len(fixedContent), []byte(fixedContent))

	requestInfo := c.transferRequestByAES(fixedContent, initAESKey)
	// log.Printf("传输请求数据长度: %d 字节", len(requestInfo))
	// log.Printf("传输请求头部数据: %X", requestInfo[:proto.REQUEST_HEAD_LEN])
	// if len(requestInfo) > proto.REQUEST_HEAD_LEN {
	// 	log.Printf("传输请求体数据(前50字节): %X", requestInfo[proto.REQUEST_HEAD_LEN:min(proto.REQUEST_HEAD_LEN+50, len(requestInfo))])
	// }

	if _, err := c.stream.Write(requestInfo); err != nil {
		return nil, fmt.Errorf("发送传输请求失败: %v", err)
	}
	// log.Printf("传输请求已发送")

	// 读取响应
	responseBuffer := make([]byte, 32*1024)
	responseBytes := []byte{}
	currentSize := 0

	for {
		n, err := c.stream.Read(responseBuffer[currentSize:])
		if err != nil {
			// log.Printf("读取响应结束: %v", err)
			break
		}
		if n < 0 {
			// log.Printf("读取到负数字节: %d", n)
			break
		}

		// log.Printf("读取到 %d 字节数据", n)
		currentSize += n
		// log.Printf("当前缓冲区大小: %d 字节", currentSize)
		// log.Printf("响应原始数据(前50字节): %X", responseBuffer[:min(50, currentSize)])

		// 解析响应，使用_忽略未使用的变量
		_, cmd, _, _, body := c.parseMessageByAES(responseBuffer, currentSize, initAESKey)
		// log.Printf("解析响应 - 长度: %d, 命令: %d, 数据长度: %d, 结果: %d", respLen, cmd, dataLen, result)

		if body != nil {
			// log.Printf("响应体解密后数据(前50字节): %X", body[:min(50, len(body))])
			responseBytes = append(responseBytes, body...)
			// log.Printf("累计响应数据长度: %d 字节", len(responseBytes))
		}

		if cmd == proto.EMM_COMMAND_LINK_CLOSE {
			// log.Printf("收到关闭链路命令，停止读取")
			break
		}
	}

	// log.Printf("收到响应数据，总长度: %d 字节", len(responseBytes))
	return responseBytes, nil
}

// SendInitRequestNoAES 发送不使用AES加密的初始化请求
func (c *TransferClient) SendInitRequestNoAES() error {
	// log.Println("===== 发送非AES加密的初始化请求 =====")

	// 检查连接状态
	if c.conn == nil || c.stream == nil {
		return fmt.Errorf("连接未建立或已关闭")
	}

	// 构造初始化请求
	initBytes := transferInit(c.config.ServerID, proto.PROTO_TYPE_HTTP, c.config.ServerName, "si:"+c.config.SessionID)
	if initBytes == nil {
		return fmt.Errorf("构造初始化请求失败")
	}

	// log.Printf("初始化请求数据长度: %d 字节", len(initBytes))
	// log.Printf("初始化请求头部数据: %X", initBytes[:proto.REQUEST_HEAD_LEN])
	// if len(initBytes) > proto.REQUEST_HEAD_LEN {
	// 	log.Printf("初始化请求体数据(前50字节): %X", initBytes[proto.REQUEST_HEAD_LEN:min(proto.REQUEST_HEAD_LEN+50, len(initBytes))])
	// }

	// 发送请求
	_, err := c.stream.Write(initBytes)
	if err != nil {
		return fmt.Errorf("发送初始化请求失败: %v", err)
	}
	// log.Printf("初始化请求已发送，写入 %d 字节", written)

	// 设置读取超时
	readTimeout := 10 * time.Second
	readDeadline := time.Now().Add(readTimeout)
	if err := c.stream.SetReadDeadline(readDeadline); err != nil {
		// log.Printf("警告：设置读取超时失败: %v", err)
	}
	defer func() {
		// 重置读取超时
		if err := c.stream.SetReadDeadline(time.Time{}); err != nil {
			// log.Printf("警告：清除读取超时失败: %v", err)
		}
	}()

	// 读取响应
	responseBuffer := make([]byte, 1024)

	// 添加重试逻辑
	maxRetries := 3
	retryDelay := 500 * time.Millisecond
	var respLen int
	var cmd uint16
	var result uint16

	for retry := 0; retry < maxRetries; retry++ {
		// 重置读取超时
		if err := c.stream.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			// log.Printf("警告：重置读取超时失败: %v", err)
		}

		n, readErr := c.stream.Read(responseBuffer)
		if readErr != nil {
			if readErr == io.EOF {
				// EOF可能意味着流已关闭
				// log.Printf("读取到EOF (尝试 %d/%d)，流已关闭", retry+1, maxRetries)

				if retry < maxRetries-1 {
					// log.Printf("尝试重新打开流...")
					// 尝试重新打开流
					newStream, streamErr := c.conn.OpenStreamSync(context.Background())
					if streamErr != nil {
						// log.Printf("重新打开流失败: %v", streamErr)
					} else {
						// log.Printf("成功重新打开流")
						c.stream.Close()     // 关闭旧流
						c.stream = newStream // 使用新流

						// 重新发送请求
						_, writeErr := c.stream.Write(initBytes)
						if writeErr != nil {
							// log.Printf("重新发送请求失败: %v", writeErr)
							continue
						}
						// log.Printf("重新发送请求成功，写入 %d 字节", written)
						time.Sleep(retryDelay)
						continue
					}
				}
			} else if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				// log.Printf("读取超时 (尝试 %d/%d): %v", retry+1, maxRetries, readErr)
			} else {
				// log.Printf("读取初始化响应失败 (尝试 %d/%d): %v", retry+1, maxRetries, readErr)
			}

			if retry < maxRetries-1 {
				// log.Printf("将在 %v 后重试...", retryDelay)
				time.Sleep(retryDelay)
				continue
			}
			return fmt.Errorf("读取初始化响应失败: %v", readErr)
		}

		if n <= 0 {
			// log.Printf("读取到0字节 (尝试 %d/%d)", retry+1, maxRetries)
			if retry < maxRetries-1 {
				// log.Printf("将在 %v 后重试...", retryDelay)
				time.Sleep(retryDelay)
				continue
			}
			return fmt.Errorf("读取初始化响应失败: 读取到0字节")
		}

		// log.Printf("收到初始化响应，长度: %d 字节", n)
		// log.Printf("响应原始数据: %X", responseBuffer[:n])

		// 解析响应，使用_忽略未使用的变量
		respLen, cmd, _, result, _ = parseMessage(responseBuffer[:n], n)
		// log.Printf("解析响应 - 长度: %d, 命令: %d, 数据长度: %d, 结果: %d", respLen, cmd, dataLen, result)
		// log.Printf("响应体: %s", body)

		if respLen > 0 {
			break // 成功解析到响应
		}

		if retry < maxRetries-1 {
			// log.Printf("解析响应长度为0，可能数据不完整，重试...")
			time.Sleep(retryDelay)
		}
	}

	if respLen == 0 {
		return fmt.Errorf("解析初始化响应失败: 响应长度为0")
	}

	if cmd != proto.EMM_COMMAND_INIT_ACK {
		return fmt.Errorf("收到非预期的响应命令: %d", cmd)
	}
	if result != proto.AUTH_STATUS_CODE_SUCCESS {
		return fmt.Errorf("初始化失败，错误码: %d", result)
	}

	// log.Printf("初始化请求成功")
	return nil
}

// SendTransferRequestNoAES 发送不使用AES加密的传输请求
func (c *TransferClient) SendTransferRequestNoAES(content string) ([]byte, error) {
	// log.Println("===== 发送非AES加密的传输请求 =====")

	// 检查连接状态
	if c.conn == nil {
		return nil, fmt.Errorf("连接未建立")
	}

	// 检查连接是否关闭
	if c.conn.Context().Err() != nil {
		// log.Printf("连接已关闭，尝试重新建立连接...")
		if err := c.Connect(context.Background()); err != nil {
			return nil, fmt.Errorf("重新建立连接失败: %v", err)
		}
	}

	// 处理消息内容
	fixedContent := strings.Replace(content, "\\r\\n", "\r\n", -1)
	// log.Printf("请求内容: %s", fixedContent)
	// log.Printf("处理后的请求内容(长度: %d): %X", len(fixedContent), []byte(fixedContent))

	// 构造传输请求
	requestInfo := transferRequest(fixedContent)

	// log.Printf("传输请求数据长度: %d 字节", len(requestInfo))
	// log.Printf("传输请求头部数据: %X", requestInfo[:proto.REQUEST_HEAD_LEN])
	// if len(requestInfo) > proto.REQUEST_HEAD_LEN {
	// 	log.Printf("传输请求体数据(前50字节): %X", requestInfo[proto.REQUEST_HEAD_LEN:min(proto.REQUEST_HEAD_LEN+50, len(requestInfo))])
	// }

	// 设置读取超时
	readTimeout := 10 * time.Second
	maxRetries := 3
	retryDelay := 500 * time.Millisecond
	var responseBytes []byte

	for retry := 0; retry < maxRetries; retry++ {
		// 确保有可用的流
		if c.stream == nil {
			// log.Printf("创建新的流...")
			stream, err := c.conn.OpenStreamSync(context.Background())
			if err != nil {
				// log.Printf("创建流失败 (尝试 %d/%d): %v", retry+1, maxRetries, err)
				if retry < maxRetries-1 {
					time.Sleep(retryDelay)
					continue
				}
				return nil, fmt.Errorf("无法创建流: %v", err)
			}
			c.stream = stream
		}

		// 设置读取超时
		if err := c.stream.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			// log.Printf("警告：设置读取超时失败: %v", err)
		}

		// 发送请求
		_, err := c.stream.Write(requestInfo)
		if err != nil {
			// log.Printf("发送请求失败 (尝试 %d/%d): %v", retry+1, maxRetries, err)
			c.stream.Close()
			c.stream = nil
			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			return nil, fmt.Errorf("发送请求失败: %v", err)
		}
		// log.Printf("传输请求已发送，写入 %d 字节", written)

		// 读取响应
		responseBuffer := make([]byte, 32*1024)
		n, err := c.stream.Read(responseBuffer)

		if err != nil {
			if err == io.EOF {
				// log.Printf("读取到EOF (尝试 %d/%d)", retry+1, maxRetries)
				// 如果已经读取了数据，可能是正常的EOF
				if n > 0 {
					// log.Printf("EOF之前读取到 %d 字节数据", n)
					responseBytes = append(responseBytes, responseBuffer[:n]...)
					break
				}
			} else {
				// log.Printf("读取响应失败 (尝试 %d/%d): %v", retry+1, maxRetries, err)
			}

			// 关闭当前流
			c.stream.Close()
			c.stream = nil

			// 检查是否需要重置连接
			if strings.Contains(err.Error(), "Application error 0x0") {
				// log.Printf("检测到应用层错误，尝试重置连接...")
				if err := c.Connect(context.Background()); err != nil {
					// log.Printf("重置连接失败: %v", err)
				}
			}

			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			if len(responseBytes) == 0 {
				return nil, fmt.Errorf("读取响应失败: %v", err)
			}
			break
		}

		if n <= 0 {
			// log.Printf("读取到0字节 (尝试 %d/%d)", retry+1, maxRetries)
			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
			break
		}

		// log.Printf("读取到 %d 字节数据", n)
		// log.Printf("响应原始数据(前50字节): %X", responseBuffer[:min(50, n)])

		// 解析读取到的数据，使用_忽略未使用的变量
		respLen, cmd, _, _, body := parseMessage(responseBuffer[:n], n)
		// log.Printf("解析响应 - 长度: %d, 命令: %d, 数据长度: %d, 结果: %d", respLen, cmd, dataLen, result)

		if body != "" {
			responseBytes = append(responseBytes, []byte(body)...)
			// log.Printf("累计响应数据长度: %d 字节", len(responseBytes))
		}

		if cmd == proto.EMM_COMMAND_LINK_CLOSE {
			// log.Printf("收到关闭链路命令，停止读取")
			break
		}

		// 如果已经读取完整个消息，则退出循环
		if respLen > 0 && respLen <= n {
			// log.Printf("已读取完整个消息，停止读取")
			break
		}
	}

	// 重置读取超时
	if c.stream != nil {
		if err := c.stream.SetReadDeadline(time.Time{}); err != nil {
			// log.Printf("警告：清除读取超时失败: %v", err)
		}
	}

	// log.Printf("收到响应数据，总长度: %d 字节", len(responseBytes))

	// 如果没有收到任何数据，但没有遇到错误，返回空响应
	if len(responseBytes) == 0 {
		// log.Printf("警告：收到空响应")
	}

	return responseBytes, nil
}

func transferInit(serverid int, protocoltype int, appname string, sessionid string) []byte {
	// log.Printf("创建非AES加密的初始化请求 - ServerID: %d, ProtocolType: %d, AppName: %s, SessionID: %s",
	// 	serverid, protocoltype, appname, sessionid)

	// 构造消息头
	head := proto.TransferHeader{
		Tag:       proto.HEAD_TAG,
		Version:   proto.PROTO_VERSION,
		Command:   proto.EMM_COMMAND_INIT,
		ProtoType: proto.DATA_PROTO_TYPE_JSON,
		Option:    0,
		Reserve:   0,
	}
	// log.Printf("消息头 - Tag: %d, Version: %d, Command: %d, ProtoType: %d",
	// 	head.Tag, head.Version, head.Command, head.ProtoType)

	// 生成请求ID
	reqUUID, _ := uuid.NewUUID()
	// log.Printf("请求UUID: %s", reqUUID.String())

	// 构造初始化信息
	initInfo := proto.InitInfo{
		Serverid:     serverid,
		ProtocolType: protocoltype,
		Appname:      appname,
		RequesetId:   reqUUID.String(),
		Sessionid:    sessionid,
	}

	// 序列化为JSON
	initBytes, err := json.Marshal(initInfo)
	if err != nil {
		// log.Printf("序列化初始化信息失败: %v", err)
		return nil
	}
	// log.Printf("初始化信息JSON(长度: %d): %s", len(initBytes), string(initBytes))

	// 构造完整消息
	buf := utils.NewEmptyBuffer()
	head.DataLen = uint32(len(initBytes))
	// log.Printf("设置消息头DataLen: %d", head.DataLen)

	headBytes, err := head.Marshal()
	if err != nil {
		// log.Printf("序列化消息头失败: %v", err)
		return nil
	}
	// log.Printf("消息头序列化后(长度: %d): %X", len(headBytes), headBytes)

	buf.WriteBytes(headBytes)
	buf.WriteBytes(initBytes)

	result := buf.Bytes()
	// log.Printf("完整消息(长度: %d): %X", len(result), result[:min(50, len(result))])

	return result
}

func transferRequest(requestinfo string) []byte {
	// log.Printf("创建非AES加密的传输请求 - 内容长度: %d", len(requestinfo))

	// 构造消息头
	head := proto.TransferHeader{
		Tag:       proto.HEAD_TAG,
		Version:   proto.PROTO_VERSION,
		Command:   proto.EMM_COMMAND_TRAN,
		ProtoType: uint8(proto.PROTO_TYPE_HTTP),
		Option:    0,
		Reserve:   0,
	}
	// log.Printf("消息头 - Tag: %d, Version: %d, Command: %d, ProtoType: %d",
	// 	head.Tag, head.Version, head.Command, head.ProtoType)

	// 构造完整消息
	buf := utils.NewEmptyBuffer()
	head.DataLen = uint32(len(requestinfo))
	// log.Printf("设置消息头DataLen: %d", head.DataLen)

	headBytes, err := head.Marshal()
	if err != nil {
		// log.Printf("序列化消息头失败: %v", err)
		return nil
	}
	// log.Printf("消息头序列化后(长度: %d): %X", len(headBytes), headBytes)

	buf.WriteBytes(headBytes)
	buf.WriteBytes([]byte(requestinfo))

	result := buf.Bytes()
	// log.Printf("完整消息(长度: %d): %X", len(result), result[:min(50, len(result))])

	return result
}

// 内部辅助方法
func (c *TransferClient) transferInitByAES(serverID int, protocolType int, serverName string,
	sessionID string, reqUUID uuid.UUID, timeStamp int64, initAESKey string) []byte {
	// 构造初始化消息
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
	// log.Printf("消息头 - Tag: %d, Version: %d, Command: %d, ProtoType: %d",
	// 	msg.Head.Tag, msg.Head.Version, msg.Head.Command, msg.Head.ProtoType)

	// 构造消息体
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
	// log.Printf("消息体原始数据(长度: %d): %X", len(rawBody), rawBody)
	// log.Printf("消息体内容 - ServerID: %d, ServerName: %s, SessionID: %s, UUID: %s, TimeStamp: %d",
	// 	serverID, serverName, sessionID, reqUUID.String(), timeStamp)
	// log.Printf("加密密钥(长度: %d): %s", len(initAESKey), initAESKey)

	// 加密消息体
	encryptedBody, err := utils.EncryptAES([]byte(initAESKey), rawBody)
	if err != nil {
		// log.Printf("加密消息体失败: %v", err)
	} else {
		// log.Printf("加密后消息体(长度: %d): %X", len(encryptedBody), encryptedBody[:min(50, len(encryptedBody))])
	}

	msg.Body = encryptedBody
	msg.Head.DataLen = uint32(len(encryptedBody))
	// log.Printf("设置消息头DataLen: %d", msg.Head.DataLen)

	// 序列化消息
	data, err := msg.Marshal()
	if err != nil {
		// log.Printf("序列化消息失败: %v", err)
	} else {
		// log.Printf("序列化后完整消息(长度: %d): %X", len(data), data[:min(50, len(data))])
	}
	return data
}

func (c *TransferClient) transferRequestByAES(content string, initAESKey string) []byte {
	// 构造请求消息
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
	// log.Printf("传输消息头 - Tag: %d, Version: %d, Command: %d, ProtoType: %d",
	// 	msg.Head.Tag, msg.Head.Version, msg.Head.Command, msg.Head.ProtoType)

	// 加密消息体
	rawContent := []byte(content)
	// log.Printf("传输消息体原始数据(长度: %d): %X", len(rawContent), rawContent[:min(50, len(rawContent))])
	// log.Printf("加密密钥(长度: %d): %s", len(initAESKey), initAESKey)

	encryptedBody, err := utils.EncryptAES([]byte(initAESKey), rawContent)
	if err != nil {
		// log.Printf("加密传输消息体失败: %v", err)
	} else {
		// log.Printf("加密后传输消息体(长度: %d): %X", len(encryptedBody), encryptedBody[:min(50, len(encryptedBody))])
	}

	msg.Body = encryptedBody
	msg.Head.DataLen = uint32(len(encryptedBody))
	// log.Printf("设置传输消息头DataLen: %d", msg.Head.DataLen)

	// 序列化消息
	data, err := msg.Marshal()
	if err != nil {
		// log.Printf("序列化传输消息失败: %v", err)
	} else {
		// log.Printf("序列化后完整传输消息(长度: %d): %X", len(data), data[:min(50, len(data))])
	}
	return data
}

func parseMessage(message []byte, msgLength int) (int, uint16, uint32, uint16, string) {
	// log.Printf("解析非AES加密的消息 - 消息长度: %d", msgLength)

	// 检查消息长度是否足够解析头部
	if msgLength < proto.RESPONSE_HEAD_LEN {
		// log.Printf("消息长度不足以解析头部，需要至少 %d 字节，实际: %d 字节",
		// 	proto.RESPONSE_HEAD_LEN, msgLength)
		return 0, 0, 0, 0, ""
	}

	// log.Printf("消息原始数据(前50字节): %X", message[:min(50, msgLength)])

	// 解析消息头
	msg := new(proto.UdpResponseMessage)
	err := msg.Head.UnMarshal(message[:proto.RESPONSE_HEAD_LEN])
	if err != nil {
		// log.Printf("解析消息头失败: %v", err)
		return 0, 0, 0, 0, ""
	}

	// 验证消息头的有效性
	if msg.Head.Tag != proto.HEAD_TAG {
		// log.Printf("无效的消息头标签: 0x%X, 期望: 0x%X", msg.Head.Tag, proto.HEAD_TAG)
		return 0, 0, 0, 0, ""
	}

	// log.Printf("解析响应头 - Tag: 0x%X, Version: %d, Command: %d, Result: %d, DataLen: %d",
	// 	msg.Head.Tag, msg.Head.Version, msg.Head.Command, msg.Head.Result, msg.Head.DataLen)

	// 计算消息总长度
	msglen := int(msg.Head.DataLen) + proto.RESPONSE_HEAD_LEN
	// log.Printf("计算消息总长度: %d", msglen)

	// 检查消息长度是否足够
	if msglen > msgLength {
		// log.Printf("消息长度不足，需要: %d, 实际: %d", msglen, msgLength)
		return 0, 0, 0, 0, ""
	}

	// 解析消息体
	if int(msg.Head.DataLen) > 0 {
		// 若请求头中len大于0，说明有body；则将body整合到UdpResponseMessage中
		err = msg.ParseBody(message[0:msglen], int(msg.Head.DataLen))
		if err != nil {
			// log.Printf("解析消息体失败: %v", err)
			return 0, 0, 0, 0, ""
		}

		bodyLength := len(msg.Body)
		// log.Printf("解析消息体 - 长度: %d", bodyLength)

		if bodyLength > 0 {
			bodyStr := string(msg.Body)
			bodyStrLen := len(bodyStr)
			if bodyStrLen > 0 {
				// log.Printf("消息体内容(前100字节): %s", bodyStr[:min(100, bodyStrLen)])
				return msglen, msg.Head.Command, msg.Head.DataLen, msg.Head.Result, bodyStr
			}
		}
	} else {
		// log.Printf("消息没有消息体")
	}

	return msglen, msg.Head.Command, msg.Head.DataLen, msg.Head.Result, ""
}

func (c *TransferClient) parseMessageByAES(message []byte, length int, initAESKey string) (int, uint16, uint32, uint16, []byte) {
	resp := &proto.UdpResponseMessage{}
	resp.ParseHead(message[:proto.RESPONSE_HEAD_LEN])
	// log.Printf("解析响应头 - Tag: %d, Version: %d, Command: %d, Result: %d, DataLen: %d",
	// 	resp.Head.Tag, resp.Head.Version, resp.Head.Command, resp.Head.Result, resp.Head.DataLen)

	if resp.Head.DataLen > 0 {
		resp.ParseBody(message, int(resp.Head.DataLen))
		if len(resp.Body) > 0 {
			// log.Printf("响应体加密数据(长度: %d): %X", len(resp.Body), resp.Body[:min(50, len(resp.Body))])
			// log.Printf("解密密钥(长度: %d): %s", len(initAESKey), initAESKey)

			decryptedBody, err := utils.DecryptAES([]byte(initAESKey), resp.Body)
			if err != nil {
				// log.Printf("解密响应体失败: %v", err)
				return resp.Head.Len(), resp.Head.Command, resp.Head.DataLen, resp.Head.Result, nil
			}

			// log.Printf("解密后响应体(长度: %d): %X", len(decryptedBody), decryptedBody[:min(50, len(decryptedBody))])
			return resp.Head.Len(), resp.Head.Command, resp.Head.DataLen, resp.Head.Result, decryptedBody
		}
	}

	return resp.Head.Len(), resp.Head.Command, resp.Head.DataLen, resp.Head.Result, nil
}

// 获取本地 IPv4 地址
func getLocalIPv4() (net.IP, error) {
	// 获取所有网络接口
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("获取网络接口失败: %v", err)
	}

	// 遍历所有网络接口
	for _, iface := range interfaces {
		// 跳过禁用的接口
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		// 跳过回环接口
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// 获取接口的地址
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		// 遍历所有地址
		for _, addr := range addrs {
			// 检查是否是 IP 网络
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// 获取 IPv4 地址
			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}

			// 跳过回环地址
			if ip[0] == 127 {
				continue
			}

			// 跳过链路本地地址
			if ip[0] == 169 && ip[1] == 254 {
				continue
			}

			return ip, nil
		}
	}

	return nil, fmt.Errorf("未找到合适的IPv4地址")
}

// 检查网络连接
func checkNetworkConnectivity(host string, port string) error {
	// 尝试使用TCP连接检查网络可达性
	address := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("网络连接检查失败: %v", err)
	}
	conn.Close()
	return nil
}

// 检查UDP连接
func checkUDPConnectivity(host string, port string) error {
	// 解析地址
	address := net.JoinHostPort(host, port)
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("解析UDP地址失败: %v", err)
	}

	// 创建UDP连接
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("创建UDP连接失败: %v", err)
	}
	defer conn.Close()

	// 发送测试数据
	_, err = conn.Write([]byte("test"))
	if err != nil {
		return fmt.Errorf("发送UDP测试数据失败: %v", err)
	}

	return nil
}

// 辅助函数，返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
