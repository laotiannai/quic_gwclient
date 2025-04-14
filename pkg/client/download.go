package client

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/laotiannai/quic_gwclient/proto"
)

// DebugMode 是否启用调试模式
var DebugMode bool = false

// SetDebugMode 设置调试模式
func SetDebugMode(enable bool) {
	DebugMode = enable
}

// debugLog 输出调试日志
func debugLog(format string, args ...interface{}) {
	if DebugMode {
		log.Printf("[QUIC-DEBUG] "+format, args...)
	}
}

// DownloadOptions 下载选项结构体
type DownloadOptions struct {
	// 是否将响应保存为本地文件
	SaveToFile bool
	// 下载文件保存的目录，如果为空则保存到当前目录
	SaveDir string
	// 自定义文件名前缀，最终文件名将是 prefix_md5.bin
	FileNamePrefix string
	// 最大下载大小（字节）
	MaxDownloadSize int64
	// 重试次数
	MaxRetries int
	// 读取超时时间
	ReadTimeout time.Duration
	// 是否自动检测HTTP协议（仅在SaveToFile=true时有效）
	DetectHTTP bool
}

// DefaultDownloadOptions 返回默认的下载选项
func DefaultDownloadOptions() *DownloadOptions {
	return &DownloadOptions{
		SaveToFile:      false,
		SaveDir:         "",
		FileNamePrefix:  "download",
		MaxDownloadSize: 4 * 1024 * 1024 * 1024, // 4GB
		MaxRetries:      2,
		ReadTimeout:     30 * time.Second,
		DetectHTTP:      true,
	}
}

// DownloadResult 下载结果结构体
type DownloadResult struct {
	// 原始响应数据（包括头部）
	RawData []byte
	// 纯净响应数据（不包括头部）
	PureData string
	// 发送的字节数
	SentBytes int
	// 接收的字节数
	ReceivedBytes int
	// 保存的文件路径（如果设置了SaveToFile）
	FilePath string
	// 文件的MD5值
	MD5Sum string
	// HTTP响应信息（如果是HTTP协议）
	HTTPInfo *HTTPResponseInfo
}

// HTTPResponseInfo HTTP响应信息
type HTTPResponseInfo struct {
	// 状态码
	StatusCode int
	// 响应头
	Headers map[string]string
	// 响应体
	Body []byte
	// 是否为HTTP响应
	IsHTTP bool
}

// SendTransferRequestWithDownload 发送传输请求并支持大型数据下载
func (c *TransferClient) SendTransferRequestWithDownload(content string, options *DownloadOptions) (*DownloadResult, error) {
	if options == nil {
		options = DefaultDownloadOptions()
	}

	debugLog("开始下载请求，SaveToFile=%v, DetectHTTP=%v", options.SaveToFile, options.DetectHTTP)

	c.mu.Lock()
	defer c.mu.Unlock()

	result := &DownloadResult{
		SentBytes:     0,
		ReceivedBytes: 0,
	}

	if c.conn == nil {
		return nil, fmt.Errorf("连接未建立")
	}

	if c.conn.Context().Err() != nil {
		debugLog("连接已关闭，尝试重新连接")
		if err := c.Connect(c.conn.Context()); err != nil {
			return nil, fmt.Errorf("重新建立连接失败: %v", err)
		}
	}

	requestInfo := transferRequest(content)
	debugLog("请求数据准备完成，大小: %d 字节", len(requestInfo))

	// 发送请求
	if c.stream == nil {
		debugLog("创建新的数据流")
		stream, err := c.conn.OpenStreamSync(c.conn.Context())
		if err != nil {
			return nil, fmt.Errorf("无法创建流: %v", err)
		}
		c.stream = stream
	}

	n, err := c.stream.Write(requestInfo)
	result.SentBytes += n
	debugLog("已发送请求数据: %d 字节", n)

	if err != nil {
		c.stream.Close()
		c.stream = nil
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}

	// 接收响应
	var totalRawResponse []byte
	var totalPureResponse []byte
	var isComplete bool
	var retries int
	var requireContinue bool = true               // 始终需要继续接收数据，直到明确完成
	var lastReadTime time.Time = time.Now()       // 记录最后一次成功读取的时间
	var noDataTimeThreshold = options.ReadTimeout // 无数据超时阈值
	var noDataCount int = 0                       // 连续没有数据的次数
	var packetCount int = 0                       // 收到的数据包数量

	readTimeout := options.ReadTimeout
	debugLog("设置读取超时: %v, 最大重试次数: %d", readTimeout, options.MaxRetries)

	// 循环读取，直到确定不再有数据或达到最大重试次数
	for !isComplete && retries <= options.MaxRetries {
		if err := c.stream.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			debugLog("设置读取超时失败: %v", err)
		}

		responseBuffer := make([]byte, 8*1024) // 使用更大的缓冲区
		readBytes, err := c.stream.Read(responseBuffer)

		if readBytes > 0 {
			packetCount++
			// 重置连续无数据计数
			noDataCount = 0

			// 更新最后读取时间
			lastReadTime = time.Now()

			result.ReceivedBytes += readBytes
			chunk := responseBuffer[:readBytes]
			totalRawResponse = append(totalRawResponse, chunk...)
			debugLog("收到数据包 #%d: %d 字节, 总计已接收: %d 字节", packetCount, readBytes, result.ReceivedBytes)

			// 直接保存原始数据，不经过解析（避免丢失数据）
			// 此处对chunk进行处理，去除协议包后，并保存纯净数据包
			// respLen, cmd, _, _, body := parseMessage(chunk, readBytes)
			// chunk = chunk[proto.RESPONSE_HEAD_LEN:]

			// 判断是否收到结束连接的标志（这部分逻辑可能需要保留）
			respLen, cmd, _, _, _ := parseMessage(chunk, readBytes)
			body := chunk[proto.RESPONSE_HEAD_LEN:]
			totalPureResponse = append(totalPureResponse, body...)

			if cmd == proto.EMM_COMMAND_LINK_CLOSE {
				debugLog("收到关闭连接命令，停止接收")
				isComplete = true
				break
			}

			// 如果是完整的消息并且不需要继续接收
			if respLen > 0 && respLen <= readBytes && !requireContinue {
				debugLog("收到完整消息且不需要继续接收")
				isComplete = true
				break
			}
		} else {
			// 连续无数据计数增加
			noDataCount++
			debugLog("未收到数据，连续无数据次数: %d", noDataCount)

			// 如果多次读取都没有数据，可能是传输已完成
			if noDataCount >= 3 {
				if time.Since(lastReadTime) > noDataTimeThreshold {
					debugLog("长时间(%v)未收到新数据且连续%d次无数据，认为传输已完成",
						time.Since(lastReadTime), noDataCount)
					isComplete = true
					break
				}
			}
		}

		// 处理读取错误
		if err != nil {
			if err == io.EOF {
				// EOF表示数据传输完成
				debugLog("收到EOF，数据接收完成")
				isComplete = true
				break
			}

			// 处理超时错误，可能是因为服务器暂时没有更多数据发送
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				debugLog("读取超时，可能是切包的间隔或传输已完成")

				// 如果已经有一段时间没有收到新数据，可能是传输完成了
				if time.Since(lastReadTime) > noDataTimeThreshold {
					debugLog("超过%v未收到新数据，认为传输完成", noDataTimeThreshold)
					isComplete = true
					break
				}

				// 继续尝试读取
				debugLog("继续尝试读取数据...")
				continue
			}

			// 处理其他错误
			debugLog("读取错误: %v", err)
			c.stream.Close()
			c.stream = nil

			// 特定的应用错误可能需要重新连接
			if err.Error() == "Application error 0x0" {
				debugLog("应用错误0x0，尝试重新连接")
				if connErr := c.Connect(c.conn.Context()); connErr != nil {
					debugLog("重新连接失败: %v", connErr)
				}
			}

			// 检查是否需要重试
			retries++
			debugLog("重试 #%d/%d", retries, options.MaxRetries)
			if retries <= options.MaxRetries {
				sleepTime := time.Duration(retries) * time.Second
				debugLog("等待 %v 后重试", sleepTime)
				time.Sleep(sleepTime)
				continue
			}

			// 如果已经接收到一些数据，则不返回错误而是返回已收到的数据
			if len(totalRawResponse) > 0 {
				debugLog("达到最大重试次数，但已接收一些数据，继续处理")
				break
			}

			return nil, fmt.Errorf("读取响应失败: %v, 重试次数: %d", err, retries)
		}

		// 检查下载大小是否超过限制
		if int64(len(totalRawResponse)) > options.MaxDownloadSize {
			debugLog("下载大小超过限制: %d > %d 字节", len(totalRawResponse), options.MaxDownloadSize)
			return nil, fmt.Errorf("下载大小超过限制: %d 字节", options.MaxDownloadSize)
		}

		// 服务器持续发送数据，减少超时时间，加快读取速度
		if requireContinue && !isComplete {
			readTimeout = 5 * time.Second
			debugLog("调整读取超时为 %v 并继续读取", readTimeout)
			continue
		}
	}

	// 重置读取超时
	if c.stream != nil {
		if err := c.stream.SetReadDeadline(time.Time{}); err != nil {
			debugLog("重置读取超时失败: %v", err)
		}
	}

	debugLog("数据接收完成，接收了 %d 个数据包，共 %d 字节", packetCount, len(totalRawResponse))

	// 分析所有接收到的数据，提取纯净响应
	debugLog("开始分析数据并提取内容...")

	// 填充结果
	result.RawData = totalRawResponse
	result.PureData = string(totalPureResponse)
	fmt.Println("result.PureData111111111111111111: ", result.PureData)
	// fmt.Println("result.RawData1111111111111111111: ", result.RawData)
	debugLog("数据统计，原始数据: %d 字节, 纯净数据: %d 字节", len(result.RawData), len(result.PureData))
	debugLog("原始数据与纯净数据的比例: %.2f%%", float64(len(result.PureData))/float64(len(result.RawData))*100)

	// 先检测HTTP响应并解析，无论是否要保存文件
	if len(result.PureData) > 0 && (result.HTTPInfo == nil || result.HTTPInfo.Body == nil || len(result.HTTPInfo.Body) == 0) {
		if isHTTPResponse(result.PureData) {
			debugLog("检测到HTTP响应，尝试解析")

			// 特别处理：只接收一次数据的情况，可能需要额外跳过协议头
			if packetCount == 1 {
				debugLog("只接收到一个数据包，尝试特殊处理")

				// 打印前100字节，便于调试
				if len(result.PureData) > 0 {
					printHexData("PureData前100字节", []byte(result.PureData), 100)
				}

				headerBodySplit := strings.Index(result.PureData, "\r\n\r\n")
				if headerBodySplit != -1 && headerBodySplit+4+proto.RESPONSE_HEAD_LEN < len(result.PureData) {
					// 先提取HTTP头部
					httpHeaders := result.PureData[:headerBodySplit]
					debugLog("HTTP头部长度: %d 字节", len(httpHeaders))

					// 解析HTTP状态码
					statusCode := 0
					statusMatch := regexp.MustCompile(`HTTP/\d\.\d\s+(\d+)\s+`).FindStringSubmatch(httpHeaders)
					if len(statusMatch) >= 2 {
						statusCode, _ = strconv.Atoi(statusMatch[1])
					}

					// 提取内容长度
					contentLength := -1
					contentLengthMatch := regexp.MustCompile(`Content-Length: (\d+)`).FindStringSubmatch(httpHeaders)
					if len(contentLengthMatch) >= 2 {
						contentLength, _ = strconv.Atoi(contentLengthMatch[1])
					}

					// 计算标准HTTP体的起始位置和特殊处理后的起始位置
					normalBodyStart := headerBodySplit + 4
					specialBodyStart := headerBodySplit + 4 + proto.RESPONSE_HEAD_LEN

					debugLog("标准HTTP体起始位置: %d, 特殊处理后起始位置: %d", normalBodyStart, specialBodyStart)

					// 前后取出20字节，查看周围内容
					if normalBodyStart > 20 && normalBodyStart+20 < len(result.PureData) {
						surroundingData := result.PureData[normalBodyStart-20 : normalBodyStart+20]
						printHexData("标准HTTP体起始点周围数据", []byte(surroundingData), 40)
					}

					if specialBodyStart > 20 && specialBodyStart+20 < len(result.PureData) {
						surroundingData := result.PureData[specialBodyStart-20 : specialBodyStart+20]
						printHexData("特殊处理后HTTP体起始点周围数据", []byte(surroundingData), 40)
					}

					// 直接将HTTP头部后面的内容加上额外的RESPONSE_HEAD_LEN字节
					body := ""
					if specialBodyStart < len(result.PureData) {
						body = result.PureData[specialBodyStart:]
					}

					debugLog("特殊处理：提取HTTP头部后，跳过额外的%d字节作为响应体起始点", proto.RESPONSE_HEAD_LEN)
					debugLog("状态码: %d, Content-Length: %d, 实际body长度: %d", statusCode, contentLength, len(body))

					// 比较特殊处理前后body的前20字节
					normalBody := ""
					if normalBodyStart < len(result.PureData) {
						normalBody = result.PureData[normalBodyStart:]
					}

					if len(normalBody) > 0 && len(body) > 0 {
						normalBodyLen := 20
						if len(normalBody) < normalBodyLen {
							normalBodyLen = len(normalBody)
						}

						specialBodyLen := 20
						if len(body) < specialBodyLen {
							specialBodyLen = len(body)
						}

						printHexData("标准处理body前20字节", []byte(normalBody[:normalBodyLen]), normalBodyLen)
						printHexData("特殊处理body前20字节", []byte(body[:specialBodyLen]), specialBodyLen)
					}

					// 创建HTTP响应结构
					httpInfo := &HTTPResponseInfo{
						StatusCode: statusCode,
						Headers:    make(map[string]string),
						Body:       []byte(body),
						IsHTTP:     true,
					}

					// 解析头部字段
					headerLines := strings.Split(httpHeaders, "\r\n")
					for i := 1; i < len(headerLines); i++ { // 跳过状态行
						line := headerLines[i]
						if line == "" {
							continue
						}
						parts := strings.SplitN(line, ":", 2)
						if len(parts) == 2 {
							key := strings.TrimSpace(parts[0])
							value := strings.TrimSpace(parts[1])
							httpInfo.Headers[key] = value
						}
					}

					// 如果提取的内容长度与内容长度头匹配，则使用此特殊处理的结果
					if contentLength > 0 && contentLength <= len(body) {
						debugLog("内容长度匹配，采用特殊处理结果")
						result.HTTPInfo = httpInfo
					} else {
						debugLog("内容长度不匹配，尝试常规解析")
						regularHttpInfo, err := parseHTTPResponse(result.PureData)
						if err == nil && regularHttpInfo != nil {
							result.HTTPInfo = regularHttpInfo

							// 如果常规解析得到的body也为空，则尝试使用特殊处理的body
							if len(regularHttpInfo.Body) == 0 && len(body) > 0 {
								debugLog("常规解析得到空body，使用特殊处理的body")
								regularHttpInfo.Body = []byte(body)
							}
						} else {
							// 常规解析失败，使用特殊处理的结果
							debugLog("常规解析失败，使用特殊处理结果: %v", err)
							result.HTTPInfo = httpInfo
						}
					}
				} else {
					// 特殊处理失败，尝试常规解析
					debugLog("无法进行特殊处理，尝试常规解析")
					httpInfo, err := parseHTTPResponse(result.PureData)
					if err == nil && httpInfo != nil {
						result.HTTPInfo = httpInfo
					} else {
						debugLog("解析HTTP响应失败: %v", err)
					}
				}
			} else {
				// 多个数据包的情况，使用常规解析
				httpInfo, err := parseHTTPResponse(result.PureData)
				if err == nil && httpInfo != nil {
					result.HTTPInfo = httpInfo
					debugLog("成功解析HTTP响应, 状态码: %d, 响应体: %d 字节",
						httpInfo.StatusCode, len(httpInfo.Body))

					// 额外检查：如果HTTP响应体为空但Content-Length不为0，重新尝试提取
					if len(httpInfo.Body) == 0 {
						if cl, exists := httpInfo.Headers["Content-Length"]; exists {
							contentLength, _ := strconv.Atoi(cl)
							if contentLength > 0 {
								debugLog("HTTP响应体为空但Content-Length为%d，尝试直接提取", contentLength)
								// 尝试直接从PureData中提取响应体
								headerBodySplit := strings.Index(result.PureData, "\r\n\r\n")
								if headerBodySplit != -1 && headerBodySplit+4 < len(result.PureData) {
									directBody := result.PureData[headerBodySplit+4:]
									debugLog("直接提取的响应体长度: %d", len(directBody))
									if len(directBody) > 0 {
										httpInfo.Body = []byte(directBody)
										debugLog("更新HTTP响应体，新大小: %d 字节", len(httpInfo.Body))
									}
								}
							}
						}
					}

					// 如果状态码和响应体大小都是预期的值，但响应体仍然为空，尝试使用全部纯净数据
					if httpInfo.StatusCode == 200 && len(httpInfo.Body) == 0 {
						expectedSize := 0
						if cl, exists := httpInfo.Headers["Content-Length"]; exists {
							expectedSize, _ = strconv.Atoi(cl)
						}

						if expectedSize > 0 && len(result.PureData) >= expectedSize {
							debugLog("HTTP状态码为200，但响应体为空，使用全部纯净数据作为响应体")
							// 尝试使用全部纯净数据
							httpInfo.Body = []byte(result.PureData)
							debugLog("使用纯净数据作为响应体，大小: %d 字节", len(httpInfo.Body))
						}
					}
				} else {
					debugLog("解析HTTP响应失败: %v", err)
				}
			}

			// 无论使用哪种方法，如果解析成功，记录一下结果
			if result.HTTPInfo != nil {
				debugLog("最终HTTP解析结果 - 状态码: %d, 响应体大小: %d 字节",
					result.HTTPInfo.StatusCode, len(result.HTTPInfo.Body))
			}
		} else {
			debugLog("未检测到HTTP响应，将作为二进制数据处理")
		}
	}

	// 详细记录各种数据大小
	fmt.Println("数据统计信息:")
	fmt.Printf("- 收到数据包: %d 个\n", packetCount)
	fmt.Printf("- 原始数据总大小: %d 字节\n", len(result.RawData))
	fmt.Printf("- 解析后纯净数据大小: %d 字节\n", len(result.PureData))
	if result.HTTPInfo != nil {
		fmt.Printf("- HTTP状态码: %d\n", result.HTTPInfo.StatusCode)
		fmt.Printf("- HTTP响应体大小: %d 字节\n", len(result.HTTPInfo.Body))
		for key, value := range result.HTTPInfo.Headers {
			if key == "Content-Type" || key == "Content-Length" {
				fmt.Printf("- HTTP头: %s: %s\n", key, value)
			}
		}
	}

	// 计算保存内容的MD5
	var contentToSave string

	// 确定要保存的内容，无论是否要保存文件
	if result.HTTPInfo != nil && result.HTTPInfo.IsHTTP && options.DetectHTTP {
		contentToSave = string(result.HTTPInfo.Body)
		debugLog("使用HTTP响应体作为内容: %d 字节", len(contentToSave))
	} else {
		contentToSave = result.PureData
		debugLog("使用纯净数据作为内容: %d 字节", len(contentToSave))
	}

	// 计算MD5
	md5sum := md5.Sum([]byte(contentToSave))
	result.MD5Sum = fmt.Sprintf("%x", md5sum)
	debugLog("计算内容MD5: %s", result.MD5Sum)

	// 检查并清理EMM包头
	cleanedContent := cleanEMMHeader([]byte(contentToSave))

	// 根据SaveToFile选项决定是否保存文件
	if options.SaveToFile {
		saveDir := options.SaveDir
		if saveDir == "" {
			saveDir = "."
		}

		// 确保目录存在
		if err := os.MkdirAll(saveDir, 0755); err != nil {
			debugLog("创建保存目录失败: %v", err)
			return result, fmt.Errorf("创建保存目录失败: %v", err)
		}

		// 构造文件名
		fileName := fmt.Sprintf("%s_%s.bin", options.FileNamePrefix, result.MD5Sum)
		filePath := filepath.Join(saveDir, fileName)
		result.FilePath = filePath
		debugLog("文件将保存为: %s", filePath)

		// 写入文件（即使内容为空也创建文件）
		if err := os.WriteFile(filePath, cleanedContent, 0644); err != nil {
			debugLog("保存文件失败: %v", err)
			return result, fmt.Errorf("保存文件失败: %v", err)
		}

		debugLog("文件保存成功: %s (%d 字节)", filePath, len(cleanedContent))
	} else {
		debugLog("不保存文件，仅返回内存中的数据")
	}

	return result, nil
}

// DownloadFile 使用指定的请求下载文件并保存到本地
func (c *TransferClient) DownloadFile(content string, saveDir string, fileNamePrefix string) (string, error) {
	debugLog("使用简化下载函数，保存到目录: %s, 前缀: %s", saveDir, fileNamePrefix)

	options := DefaultDownloadOptions()
	options.SaveToFile = true
	options.SaveDir = saveDir
	options.FileNamePrefix = fileNamePrefix
	options.DetectHTTP = true // 保证检测HTTP响应

	// 增加最大重试次数和读取超时时间，以确保能处理服务器切包返回的情况
	options.MaxRetries = 5
	options.ReadTimeout = 60 * time.Second

	debugLog("设置下载选项: SaveToFile=true, SaveDir=%s, FileNamePrefix=%s, MaxRetries=%d, ReadTimeout=%v",
		saveDir, fileNamePrefix, options.MaxRetries, options.ReadTimeout)

	// 确保目录存在
	if saveDir != "" {
		if err := os.MkdirAll(saveDir, 0755); err != nil {
			debugLog("创建保存目录失败: %v", err)
			return "", fmt.Errorf("创建保存目录失败: %v", err)
		}
		debugLog("保存目录已创建或已存在: %s", saveDir)
	}

	result, err := c.SendTransferRequestWithDownload(content, options)
	if err != nil {
		debugLog("下载失败: %v", err)
		return "", err
	}

	fmt.Println("下载结果数据统计:")
	fmt.Printf("- 发送字节数: %d\n", result.SentBytes)
	fmt.Printf("- 接收字节数: %d\n", result.ReceivedBytes)
	fmt.Printf("- 原始数据大小: %d 字节\n", len(result.RawData))
	fmt.Printf("- 纯净数据大小: %d 字节\n", len(result.PureData))
	if result.HTTPInfo != nil {
		fmt.Printf("- HTTP状态码: %d\n", result.HTTPInfo.StatusCode)
		fmt.Printf("- HTTP响应体大小: %d 字节\n", len(result.HTTPInfo.Body))
	}

	if result.FilePath == "" {
		fmt.Println("警告: 未能设置文件路径，但下载可能已成功")

		// 确定要保存的内容
		var contentToSave string
		if result.HTTPInfo != nil && result.HTTPInfo.IsHTTP {
			contentToSave = string(result.HTTPInfo.Body)
			fmt.Printf("使用HTTP响应体作为保存内容: %d 字节\n", len(contentToSave))
		} else if len(result.PureData) > 0 {
			contentToSave = result.PureData
			fmt.Printf("使用纯净数据作为保存内容: %d 字节\n", len(contentToSave))
		} else if len(result.RawData) > 0 {
			contentToSave = string(result.RawData)
			fmt.Printf("使用原始数据作为保存内容: %d 字节\n", len(contentToSave))
		} else {
			fmt.Println("所有数据均为空，将创建空文件")
			contentToSave = ""
		}

		// 为内容生成MD5
		md5sum := md5.Sum([]byte(contentToSave))
		md5str := fmt.Sprintf("%x", md5sum)
		if len(contentToSave) == 0 {
			md5str = "empty"
		}

		// 生成文件名
		fileName := fmt.Sprintf("%s_%s.bin", fileNamePrefix, md5str)
		filePath := filepath.Join(saveDir, fileName)
		fmt.Printf("将数据保存到: %s\n", filePath)

		// 使用保存函数保存内容
		if err := saveContentToFile(filePath, []byte(contentToSave)); err != nil {
			fmt.Printf("创建文件失败: %v\n", err)
			return "", fmt.Errorf("保存文件失败: %v", err)
		}

		fmt.Printf("文件创建成功: %s\n", filePath)
		return filePath, nil
	}

	debugLog("文件下载成功: %s", result.FilePath)
	return result.FilePath, nil
}

// 判断数据是否为HTTP响应
func isHTTPResponse(data string) bool {
	if len(data) < 10 {
		debugLog("数据太短，无法判断是否为HTTP响应")
		return false
	}

	// 检查是否以HTTP/开头，这是最明确的标识
	if strings.HasPrefix(data, "HTTP/") {
		debugLog("数据以HTTP/开头，确认为HTTP响应")
		return true
	}

	// 尝试在数据中查找HTTP/标记，因为有时候响应可能包含前导数据
	httpPosition := strings.Index(data, "HTTP/")
	if httpPosition > 0 && httpPosition < 100 { // 限制在前100个字符中查找，避免误判
		// 找到HTTP/标记，但不在开头，检查前面是否有有效内容
		prefix := data[:httpPosition]
		trimmedPrefix := strings.TrimSpace(prefix)
		if len(trimmedPrefix) == 0 || strings.Contains(trimmedPrefix, "\n") {
			// 前缀为空白或包含换行，可能是有效的HTTP响应
			debugLog("在位置%d找到HTTP/标记，前导数据可能为噪声", httpPosition)
			return true
		}
	}

	// 检查是否包含常见的HTTP头部字段
	// 注意：这是一个启发式检测，不如直接检查HTTP/准确
	commonHeaders := []string{
		"Content-Type:",
		"Content-Length:",
		"Server:",
		"Date:",
		"Last-Modified:",
		"ETag:",
		"Cache-Control:",
		"Access-Control-Allow-Origin:",
	}

	headerCount := 0
	for _, header := range commonHeaders {
		if strings.Contains(data, header) {
			headerCount++
			debugLog("找到HTTP头部: %s", header)
		}
	}

	// 如果包含多个HTTP头部字段，可能是HTTP响应
	if headerCount >= 2 {
		debugLog("找到%d个HTTP头部字段，可能是HTTP响应", headerCount)

		// 检查是否包含常见的HTTP状态行模式
		statusLinePattern := regexp.MustCompile(`(?i)HTTP/\d\.\d\s+\d{3}\s+`)
		if statusLinePattern.MatchString(data) {
			debugLog("匹配到HTTP状态行模式，确认为HTTP响应")
			return true
		}

		// 检查是否有头部和主体分隔符
		if strings.Contains(data, "\r\n\r\n") || strings.Contains(data, "\n\n") {
			debugLog("找到头部和主体分隔符，确认为HTTP响应")
			return true
		}

		// 如果找到足够多的HTTP头部，也认为是HTTP响应
		if headerCount >= 3 {
			debugLog("找到%d个HTTP头部字段，确认为HTTP响应", headerCount)
			return true
		}
	}

	debugLog("未检测到HTTP响应特征")
	return false
}

// 分析字符串中的特殊字符
func analyzeSpecialChars(data string) string {
	if len(data) == 0 {
		return "空字符串"
	}

	var result strings.Builder
	for i, c := range data {
		if i > 100 {
			result.WriteString("...(更多字符被省略)")
			break
		}

		// 检查是否为控制字符或特殊字符
		if c < 32 || c > 126 {
			result.WriteString(fmt.Sprintf("[%d:%X]", i, c))
		}
	}

	if result.Len() == 0 {
		return "没有特殊字符"
	}
	return result.String()
}

// 解析HTTP响应
func parseHTTPResponse(data string) (*HTTPResponseInfo, error) {
	if len(data) == 0 {
		return nil, errors.New("空的HTTP响应")
	}

	debugLog("开始解析HTTP响应，数据长度: %d 字节", len(data))
	specialChars := analyzeSpecialChars(data)
	debugLog("响应中的特殊字符: %s", specialChars)

	if len(data) > 200 {
		debugLog("HTTP响应前200字节: %s", data[:200])
	} else {
		debugLog("HTTP响应全文: %s", data)
	}

	// 检查是否有可能被截断的HTTP头
	if !strings.HasPrefix(data, "HTTP/") {
		debugLog("警告: 响应不是以HTTP/开头，可能不是完整的HTTP响应或被截断")
		// 尝试在响应中查找HTTP头的开始
		httpHeaderStart := strings.Index(data, "HTTP/")
		if httpHeaderStart > 0 {
			debugLog("在位置 %d 找到HTTP头开始标记，尝试从此处解析", httpHeaderStart)
			data = data[httpHeaderStart:]
		}
	}

	result := &HTTPResponseInfo{
		Headers: make(map[string]string),
		IsHTTP:  true,
	}

	// 查找头部和主体分隔符
	headerBodySplit := strings.Index(data, "\r\n\r\n")
	if headerBodySplit == -1 {
		debugLog("未找到标准头部和主体分隔符\\r\\n\\r\\n，尝试其他分隔符")
		// 尝试使用\n\n作为分隔符
		headerBodySplit = strings.Index(data, "\n\n")
		if headerBodySplit == -1 {
			debugLog("也未找到\\n\\n分隔符")
			// 如果没有找到分隔符，可能是不完整的响应
			return nil, errors.New("HTTP响应不完整，缺少头部和主体分隔符")
		}
		debugLog("使用\\n\\n作为分隔符，位置: %d", headerBodySplit)
		// 提取头部和主体
		headers := data[:headerBodySplit]
		body := data[headerBodySplit+2:] // +2 跳过\n\n

		// 将\n替换为\r\n以保持统一处理
		headers = strings.ReplaceAll(headers, "\n", "\r\n")

		debugLog("提取的头部长度: %d 字节", len(headers))
		debugLog("提取的主体长度: %d 字节", len(body))

		// 重新赋值数据，以便后续处理
		data = headers + "\r\n\r\n" + body
		headerBodySplit = len(headers)
	}

	debugLog("找到头部和主体分隔符，位置: %d", headerBodySplit)

	// 提取头部和主体
	headers := data[:headerBodySplit]
	body := ""
	if headerBodySplit+4 < len(data) {
		body = data[headerBodySplit+4:]
	}

	debugLog("提取的头部长度: %d 字节", len(headers))
	debugLog("提取的主体长度: %d 字节", len(body))

	// 解析第一行（状态行）
	headerLines := strings.Split(headers, "\r\n")
	if len(headerLines) == 0 {
		// 尝试用\n分割
		headerLines = strings.Split(headers, "\n")
		if len(headerLines) == 0 {
			debugLog("解析的头部行数为0")
			return nil, errors.New("HTTP响应头部格式错误")
		}
	}

	debugLog("头部行数: %d", len(headerLines))
	debugLog("状态行: %s", headerLines[0])

	// 解析状态码
	statusLine := headerLines[0]
	statusMatch := regexp.MustCompile(`HTTP/\d\.\d\s+(\d+)\s+`).FindStringSubmatch(statusLine)
	if len(statusMatch) >= 2 {
		statusCode, err := strconv.Atoi(statusMatch[1])
		if err == nil {
			result.StatusCode = statusCode
			debugLog("解析到状态码: %d", statusCode)
		} else {
			debugLog("状态码解析失败: %v", err)
		}
	} else {
		debugLog("未匹配到状态码，状态行: %s", statusLine)
	}

	// 解析其他头部字段
	for i := 1; i < len(headerLines); i++ {
		line := headerLines[i]
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			result.Headers[key] = value
			debugLog("解析到头部: %s: %s", key, value)
		} else {
			debugLog("无法解析头部行: %s", line)
		}
	}

	// 获取Content-Length
	contentLength := -1
	if cl, exists := result.Headers["Content-Length"]; exists {
		if len, err := strconv.Atoi(cl); err == nil {
			contentLength = len
			debugLog("Content-Length: %d", contentLength)
		} else {
			debugLog("Content-Length解析失败: %v", err)
		}
	} else {
		debugLog("未找到Content-Length头")
	}

	// 处理Transfer-Encoding: chunked
	if encoding, exists := result.Headers["Transfer-Encoding"]; exists && strings.ToLower(encoding) == "chunked" {
		debugLog("检测到分块编码，尝试解析")
		unchunkedBody, err := parseChunkedBody([]byte(body))
		if err == nil {
			debugLog("分块编码解析成功，解码后长度: %d 字节", len(unchunkedBody))
			body = string(unchunkedBody)
		} else {
			debugLog("分块编码解析失败: %v", err)
		}
	}

	// 记录最终解析的主体大小
	bodyBytes := []byte(body)
	debugLog("最终主体大小: %d 字节", len(bodyBytes))

	// 如果主体长度为0但HTTP响应有内容，可能是解析问题
	if len(bodyBytes) == 0 && contentLength > 0 {
		debugLog("警告: 主体长度为0但Content-Length不为0，可能存在解析问题")
		// 尝试直接获取原始内容的主体部分
		if headerBodySplit+4 < len(data) {
			rawBody := data[headerBodySplit+4:]
			debugLog("直接提取的主体长度: %d 字节", len(rawBody))
			bodyBytes = []byte(rawBody)
		}
	}

	// 检查主体大小与Content-Length是否匹配
	if contentLength > 0 && len(bodyBytes) != contentLength {
		debugLog("警告: 主体大小(%d)与Content-Length(%d)不匹配", len(bodyBytes), contentLength)
		// 如果主体小于Content-Length，可能是被截断了
		if len(bodyBytes) < contentLength {
			debugLog("主体可能被截断，尝试使用原始数据")
		}
	}

	result.Body = bodyBytes
	return result, nil
}

// 解析分块编码的响应体
func parseChunkedBody(chunkedBody []byte) ([]byte, error) {
	if len(chunkedBody) == 0 {
		debugLog("分块编码响应为空")
		return []byte{}, nil
	}

	debugLog("开始解析分块编码响应，大小: %d 字节", len(chunkedBody))
	// 打印前50个字节用于调试
	previewSize := 50
	if len(chunkedBody) < previewSize {
		previewSize = len(chunkedBody)
	}
	debugLog("分块编码预览: %s", string(chunkedBody[:previewSize]))

	var result []byte
	remaining := chunkedBody

	chunkIndex := 0
	for len(remaining) > 0 {
		chunkIndex++
		debugLog("解析第%d个分块，剩余数据: %d 字节", chunkIndex, len(remaining))

		// 查找块大小行结束
		chunkSizeEnd := bytes.Index(remaining, []byte("\r\n"))
		if chunkSizeEnd == -1 {
			// 尝试只用\n作为分隔符
			chunkSizeEnd = bytes.Index(remaining, []byte("\n"))
			if chunkSizeEnd == -1 {
				debugLog("无法找到块大小行结束标记")
				return nil, errors.New("无效的分块编码：找不到块大小行结束")
			}
			debugLog("使用\\n作为分隔符，位置: %d", chunkSizeEnd)
		} else {
			debugLog("使用\\r\\n作为分隔符，位置: %d", chunkSizeEnd)
		}

		// 解析块大小（十六进制）
		chunkSizeHex := string(remaining[:chunkSizeEnd])
		chunkSizeHex = strings.TrimSpace(chunkSizeHex)
		debugLog("块大小Hex字符串: '%s'", chunkSizeHex)

		// 检查分块大小是否包含扩展信息（分号后的内容）
		semicolonIndex := strings.Index(chunkSizeHex, ";")
		if semicolonIndex != -1 {
			debugLog("块大小包含扩展信息，去除")
			chunkSizeHex = chunkSizeHex[:semicolonIndex]
		}

		chunkSize, err := strconv.ParseInt(chunkSizeHex, 16, 64)
		if err != nil {
			debugLog("解析块大小失败: %v, 块大小字符串: '%s'", err, chunkSizeHex)
			return nil, fmt.Errorf("无效的分块大小: %s, 错误: %v", chunkSizeHex, err)
		}

		debugLog("分块大小: %d 字节", chunkSize)

		// 如果块大小为0，表示分块结束
		if chunkSize == 0 {
			debugLog("遇到大小为0的块，分块编码结束")
			break
		}

		// 计算块的开始和结束位置
		var chunkStart, chunkEnd int
		if bytes.HasPrefix(remaining[chunkSizeEnd:], []byte("\r\n")) {
			chunkStart = chunkSizeEnd + 2 // +2 跳过\r\n
		} else {
			chunkStart = chunkSizeEnd + 1 // +1 跳过\n
		}
		chunkEnd = chunkStart + int(chunkSize)

		// 检查是否超出范围
		if chunkEnd > len(remaining) {
			debugLog("块结束位置(%d)超出剩余数据范围(%d)", chunkEnd, len(remaining))
			// 使用剩余所有数据
			debugLog("使用所有剩余数据作为块内容")
			result = append(result, remaining[chunkStart:]...)
			break
		}

		// 添加块内容到结果
		debugLog("添加块内容，起始位置: %d, 结束位置: %d, 大小: %d 字节",
			chunkStart, chunkEnd, chunkEnd-chunkStart)
		result = append(result, remaining[chunkStart:chunkEnd]...)

		// 检查块结束后是否有\r\n
		if chunkEnd+2 <= len(remaining) && bytes.Equal(remaining[chunkEnd:chunkEnd+2], []byte("\r\n")) {
			// 移动到下一个块
			remaining = remaining[chunkEnd+2:] // +2 跳过块结尾的\r\n
			debugLog("使用\\r\\n作为块尾分隔符，移动到下一个块，剩余: %d 字节", len(remaining))
		} else if chunkEnd+1 <= len(remaining) && remaining[chunkEnd] == '\n' {
			// 只有\n作为分隔符
			remaining = remaining[chunkEnd+1:] // +1 跳过块结尾的\n
			debugLog("使用\\n作为块尾分隔符，移动到下一个块，剩余: %d 字节", len(remaining))
		} else {
			// 没有找到预期的分隔符，但继续处理剩余数据
			if chunkEnd < len(remaining) {
				remaining = remaining[chunkEnd:]
				debugLog("未找到块尾分隔符，直接移动到下一个位置，剩余: %d 字节", len(remaining))
			} else {
				// 已经处理完所有数据
				debugLog("已处理完所有分块数据")
				break
			}
		}
	}

	debugLog("分块编码解析完成，解析后数据大小: %d 字节", len(result))
	return result, nil
}

// 打印数据的十六进制表示，用于调试
func printHexData(name string, data []byte, maxLen int) {
	if len(data) == 0 {
		debugLog("%s: 空数据", name)
		return
	}

	if len(data) > maxLen {
		data = data[:maxLen]
	}

	var hexStr strings.Builder
	var asciiStr strings.Builder

	hexStr.WriteString(fmt.Sprintf("%s (长度=%d):\n", name, len(data)))

	for i, b := range data {
		// 每16个字节一行
		if i%16 == 0 && i > 0 {
			hexStr.WriteString("  ")
			hexStr.WriteString(asciiStr.String())
			hexStr.WriteString("\n")
			asciiStr.Reset()
		}

		// 写入十六进制表示
		hexStr.WriteString(fmt.Sprintf(" %02X", b))

		// 写入ASCII表示（对于可打印字符）
		if b >= 32 && b <= 126 {
			asciiStr.WriteByte(b)
		} else {
			asciiStr.WriteString(".")
		}
	}

	// 补齐最后一行
	remainder := len(data) % 16
	if remainder > 0 {
		// 补齐空格
		for i := 0; i < (16-remainder)*3; i++ {
			hexStr.WriteString(" ")
		}
		hexStr.WriteString("  ")
		hexStr.WriteString(asciiStr.String())
	}

	debugLog("%s", hexStr.String())
}

// 检查并清理EMM包头
func cleanEMMHeader(data []byte) []byte {
	// 检查数据长度是否足够
	if len(data) < 24 { // 至少需要包含"EMM:"和20字节的头部
		return data
	}

	// 查找所有的"EMM:"标记
	emmMarker := []byte{0x45, 0x4D, 0x4D, 0x3A} // "EMM:"的ASCII码
	var cleanedData []byte
	var lastEnd int = 0
	var foundHeaders int = 0

	for i := 0; i <= len(data)-4; i++ {
		if bytes.Equal(data[i:i+4], emmMarker) {
			// 确认这是一个真正的EMM包头：检查后面是否有足够的20字节空间
			if i+20 <= len(data) {
				// 添加EMM标记前的数据到结果
				if i > lastEnd {
					cleanedData = append(cleanedData, data[lastEnd:i]...)
				}
				// 跳过EMM包头（20字节）
				lastEnd = i + 20
				foundHeaders++
				debugLog("在位置 %d 发现并移除EMM包头", i)
			}
		}
	}

	// 添加剩余数据
	if lastEnd < len(data) {
		cleanedData = append(cleanedData, data[lastEnd:]...)
	}

	// 如果没有找到EMM包头或者清理后数据为空，返回原始数据
	if foundHeaders == 0 || len(cleanedData) == 0 {
		debugLog("未发现EMM包头或清理后数据为空，保持原始数据不变")
		return data
	}

	debugLog("清理了 %d 个EMM包头，数据大小从 %d 减少到 %d 字节", foundHeaders, len(data), len(cleanedData))
	return cleanedData
}

// 保存内容到文件，并进行EMM包头检查
func saveContentToFile(filePath string, content []byte) error {
	// 清理EMM包头
	cleanedContent := cleanEMMHeader(content)

	// 写入文件
	if err := os.WriteFile(filePath, cleanedContent, 0644); err != nil {
		debugLog("保存文件失败: %v", err)
		return fmt.Errorf("保存文件失败: %v", err)
	}

	debugLog("文件保存成功: %s (%d 字节)", filePath, len(cleanedContent))
	return nil
}
