# QUIC Gateway Client

这是一个基于QUIC协议的传输客户端模块，提供了简单的接口用于与服务器进行通信。

## 功能特性

- 基于QUIC的可靠传输
- 支持AES加密通信
- 简单易用的客户端API
- 完整的错误处理
- 支持超时控制
- 详细的日志输出

## 安装

确保你的系统已安装Go 1.20或更高版本。

```bash
# 克隆项目
git clone <your-repository-url>
cd quic_gwclient

# 安装依赖
go mod download
```

## 编译

```bash
go build -o quic_client main.go
```

## 使用示例

```go
package main

import (
    "context"
    "log"
    "time"

    "quic_gwclient/pkg/client"
)

func main() {
    // 创建客户端配置
    config := &client.Config{
        ServerID:   8903,
        ServerName: "stresss_H5_nginx",
        SessionID:  "abac17fd-e8e0-4600-b822-09f5755148d7",
    }

    // 创建客户端
    c := client.NewTransferClient("10.10.27.129:8002", config)

    // 连接服务器
    ctx := context.Background()
    if err := c.Connect(ctx); err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    // 发送初始化请求
    if err := c.SendInitRequest(); err != nil {
        log.Fatal(err)
    }

    // 发送传输请求
    content := "GET /index.html HTTP/1.1\r\n..."
    response, err := c.SendTransferRequest(content)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("收到响应: %s", string(response))
}
```

## API文档

### TransferClient

主要的客户端结构体，用于处理与服务器的通信。

#### 创建新客户端

```go
func NewTransferClient(serverAddr string, config *Config) *TransferClient
```

参数:
- serverAddr: 服务器地址，格式为"host:port"
- config: 客户端配置

#### 连接服务器

```go
func (c *TransferClient) Connect(ctx context.Context) error
```

建立与服务器的QUIC连接。

#### 发送初始化请求

```go
func (c *TransferClient) SendInitRequest() error
```

向服务器发送初始化请求。

#### 发送传输请求

```go
func (c *TransferClient) SendTransferRequest(content string) ([]byte, error)
```

发送传输请求并返回服务器响应。

#### 关闭连接

```go
func (c *TransferClient) Close() error
```

关闭与服务器的连接。

### Config

客户端配置结构体。

```go
type Config struct {
    ServerID   int    // 服务器ID
    ServerName string // 服务器名称
    SessionID  string // 会话ID
}
```

## 依赖

- github.com/google/uuid v1.3.0
- github.com/quic-go/quic-go v0.41.0

## 许可证

MIT License 