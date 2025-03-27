package client

import (
	"context"
	"log"
	"os"
	"time"
)

// DownloadExample 演示如何使用下载功能
func DownloadExample() {
	// 创建客户端配置
	config := &Config{
		ServerID:   8903,
		ServerName: "test_server",
		SessionID:  "session-123456",
		MaxRetries: 2,
		RetryDelay: time.Second,
	}

	// 创建客户端实例
	serverAddr := "example.com:8002" // 替换为实际服务器地址
	client := NewTransferClient(serverAddr, config)

	// 连接服务器
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		log.Fatalf("连接服务器失败: %v", err)
	}
	defer client.Close()

	// 发送初始化请求
	sentBytes, receivedBytes, err := client.SendInitRequestNoAES()
	if err != nil {
		log.Fatalf("初始化请求失败: %v", err)
	}
	log.Printf("初始化成功，发送: %d 字节，接收: %d 字节", sentBytes, receivedBytes)

	// 准备HTTP请求
	httpRequest := "GET /large-file.bin HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"User-Agent: QuicGatewayClient/1.0\r\n" +
		"Accept: */*\r\n" +
		"Connection: close\r\n\r\n"

	log.Println("开始下载文件...")

	// 示例1：处理服务器切包返回的情况
	options := DefaultDownloadOptions()
	// 增大重试次数以处理切包情况
	options.MaxRetries = 3
	// 设置更长的读取超时时间
	options.ReadTimeout = 60 * time.Second

	log.Println("示例1: 处理服务器切包返回")
	result, err := client.SendTransferRequestWithDownload(httpRequest, options)
	if err != nil {
		log.Fatalf("下载失败: %v", err)
	}

	log.Printf("下载成功，接收数据大小: %d 字节, MD5: %s", len(result.PureData), result.MD5Sum)
	log.Printf("发送字节: %d, 接收字节: %d", result.SentBytes, result.ReceivedBytes)

	// 如果是HTTP响应，显示HTTP信息
	if result.HTTPInfo != nil && result.HTTPInfo.IsHTTP {
		log.Printf("检测到HTTP响应: 状态码=%d, 响应体大小=%d 字节",
			result.HTTPInfo.StatusCode, len(result.HTTPInfo.Body))
		log.Printf("响应头: %v", result.HTTPInfo.Headers)
	}

	// 示例2：下载HTTP响应并只保存响应体
	saveOptions := DefaultDownloadOptions()
	saveOptions.SaveToFile = true           // 启用文件保存
	saveOptions.SaveDir = "./downloads"     // 保存目录
	saveOptions.FileNamePrefix = "document" // 文件名前缀
	saveOptions.DetectHTTP = true           // 启用HTTP检测（默认为true）

	log.Println("示例2: HTTP响应下载，仅保存响应体")
	httpDownloadRequest := "GET /document.pdf HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"User-Agent: QuicGatewayClient/1.0\r\n" +
		"Accept: */*\r\n" +
		"Connection: close\r\n\r\n"

	saveResult, err := client.SendTransferRequestWithDownload(httpDownloadRequest, saveOptions)
	if err != nil {
		log.Fatalf("下载并保存失败: %v", err)
	}

	if saveResult.FilePath != "" {
		log.Printf("文件已保存到: %s", saveResult.FilePath)
		// 查看文件大小
		fileInfo, err := os.Stat(saveResult.FilePath)
		if err == nil {
			log.Printf("保存的文件大小: %d 字节 (HTTP响应体大小: %d 字节)",
				fileInfo.Size(), len(saveResult.HTTPInfo.Body))
		}
	}

	// 示例3：下载非HTTP协议数据
	rawOptions := DefaultDownloadOptions()
	rawOptions.SaveToFile = true
	rawOptions.SaveDir = "./downloads"
	rawOptions.FileNamePrefix = "binary"
	// 禁用HTTP检测，直接将所有数据视为二进制保存
	rawOptions.DetectHTTP = false

	log.Println("示例3: 下载非HTTP协议数据")
	binaryRequest := "BINARY_PROTOCOL\r\n" +
		"Command: DOWNLOAD\r\n" +
		"Target: data.bin\r\n\r\n"

	binaryResult, err := client.SendTransferRequestWithDownload(binaryRequest, rawOptions)
	if err != nil {
		log.Printf("二进制下载失败: %v", err)
	} else {
		log.Printf("二进制数据已保存到: %s", binaryResult.FilePath)
	}

	// 示例4：使用简化的下载方法，自动检测HTTP
	log.Println("示例4: 使用简化的下载方法")
	filePath, err := client.DownloadFile(httpRequest, "./downloads", "simple", true)
	if err != nil {
		log.Fatalf("简化下载失败: %v", err)
	}

	log.Printf("文件已保存到: %s", filePath)

	// 示例5：下载大型文件并监控进度
	log.Println("示例5: 大型文件下载")
	largeFileRequest := "GET /very-large-file.bin HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"User-Agent: QuicGatewayClient/1.0\r\n" +
		"Accept: */*\r\n" +
		"Connection: close\r\n\r\n"

	startTime := time.Now()
	largeOptions := DefaultDownloadOptions()
	largeOptions.ReadTimeout = 120 * time.Second // 更长的超时时间用于大文件
	largeResult, err := client.SendTransferRequestWithDownload(largeFileRequest, largeOptions)
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("大文件下载失败: %v", err)
	} else {
		// 计算下载速度
		downloadSpeed := float64(largeResult.ReceivedBytes) / duration.Seconds() / 1024 // KB/s
		log.Printf("大文件下载完成: %d 字节, 用时: %v, 速度: %.2f KB/s",
			largeResult.ReceivedBytes, duration, downloadSpeed)

		// 如果检测到HTTP协议
		if largeResult.HTTPInfo != nil && largeResult.HTTPInfo.IsHTTP {
			log.Printf("检测到HTTP协议，响应体大小: %d 字节", len(largeResult.HTTPInfo.Body))
		}
	}
}
