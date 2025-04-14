# QUIC客户端模块

这个模块提供了一个简单的API，用于在其他Go项目中使用QUIC客户端功能。

## 功能特点

- 支持配置服务器IP、端口、超时时间和重试次数
- 支持从IPSServerInfo结构体中读取配置
- 支持响应断言验证
- 提供详细的请求结果，包括成功状态、响应内容、错误信息和耗时

## 使用方法

### 1. 导入模块

```go
import "your-project-path/pkg/client"
```

### 2. 直接使用RequestOptions发送请求

```go
// 创建请求选项
opts := client.DefaultRequestOptions()
opts.ServerIP = "10.10.27.129"
opts.ServerPort = "8002"
opts.ServerID = 8903
opts.ServerName = "stresss_H5_nginx"
opts.SessionID = "abac17fd-e8e0-4600-b822-09f5755148d7"
opts.ConnectTimeout = 30 * time.Second
opts.ReadTimeout = 10 * time.Second
opts.MaxRetries = 3
opts.EnableConnectRetry = false // 禁用连接重试（默认）
opts.MessageContent = "GET /index.html HTTP/1.1\r\n" +
    "User-Agent: PostmanRuntime/7.26.8\r\n" +
    "Accept: */*\r\n" +
    "Connection: close\r\n\r\n"
opts.ResponseAssertion = "HTTP"

// 发送请求
result := client.SendQuicRequest(opts)
if result.Error != nil {
    log.Printf("请求失败: %v", result.Error)
} else {
    log.Printf("请求成功！耗时: %v", result.ElapsedTime)
    log.Printf("响应断言结果: %v", result.AssertionResult)
    log.Printf("响应内容 (长度: %d 字节):\n%s", len(result.ResponseBytes), result.Response)
}
```

### 启用连接重试的配置

```go
// 创建请求选项，启用连接重试
opts := client.DefaultRequestOptions()
opts.ServerIP = "10.10.27.129"
opts.ServerPort = "8002"
opts.ServerID = 8903
opts.ServerName = "stresss_H5_nginx"
opts.SessionID = "abac17fd-e8e0-4600-b822-09f5755148d7"
opts.ConnectTimeout = 30 * time.Second
opts.ReadTimeout = 10 * time.Second
opts.MaxRetries = 3
opts.EnableConnectRetry = true // 启用连接重试
opts.MessageContent = "GET /index.html HTTP/1.1\r\n" +
    "User-Agent: PostmanRuntime/7.26.8\r\n" +
    "Accept: */*\r\n" +
    "Connection: close\r\n\r\n"
opts.ResponseAssertion = "HTTP"
```

### 3. 从IPSServerInfo发送请求

```go
// 创建IPSServerInfo
ipsInfo := &client.IPSServerInfo{
    ServerID:       62,
    ServerName:     "testapp64",
    SessionID:      "si:abac17fd-e8e0-4600-b822-09f5755148d7",
    ResponseAssert: "nginx",
    MessageContent: "GET /index.html HTTP/1.1\r\nUser-Agent: PostmanRuntime/7.26.8\r\nAccept: */*\r\nConnection: close\r\n\r\n",
}

// 发送请求
result := client.SendQuicRequestFromIPSInfo(
    "10.10.27.129",
    "8002",
    30*time.Second,
    10*time.Second,
    3,
    false, // 不启用连接重试
    ipsInfo,
)

if result.Error != nil {
    log.Printf("请求失败: %v", result.Error)
} else {
    log.Printf("请求成功！耗时: %v", result.ElapsedTime)
    log.Printf("响应断言结果: %v", result.AssertionResult)
    log.Printf("响应内容 (长度: %d 字节):\n%s", len(result.ResponseBytes), result.Response)
}
```

### 4. 批量处理多个IPSServerInfo

```go
// 从文件读取IPSServerInfo列表
ipsInfoList, err := readIPSServerInfoFromFile()
if err != nil {
    log.Fatalf("读取IPSServerInfo失败: %v", err)
}

// 批量处理
for i, info := range ipsInfoList {
    log.Printf("处理第 %d 个请求...", i+1)
    result := client.SendQuicRequestFromIPSInfo(
        "10.10.27.129",
        "8002",
        30*time.Second,
        10*time.Second,
        3,
        false, // 不启用连接重试
        info,
    )
    
    if result.Error != nil {
        log.Printf("请求失败: %v", result.Error)
    } else {
        log.Printf("请求成功！耗时: %v", result.ElapsedTime)
        log.Printf("响应断言结果: %v", result.AssertionResult)
        log.Printf("响应内容 (长度: %d 字节):\n%s", len(result.ResponseBytes), result.Response[:100])
    }
}
```

## 数据结构

### RequestOptions

```go
type RequestOptions struct {
    // 服务器地址配置
    ServerIP   string
    ServerPort string
    
    // 超时和重试配置
    ConnectTimeout time.Duration
    ReadTimeout    time.Duration
    MaxRetries     int
    
    // 服务器信息配置
    ServerID       int
    ServerName     string
    SessionID      string
    
    // 请求内容
    MessageContent string
    
    // 响应断言
    ResponseAssertion string
    
    // 其他可选配置
    AppName        string
    Username       string
    ClientAddr     string
    DeviceID       string
    DeviceType     string
    AppVersion     string
    TokenID        string
    JSessionID     string
    Connectors     string
    EnableConnectRetry bool
}
```

### RequestResult

```go
type RequestResult struct {
    Success         bool
    Response        string
    ResponseBytes   []byte
    Error           error
    ElapsedTime     time.Duration
    AssertionResult bool
}
```

### IPSServerInfo

```go
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
```

## 从ipsserverinfo.dat读取数据

您可以使用以下代码从ipsserverinfo.dat文件中读取IPSServerInfo数据：

```go
import (
    "bufio"
    "fmt"
    "os"
    "strconv"
    "strings"
    
    "github.com/laotiannai/quic_gwclient/pkg/client"
)

// ReadIpsServerInfoFile 读取./Ini/ipserverinfo.dat文件的函数
func ReadIpsServerInfoFile() ([]*client.IPSServerInfo, error) {
    var serverInfos []*client.IPSServerInfo

    // 打开文件
    file, err := os.Open("./ini/ipsserverinfo.dat")
    if err != nil {
        return nil, fmt.Errorf("打开文件失败 %s", err.Error())
    }

    defer file.Close()

    // 读取文件内容
    scanner := bufio.NewScanner(file)
    scanner.Scan() // 跳过首行，不处理参数名

    for scanner.Scan() {
        line := scanner.Text()
        if line == "" {
            continue
        }

        textArr := strings.SplitN(line, ",", 15)
        if len(textArr) != 15 {
            fmt.Println(len(textArr))
            fmt.Println(textArr)
            return nil, fmt.Errorf("文件格式不正确，每行应有15个数据")
        }

        serverID, err := strconv.Atoi(textArr[0])
        if err != nil {
            return nil, fmt.Errorf("ServerID 不是有效的整数值")
        }

        serverInfo := &client.IPSServerInfo{
            ServerID:       serverID,
            AppName:        textArr[1],
            ServerName:     textArr[2],
            Username:       textArr[3],
            SessionID:      textArr[4],
            ClientAddr:     textArr[5],
            ServerAddr:     textArr[6],
            DeviceID:       textArr[7],
            DeviceType:     textArr[8],
            AppVersion:     textArr[9],
            TokenID:        textArr[10],
            JSessionId:     textArr[11],
            Connectors:     textArr[12],
            ResponseAssert: textArr[13],
            MessageContent: textArr[14],
        }
        serverInfos = append(serverInfos, serverInfo)
    }

    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("读取文件失败 %s", err.Error())
    }

    return serverInfos, nil
}
``` 