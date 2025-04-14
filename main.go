package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/laotiannai/quic_gwclient/pkg/client"
)

func main() {
	// 启用调试模式
	client.SetDebugMode(true)

	// 设置日志格式
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetPrefix("[QUIC Client] ")
	log.Println("=== QUIC客户端启动 ===")
	log.Println("详细日志模式已启用")

	// 创建客户端配置
	config := &client.Config{
		ServerID:   381634,
		ServerName: "stresss_H5_nginx",
		SessionID:  "abac17fd-e8e0-4600-b822-09f5755148d7",
		// 设置重试参数
		// EnableConnectRetry: false,
		MaxRetries:    15,                     // 最大重试15次
		RetryDelay:    500 * time.Millisecond, // 每次重试延迟500ms
		RetryInterval: 2 * time.Second,        // 重试间隔2s
	}
	log.Printf("客户端配置 - ServerID: %d, ServerName: %s, SessionID: %s, MaxRetries: %d",
		config.ServerID, config.ServerName, config.SessionID, config.MaxRetries)

	// 创建客户端
	serverAddr := "10.10.27.129:8002"
	log.Printf("目标服务器地址: %s", serverAddr)
	c := client.NewTransferClient(serverAddr, config)

	// 设置上下文，支持超时和取消
	timeout := 30 * time.Second
	log.Printf("设置连接超时时间: %v", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 连接服务器，使用配置的重试参数
	var err error
	log.Printf("开始连接服务器，最大重试次数: %d", config.MaxRetries)
	for i := 0; i < config.MaxRetries; i++ {
		log.Printf("===== 尝试连接服务器 (尝试 %d/%d) =====", i+1, config.MaxRetries)
		startTime := time.Now()
		err = c.Connect(ctx)
		elapsedTime := time.Since(startTime)

		if err == nil {
			log.Printf("连接服务器成功！耗时: %v", elapsedTime)
			break
		}

		log.Printf("连接失败: %v, 耗时: %v", err, elapsedTime)
		if i < config.MaxRetries-1 {
			log.Printf("将在 %v 后重试...", config.RetryInterval)
			time.Sleep(config.RetryInterval)
		}
	}

	if err != nil {
		log.Fatalf("连接服务器失败，已尝试 %d 次: %v", config.MaxRetries, err)
	}
	defer c.Close()

	// 发送初始化请求
	log.Println("===== 开始发送初始化请求 =====")
	startTime := time.Now()
	sentBytes, receivedBytes, err := c.SendInitRequestNoAES()
	if err != nil {
		log.Fatalf("初始化请求失败: %v", err)
	}
	log.Printf("初始化请求成功，耗时: %v, 发送: %d 字节, 接收: %d 字节", time.Since(startTime), sentBytes, receivedBytes)

	// 发送传输请求
	log.Println("===== 开始发送传输请求 =====")
	content := "GET /index.html HTTP/1.1\r\n" +
		"User-Agent: PostmanRuntime/7.26.8\r\n" +
		"Accept: */*\r\n" +
		"Postman-Token: d2aeeecc-1612-4518-94ef-e882b0767b44\r\n" +
		"Host: 192.168.247.111:8089\r\n" +
		"Accept-Encoding: gzip\r\n" +
		"Connection: close\r\n\r\n"
	log.Printf("传输请求内容:\n%s", content)

	// startTime = time.Now()
	// response, sentBytes, receivedBytes, err := c.SendTransferRequestNoAES(content)
	// if err != nil {
	// 	log.Fatalf("传输请求失败: %v", err)
	// }
	// log.Printf("传输请求成功，耗时: %v, 发送: %d 字节, 接收: %d 字节", time.Since(startTime), sentBytes, receivedBytes)

	// log.Printf("收到响应 (长度: %d 字节):\n%s", len(response), string(response))

	// // 分析HTTP响应
	// if len(response) > 0 {
	// 	log.Println("===== HTTP响应分析 =====")
	// 	analyzeHTTPResponse(response)
	// }

	// 示例3：使用简化的下载方法
	filePath, err := c.DownloadFile(content, "./downloads", "simple")
	if err != nil {
		log.Fatalf("简化下载失败: %v", err)
	}

	log.Printf("文件已保存到: %s", filePath)

	// 等待中断信号
	log.Println("客户端运行中，按Ctrl+C退出...")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("程序正在退出...")
}

// 分析HTTP响应
func analyzeHTTPResponse(response []byte) {
	responseStr := string(response)

	// 查找HTTP状态行
	var statusLine string
	var headers string
	var body string

	// 分离状态行、头部和正文
	parts := splitHTTPResponse(responseStr)
	if len(parts) >= 1 {
		statusLine = parts[0]
		log.Printf("HTTP状态行: %s", statusLine)
	}

	if len(parts) >= 2 {
		headers = parts[1]
		log.Printf("HTTP头部:\n%s", headers)
	}

	if len(parts) >= 3 {
		body = parts[2]
		if len(body) > 200 {
			log.Printf("HTTP正文 (前200字节):\n%s...", body[:200])
		} else {
			log.Printf("HTTP正文:\n%s", body)
		}
	}
}

// 分离HTTP响应的状态行、头部和正文
func splitHTTPResponse(response string) []string {
	result := make([]string, 0, 3)

	// 查找第一个换行符，分离状态行
	statusEnd := -1
	for i := 0; i < len(response); i++ {
		if response[i] == '\r' && i+1 < len(response) && response[i+1] == '\n' {
			statusEnd = i
			break
		}
	}

	if statusEnd != -1 {
		result = append(result, response[:statusEnd])

		// 查找头部和正文的分隔
		headerEnd := -1
		for i := statusEnd + 2; i < len(response)-3; i++ {
			if response[i] == '\r' && response[i+1] == '\n' &&
				response[i+2] == '\r' && response[i+3] == '\n' {
				headerEnd = i
				break
			}
		}

		if headerEnd != -1 {
			result = append(result, response[statusEnd+2:headerEnd])
			result = append(result, response[headerEnd+4:])
		} else {
			result = append(result, response[statusEnd+2:])
		}
	} else {
		result = append(result, response)
	}

	return result
}
