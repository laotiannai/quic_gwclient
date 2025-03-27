# QUIC Gateway Client

这是一个基于QUIC协议的传输客户端模块，提供了简单的接口用于与服务器进行通信。

## 功能特性

- 基于QUIC的可靠传输
- 支持AES加密通信
- 简单易用的客户端API
- 完整的错误处理
- 支持超时控制
- 详细的日志输出

## 在其他项目中使用

### 1. 添加依赖

#### 方法一：使用 go get（推荐）

在你的项目目录下运行以下命令获取最新版本：

```bash
go get github.com/laotiannai/quic_gwclient@latest
```

指定具体版本号（推荐在生产环境中使用固定版本）：

```bash
go get github.com/laotiannai/quic_gwclient@v1.0.0  # 请使用实际最新版本
```

#### 方法二：手动添加到 go.mod

在你的项目的 `go.mod` 文件中添加以下依赖：

```bash
require github.com/laotiannai/quic_gwclient latest  # 使用最新版本
```

或指定具体版本（推荐）：

```bash
require github.com/laotiannai/quic_gwclient v1.0.0  # 请使用实际最新版本
```

然后执行：

```bash
go mod tidy
```

#### 方法三：使用特定分支或提交（适用于测试新功能）

如需引用开发中的特定分支或提交：

```bash
# 引用特定分支
go get github.com/laotiannai/quic_gwclient@branch_name

# 引用特定提交
go get github.com/laotiannai/quic_gwclient@commit_hash
```

### 2. 导入包

在你的代码中导入 quic_gwclient 包：

```go
import "github.com/laotiannai/quic_gwclient/pkg/client"
```

使用别名以简化代码：

```go
import quic "github.com/laotiannai/quic_gwclient/pkg/client"
```

### 3. 使用示例

#### 基本用法

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/laotiannai/quic_gwclient/pkg/client"
)

func main() {
    // 设置日志
    log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
    
    // 创建客户端配置
    config := &client.Config{
        ServerID:   8903,
        ServerName: "stresss_H5_nginx",
        SessionID:  "abac17fd-e8e0-4600-b822-09f5755148d7",
    }

    // 创建客户端实例
    serverAddr := "your.server.address:port"
    c := client.NewTransferClient(serverAddr, config)

    // 设置连接超时
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // 连接服务器
    if err := c.Connect(ctx); err != nil {
        log.Printf("连接失败: %v", err)
    }
    defer c.Close()

    // 发送初始化请求（根据需要选择加密或非加密方式）
    sentBytes, recvBytes, err := c.SendInitRequestNoAES()
    if err != nil {
        log.Printf("初始化请求失败: %v", err)
    }
    log.Printf("初始化请求成功，发送: %d 字节，接收: %d 字节", sentBytes, recvBytes)

    // 准备HTTP请求内容
    content := "GET /index.html HTTP/1.1\r\n" +
        "Host: example.com\r\n" +
        "Connection: close\r\n\r\n"

    // 发送传输请求（根据需要选择加密或非加密方式）
    response, sentBytes, recvBytes, err := c.SendTransferRequestNoAES(content)
    if err != nil {
        log.Printf("传输请求失败: %v", err)
    }

    log.Printf("传输请求成功，发送: %d 字节，接收: %d 字节", sentBytes, recvBytes)
    log.Printf("收到响应: %s", string(response))
}
```

#### 带重试机制的使用示例

```go
func connectWithRetry(c *client.TransferClient, ctx context.Context) error {
    maxRetries := 3
    for i := 0; i < maxRetries; i++ {
        err := c.Connect(ctx)
        if err == nil {
            return nil
        }
        
        log.Printf("连接失败 (尝试 %d/%d): %v", i+1, maxRetries, err)
        if i < maxRetries-1 {
            time.Sleep(time.Duration(i+1) * 2 * time.Second)
        }
    }
    return fmt.Errorf("连接失败，已重试%d次", maxRetries)
}
```

### 4. 数据校验

为确保数据传输的正确性，你可以：

1. 检查响应状态码：
```go
if len(response) > 0 {
    responseStr := string(response)
    if strings.Contains(responseStr, "200 OK") {
        log.Println("请求成功")
    } else {
        log.Printf("请求失败: %s", responseStr)
    }
}
```

2. 验证响应数据完整性：
```go
func validateResponse(response []byte) bool {
    // 检查响应是否包含完整的HTTP头部
    if !bytes.Contains(response, []byte("\r\n\r\n")) {
        return false
    }
    
    // 如果响应包含Content-Length头部，验证实际内容长度
    headers := string(bytes.Split(response, []byte("\r\n\r\n"))[0])
    if matches := regexp.MustCompile(`Content-Length: (\d+)`).FindStringSubmatch(headers); len(matches) > 1 {
        expectedLength, _ := strconv.Atoi(matches[1])
        actualLength := len(bytes.Split(response, []byte("\r\n\r\n"))[1])
        return expectedLength == actualLength
    }
    
    return true
}
```

## 安装

确保你的系统已安装Go 1.23或更高版本。

```bash
# 克隆项目
git clone https://github.com/laotiannai/quic_gwclient.git
cd quic_gwclient

# 安装依赖
go mod download
```

## 编译

```bash
go build -o quic_client main.go
```

## API文档

### TransferClient

主要的客户端结构体，用于处理与服务器的通信。

#### 创建新客户端

```go
func NewTransferClient(serverAddr string, config *Config) *TransferClient
```

参数:
- `serverAddr`: 服务器地址，格式为"host:port"
- `config`: 客户端配置对象

返回值:
- `*TransferClient`: 新创建的客户端实例

#### 连接服务器

```go
func (c *TransferClient) Connect(ctx context.Context) error
```

建立与服务器的QUIC连接。

参数:
- `ctx`: 上下文对象，可用于设置超时和取消操作

返回值:
- `error`: 如果连接成功返回nil，否则返回错误信息

#### 发送初始化请求

```go
func (c *TransferClient) SendInitRequest() error
func (c *TransferClient) SendInitRequestNoAES() (int, int, error)
```

`SendInitRequestNoAES` 返回发送字节数、接收字节数以及可能的错误。

#### 发送传输请求

```go
func (c *TransferClient) SendTransferRequest(content string) ([]byte, error)
func (c *TransferRequestNoAES` 返回响应数据、发送字节数、接收字节数以及可能的错误。

参数:
- `content`: 要发送的请求内容，通常是HTTP请求字符串

返回值:
- `[]byte`: 服务器的响应数据
- `int`: 发送的字节数
- `int`: 接收的字节数
- `error`: 如果请求成功返回nil，否则返回错误信息

#### 大文件下载功能

```go
func (c *TransferClient) SendTransferRequestWithDownload(content string, options *DownloadOptions) (*DownloadResult, error)
func (c *TransferClient) SendTransferRequestWithAESDownload(content string, options *DownloadOptions, initAESKey string) (*DownloadResult, error)
func (c *TransferClient) DownloadFile(content string, saveDir string, fileNamePrefix string, saveToFile bool) (string, error)
```

这些方法用于处理大型数据传输和文件下载：

- `SendTransferRequestWithDownload`: 发送请求并支持大型数据下载，可选择是否保存为文件
- `SendTransferRequestWithAESDownload`: 使用AES加密方式下载
- `DownloadFile`: 发送请求并将响应直接保存为文件（便捷方法）

参数:
- `content`: 要发送的请求内容
- `options`: 下载选项，控制保存行为、重试次数等
- `initAESKey`: AES加密方式使用的密钥
- `saveDir`: 文件保存目录
- `fileNamePrefix`: 保存文件名前缀
- `saveToFile`: 是否将响应保存为文件

返回值:
- `*DownloadResult`: 下载结果，包含原始数据、处理后数据、字节统计和MD5值
- `string`: 保存的文件路径（仅DownloadFile方法）
- `error`: 如果下载成功返回nil，否则返回错误信息

#### 下载选项与结果

```go
type DownloadOptions struct {
    SaveToFile      bool    // 是否将响应保存为本地文件
    SaveDir         string  // 下载文件保存目录
    FileNamePrefix  string  // 文件名前缀
    MaxDownloadSize int64   // 最大下载大小（字节）
    MaxRetries      int     // 重试次数
    ReadTimeout     time.Duration // 读取超时时间
    DetectHTTP      bool    // 是否自动检测HTTP协议
}

type DownloadResult struct {
    RawData         []byte  // 原始响应数据（包括头部）
    PureData        []byte  // 纯净响应数据（不包括头部）
    SentBytes       int     // 发送的字节数
    ReceivedBytes   int     // 接收的字节数 
    FilePath        string  // 保存的文件路径（如果设置了SaveToFile）
    MD5Sum          string  // 文件的MD5值
    HTTPInfo        *HTTPResponseInfo // HTTP响应信息（如果是HTTP协议）
}

func DefaultDownloadOptions() *DownloadOptions
```

`DefaultDownloadOptions` 返回默认的下载选项配置。

**重要说明**:
1. `SaveToFile` 参数控制是否将响应保存为本地文件：
   - 设置为 `true` 时，会将响应保存到本地文件
   - 设置为 `false` 时，不会保存文件，而是返回带有 "memory:" 前缀的虚拟路径
2. 当 `DetectHTTP` 为 `true` 时，会自动检测和解析HTTP协议
3. 文件路径格式：
   - 保存文件时：`SaveDir/FileNamePrefix_MD5.bin`
   - 不保存文件时：`memory:SaveDir/FileNamePrefix_MD5.bin`

#### 关闭连接

```go
func (c *TransferClient) Close() error
```

关闭与服务器的连接并释放资源。

返回值:
- `error`: 如果关闭成功返回nil，否则返回错误信息

### Config

客户端配置结构体。

```go
type Config struct {
    ServerID   int    // 服务器ID，用于标识目标服务器
    ServerName string // 服务器名称，用于服务器识别
    SessionID  string // 会话ID，用于跟踪单个连接会话
}
```

### 错误处理

客户端可能返回的错误类型：

1. 连接错误
```go
if err := client.Connect(ctx); err != nil {
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        // 处理连接超时
    case errors.Is(err, context.Canceled):
        // 处理连接被取消
    default:
        // 处理其他连接错误
    }
}
```

2. 传输错误
```go
if _, err := client.SendTransferRequest(content); err != nil {
    switch {
    case strings.Contains(err.Error(), "connection refused"):
        // 处理服务器拒绝连接
    case strings.Contains(err.Error(), "timeout"):
        // 处理请求超时
    default:
        // 处理其他传输错误
    }
}
```

## 依赖

该项目使用以下主要依赖：

- github.com/quic-go/quic-go v0.50.1
- github.com/google/uuid v1.6.0

## 许可证

MIT License

## 贡献指南

欢迎提交 Issue 和 Pull Request 来改进这个项目。在提交 PR 之前，请确保：

1. 代码符合 Go 的代码规范
2. 添加了必要的测试用例
3. 所有测试都能通过
4. 更新了相关文档

## 版本历史

### v1.0.0 (最新稳定版)

- 初始稳定版本发布
- 支持基本的QUIC通信功能
- 提供加密和非加密两种通信方式
- 包含完整的错误处理和重试机制

### 开发版

当前开发中的功能：

- 性能优化
- 更多的安全选项
- 改进的API接口

## 兼容性

- Go 1.23或更高版本
- 支持Windows/Linux/macOS平台
- 依赖：
  - github.com/quic-go/quic-go v0.50.1
  - github.com/google/uuid v1.6.0

## 获取帮助

如果在使用过程中遇到问题，可以：

1. 查看详细的[使用说明文档](./使用说明.md)
2. 提交Issue报告问题
3. 通过Pull Request贡献代码或文档改进

## 更新历史

### 版本 1.0.5 (2024-XX-XX)

- **新功能**: 增强了大文件下载功能
  - 支持自动处理服务器端切包返回的场景
  - 增加了HTTP协议自动检测和解析
  - 在保存文件时可以智能提取HTTP响应体
  - 支持HTTP分块传输编码的自动解析
  - 提供设置读取超时和重试次数的选项
  - 优化了下载过程中的内存使用

### 版本 1.0.4 (2024-XX-XX)

- **新功能**: 添加了大文件下载支持
  - 新增 `SendTransferRequestWithDownload` 和 `SendTransferRequestWithAESDownload` 方法
  - 支持自动处理数据接收和包重组
  - 支持下载内容保存为本地文件
  - 包含 MD5 校验和字节统计
  - 提供默认最大 4GB 的下载大小限制
  - 自动重试机制确保完整接收数据
  - 添加了 `DownloadFile` 便捷方法

### 版本 1.0.3 (2024-XX-XX)

- **新功能**: 添加了发送和接收字节数统计功能
  - `SendInitRequestNoAES` 现在返回 `(int, int, error)`，分别表示发送字节数、接收字节数和错误
  - `SendTransferRequestNoAES` 现在返回 `([]byte, int, int, error)`，分别表示响应数据、发送字节数、接收字节数和错误
  - 可用于网络流量分析、性能监控和网络诊断

- **改进**: RequestResult 结构体现在包含 SentBytes 和 ReceivedBytes 字段，用于跟踪总流量

### 版本 1.0.0 (历史版本)

- 初始版本发布
- 提供基本的 QUIC 通信功能 