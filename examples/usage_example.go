package main

import (
	"fmt"
	"log"
	"time"

	"github.com/laotiannai/quic_gwclient/pkg/client"
)

func main() {
	// 设置日志格式
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetPrefix("[QUIC Client Example] ")
	log.Println("=== QUIC客户端示例启动 ===")

	// 方法1：使用RequestOptions直接发送请求
	log.Println("示例1：使用RequestOptions直接发送请求")
	opts := client.DefaultRequestOptions()
	opts.ServerIP = "10.10.27.129"
	opts.ServerPort = "8002"
	opts.ServerID = 8903
	opts.ServerName = "stresss_H5_nginx"
	opts.SessionID = "abac17fd-e8e0-4600-b822-09f5755148d7"
	opts.ConnectTimeout = 30 * time.Second
	opts.ReadTimeout = 10 * time.Second
	opts.MaxRetries = 3
	opts.MessageContent = "GET /index.html HTTP/1.1\r\n" +
		"User-Agent: PostmanRuntime/7.26.8\r\n" +
		"Accept: */*\r\n" +
		"Postman-Token: d2aeeecc-1612-4518-94ef-e882b0767b44\r\n" +
		"Host: 192.168.247.111:8089\r\n" +
		"Accept-Encoding: gzip\r\n" +
		"Connection: close\r\n\r\n"
	opts.ResponseAssertion = "HTTP"

	result := client.SendQuicRequest(opts)
	if result.Error != nil {
		log.Printf("请求失败: %v", result.Error)
	} else {
		log.Printf("请求成功！耗时: %v", result.ElapsedTime)
		log.Printf("响应断言结果: %v", result.AssertionResult)
		log.Printf("响应内容 (长度: %d 字节):\n%s", len(result.ResponseBytes), result.Response)
	}

	// 方法2：从IPSServerInfo发送请求
	log.Println("\n示例2：从IPSServerInfo发送请求")

	// 模拟从ipsserverinfo.dat读取的数据
	ipsInfo := &client.IPSServerInfo{
		ServerID:       62,
		AppName:        "testapp64",
		ServerName:     "testapp64",
		Username:       "jxp",
		SessionID:      "si:abac17fd-e8e0-4600-b822-09f5755148d7",
		ClientAddr:     "1.1.1.1",
		ServerAddr:     "10.18.13.39:8084",
		DeviceID:       "4ec05a56a1bbdf074fd8908fd6facaab",
		DeviceType:     "android_phone",
		AppVersion:     "5.9.20230529",
		TokenID:        "4318071645f947e59cf019e4b55f9802",
		JSessionId:     "cffd93d1-d3dc-4a0d-a0a6-5bf6cedaae65",
		Connectors:     "192.168.247.144:18081",
		ResponseAssert: "nginx",
		MessageContent: "GET /index.html HTTP/1.1\r\nUser-Agent: PostmanRuntime/7.26.8\r\nAccept: */*\r\nPostman-Token: d2aeeecc-1612-4518-94ef-e882b0767b44\r\nHost: 192.168.247.111:8089\r\nAccept-Encoding: gzip\r\nConnection: close\r\n\r\n",
	}

	// 发送请求
	result = client.SendQuicRequestFromIPSInfo(
		"10.10.27.129",
		"8002",
		30*time.Second,
		10*time.Second,
		3,
		ipsInfo,
	)

	if result.Error != nil {
		log.Printf("请求失败: %v", result.Error)
	} else {
		log.Printf("请求成功！耗时: %v", result.ElapsedTime)
		log.Printf("响应断言结果: %v", result.AssertionResult)
		log.Printf("响应内容 (长度: %d 字节):\n%s", len(result.ResponseBytes), result.Response)
	}

	// 方法3：批量处理多个IPSServerInfo
	log.Println("\n示例3：批量处理多个IPSServerInfo")

	// 模拟从ipsserverinfo.dat读取的多条数据
	ipsInfoList := []*client.IPSServerInfo{
		{
			ServerID:       62,
			ServerName:     "testapp64",
			SessionID:      "si:abac17fd-e8e0-4600-b822-09f5755148d7",
			ResponseAssert: "nginx",
			MessageContent: "GET /index.html HTTP/1.1\r\nUser-Agent: PostmanRuntime/7.26.8\r\nAccept: */*\r\nConnection: close\r\n\r\n",
		},
		{
			ServerID:       63,
			ServerName:     "testapp65",
			SessionID:      "si:bbac17fd-e8e0-4600-b822-09f5755148d8",
			ResponseAssert: "nginx",
			MessageContent: "GET /about.html HTTP/1.1\r\nUser-Agent: PostmanRuntime/7.26.8\r\nAccept: */*\r\nConnection: close\r\n\r\n",
		},
	}

	// 批量处理
	for i, info := range ipsInfoList {
		log.Printf("处理第 %d 个请求...", i+1)
		result = client.SendQuicRequestFromIPSInfo(
			"10.10.27.129",
			"8002",
			30*time.Second,
			10*time.Second,
			3,
			info,
		)

		if result.Error != nil {
			log.Printf("请求失败: %v", result.Error)
		} else {
			log.Printf("请求成功！耗时: %v", result.ElapsedTime)
			log.Printf("响应断言结果: %v", result.AssertionResult)
			fmt.Printf("响应内容 (长度: %d 字节):\n%s\n", len(result.ResponseBytes), result.Response[:100])
		}
	}
}

// 实际项目中，您可以使用以下函数从ipsserverinfo.dat读取数据
// 这里只是示例，实际使用时请替换为您的实际读取函数
func readIPSServerInfoFromFile() ([]*client.IPSServerInfo, error) {
	// 这里应该是您的实际读取逻辑
	// 返回从文件中读取的IPSServerInfo列表
	return nil, nil
}
