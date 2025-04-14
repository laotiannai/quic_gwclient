package client

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RequestOptions 请求选项配置
type RequestOptions struct {
	// 服务器地址配置
	ServerIP   string
	ServerPort string

	// 超时和重试配置
	ConnectTimeout     time.Duration
	ReadTimeout        time.Duration
	MaxRetries         int
	EnableConnectRetry bool // 是否在连接失败时尝试不同的协议组合

	// 服务器信息配置
	ServerID   int
	ServerName string
	SessionID  string

	// 请求内容
	MessageContent string

	// 响应断言
	ResponseAssertion string

	// 其他可选配置
	AppName    string
	Username   string
	ClientAddr string
	DeviceID   string
	DeviceType string
	AppVersion string
	TokenID    string
	JSessionID string
	Connectors string
}

// RequestResult 请求结果
type RequestResult struct {
	Success         bool
	Response        string
	ResponseBytes   []byte
	Error           error
	ElapsedTime     time.Duration
	AssertionResult bool
	SentBytes       int64
	ReceivedBytes   int64
}

// DefaultRequestOptions 返回默认的请求选项
func DefaultRequestOptions() *RequestOptions {
	return &RequestOptions{
		ServerIP:           "127.0.0.1",
		ServerPort:         "8002",
		ConnectTimeout:     30 * time.Second,
		ReadTimeout:        10 * time.Second,
		MaxRetries:         3,
		EnableConnectRetry: false,
		ServerID:           0,
		ServerName:         "",
		SessionID:          "",
		MessageContent:     "",
		ResponseAssertion:  "",
	}
}

// SendQuicRequest 发送QUIC请求并返回结果
func SendQuicRequest(opts *RequestOptions) *RequestResult {
	startTime := time.Now()
	result := &RequestResult{
		Success: false,
	}

	// 验证必要参数
	if opts.ServerIP == "" || opts.ServerPort == "" {
		result.Error = fmt.Errorf("服务器IP和端口不能为空")
		return result
	}

	if opts.ServerID == 0 || opts.ServerName == "" || opts.SessionID == "" {
		result.Error = fmt.Errorf("ServerID、ServerName和SessionID不能为空")
		return result
	}

	if opts.MessageContent == "" {
		result.Error = fmt.Errorf("请求内容不能为空")
		return result
	}

	// 构建服务器地址
	serverAddr := fmt.Sprintf("%s:%s", opts.ServerIP, opts.ServerPort)

	// 创建客户端配置
	config := &Config{
		ServerID:           opts.ServerID,
		ServerName:         opts.ServerName,
		SessionID:          opts.SessionID,
		EnableConnectRetry: opts.EnableConnectRetry,
	}

	// 创建客户端
	c := NewTransferClient(serverAddr, config)

	// 设置上下文，支持超时和取消
	ctx, cancel := context.WithTimeout(context.Background(), opts.ConnectTimeout)
	defer cancel()

	// 连接服务器，添加重试逻辑
	var err error
	for i := 0; i < opts.MaxRetries; i++ {
		// log.Printf("尝试连接服务器 (尝试 %d/%d)", i+1, opts.MaxRetries)
		err = c.Connect(ctx)

		if err == nil {
			break
		}

		// log.Printf("连接失败: %v", err)
		if i < opts.MaxRetries-1 {
			retryDelay := time.Duration(i+1) * 2 * time.Second
			// log.Printf("将在 %v 后重试...", retryDelay)
			time.Sleep(retryDelay)
		}
	}

	if err != nil {
		result.Error = fmt.Errorf("连接服务器失败，已尝试 %d 次: %v", opts.MaxRetries, err)
		return result
	}
	defer c.Close()

	// 发送初始化请求
	sentInitBytes, receivedInitBytes, err := c.SendInitRequestNoAES()
	if err != nil {
		result.Error = fmt.Errorf("初始化请求失败: %v", err)
		return result
	}

	// 发送传输请求
	response, sentTransBytes, receivedTransBytes, err := c.SendTransferRequestNoAES(opts.MessageContent)
	if err != nil {
		result.Error = fmt.Errorf("传输请求失败: %v", err)
		return result
	}

	// 设置结果
	result.Success = true
	result.ResponseBytes = response
	result.Response = string(response)
	result.ElapsedTime = time.Since(startTime)
	result.SentBytes = int64(sentInitBytes + sentTransBytes)
	result.ReceivedBytes = int64(receivedInitBytes + receivedTransBytes)

	// 检查响应断言
	if opts.ResponseAssertion != "" {
		result.AssertionResult = strings.Contains(result.Response, opts.ResponseAssertion)
	} else {
		result.AssertionResult = true
	}

	return result
}

// SendQuicRequestFromIPSInfo 从IPSServerInfo发送QUIC请求
func SendQuicRequestFromIPSInfo(serverIP string, serverPort string, connectTimeout time.Duration, readTimeout time.Duration, maxRetries int, enableConnectRetry bool, ipsInfo *IPSServerInfo) *RequestResult {
	opts := &RequestOptions{
		ServerIP:           serverIP,
		ServerPort:         serverPort,
		ConnectTimeout:     connectTimeout,
		ReadTimeout:        readTimeout,
		MaxRetries:         maxRetries,
		EnableConnectRetry: enableConnectRetry,
		ServerID:           ipsInfo.ServerID,
		ServerName:         ipsInfo.ServerName,
		SessionID:          ipsInfo.SessionID,
		MessageContent:     ipsInfo.MessageContent,
		ResponseAssertion:  ipsInfo.ResponseAssert,
		AppName:            ipsInfo.AppName,
		Username:           ipsInfo.Username,
		ClientAddr:         ipsInfo.ClientAddr,
		DeviceID:           ipsInfo.DeviceID,
		DeviceType:         ipsInfo.DeviceType,
		AppVersion:         ipsInfo.AppVersion,
		TokenID:            ipsInfo.TokenID,
		JSessionID:         ipsInfo.JSessionId,
		Connectors:         ipsInfo.Connectors,
	}

	return SendQuicRequest(opts)
}

// IPSServerInfo IPS服务器信息结构体
type IPSServerInfo struct {
	ServerID       int
	AppName        string
	ServerName     string
	Username       string
	SessionID      string
	ClientAddr     string
	ServerAddr     string
	DeviceID       string
	DeviceType     string
	AppVersion     string
	TokenID        string
	JSessionId     string
	Connectors     string
	ResponseAssert string
	MessageContent string
}
