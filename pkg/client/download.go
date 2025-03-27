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

		responseBuffer := make([]byte, 64*1024) // 使用更大的缓冲区
		readBytes, err := c.stream.Read(responseBuffer)

		if readBytes > 0 {
			packetCount++
			// 重置连续无数据计数
			noDataCount = 0

			// 更新最后读取时间
			lastReadTime = time.Now()

			result.ReceivedBytes += readBytes
			chunk := responseBuffer[:readBytes]

			// 分析接收到的数据包的内容
			if DebugMode {
				// 检查前20个字节，看是否符合协议格式
				if readBytes >= 20 {
					headBytes := chunk[:20]
					tagBytes := headBytes[:4]
					var tagStr string
					if len(tagBytes) >= 4 {
						tagStr = fmt.Sprintf("%c%c%c%c", tagBytes[0], tagBytes[1], tagBytes[2], tagBytes[3])
					}
					debugLog("数据包#%d头部: [%X], ASCII头部标记: %s", packetCount, headBytes, tagStr)

					// 检查是否是有效的协议头
					if len(tagBytes) >= 4 && tagBytes[0] == 69 && tagBytes[1] == 77 &&
						tagBytes[2] == 77 && tagBytes[3] == 58 {
						debugLog("数据包#%d有效的协议头标记EMM:", packetCount)
					} else {
						// 检查是否包含HTTP格式数据
						isHTTP := false
						if readBytes > 10 {
							previewData := chunk
							if len(previewData) > 50 {
								previewData = previewData[:50]
							}
							if bytes.Contains(previewData, []byte("HTTP/")) {
								isHTTP = true
								debugLog("数据包#%d包含HTTP格式数据", packetCount)
							}
						}

						if !isHTTP {
							debugLog("数据包#%d没有有效的协议头标记或HTTP标记", packetCount)
						}
					}
				}
			}

			totalRawResponse = append(totalRawResponse, chunk...)
			debugLog("收到数据包 #%d: %d 字节, 总计已接收: %d 字节", packetCount, readBytes, result.ReceivedBytes)

			// 直接保存原始数据，不经过解析（避免丢失数据）
			// 此处对chunk进行处理，去除协议包后，并保存纯净数据包
			// respLen, cmd, _, _, body := parseMessage(chunk, readBytes)
			// chunk = chunk[proto.RESPONSE_HEAD_LEN:]

			// 判断是否收到结束连接的标志（这部分逻辑可能需要保留）
			respLen, cmd, _, _, _ := parseMessage(chunk, readBytes)

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
	fmt.Printf("totalRawResponse: %v\n", totalRawResponse)

	// 处理数据，逐段去除协议包，并提取纯净数据
	debugLog("开始解析和提取纯净响应数据...")

	// 创建临时缓冲区以存储所有提取出的body内容
	var pureDataBuffer []byte

	// 创建临时变量保存待处理的数据
	remainingRaw := totalRawResponse

	// 循环处理，直到所有数据都被处理完
	processCount := 0
	totalBodySize := 0
	for len(remainingRaw) > proto.RESPONSE_HEAD_LEN {
		processCount++
		debugLog("处理第%d段数据，当前剩余数据大小: %d 字节", processCount, len(remainingRaw))

		// 输出头部信息以便诊断
		if len(remainingRaw) >= 20 {
			head := remainingRaw[:20]
			tagBytes := head[:4]
			var tagStr string
			if len(tagBytes) >= 4 {
				tagStr = fmt.Sprintf("%c%c%c%c", tagBytes[0], tagBytes[1], tagBytes[2], tagBytes[3])
			}
			debugLog("数据段头部(前20字节): [%X], ASCII头部: %s", head, tagStr)
		}

		// 解析当前数据段
		respLen, cmd, dataLen, result, body := parseMessage(remainingRaw, len(remainingRaw))
		debugLog("解析结果: respLen=%d, cmd=%d, dataLen=%d, result=%d, body长度=%d",
			respLen, cmd, dataLen, result, len(body))

		// 如果解析结果无效，尝试分析数据是否为原始HTTP数据
		if respLen <= 0 || cmd == 0 {
			debugLog("解析无效，可能是原始HTTP数据或协议标记错误")

			// 尝试检测HTTP格式
			httpPrefix := []byte("HTTP/")
			httpIndex := bytes.Index(remainingRaw, httpPrefix)
			if httpIndex >= 0 {
				debugLog("在偏移量%d处发现HTTP头标记，将剩余数据作为HTTP数据处理", httpIndex)

				// 将HTTP内容提取出来
				httpData := remainingRaw[httpIndex:]
				debugLog("提取的HTTP数据大小: %d 字节", len(httpData))

				// 将HTTP数据添加到纯净数据
				pureDataBuffer = append(pureDataBuffer, httpData...)
				totalBodySize += len(httpData)
				debugLog("添加HTTP数据到纯净数据缓冲区，当前累计纯净数据大小: %d 字节", len(pureDataBuffer))

				// 结束解析
				debugLog("将剩余所有数据作为HTTP内容处理完毕")
				break
			}

			// 检查是否是二进制数据中存在协议头错位问题
			// 检查前20个字节，看看是否存在有效的协议标记
			var validTag bool = false
			var validOffset int = -1

			// 检查接下来的100个字节，寻找有效的协议头标记(EMM:)
			maxScanLen := 100
			if len(remainingRaw) < maxScanLen {
				maxScanLen = len(remainingRaw)
			}

			for i := 1; i < maxScanLen-4; i++ {
				// 检查是否是EMM:标记（头部标记的ASCII值）
				if remainingRaw[i] == 69 && remainingRaw[i+1] == 77 &&
					remainingRaw[i+2] == 77 && remainingRaw[i+3] == 58 {
					validTag = true
					validOffset = i
					debugLog("在偏移量%d处找到有效的协议头标记", validOffset)
					break
				}
			}

			if validTag && validOffset > 0 {
				debugLog("尝试从偏移量%d处重新解析协议数据", validOffset)
				remainingRaw = remainingRaw[validOffset:]
				continue
			}

			// 如果既不是HTTP也没找到有效协议头，尝试使用整个数据作为Body
			debugLog("未发现协议头或HTTP标记，将剩余数据作为原始内容处理")
			if len(remainingRaw) > 0 {
				// 作为最后的尝试，将剩余所有数据作为响应体
				pureDataBuffer = append(pureDataBuffer, remainingRaw...)
				totalBodySize += len(remainingRaw)
				debugLog("将剩余%d字节数据作为原始内容添加到结果中", len(remainingRaw))
			}

			// 结束解析
			break
		}

		// 检查命令类型
		if cmd == proto.EMM_COMMAND_LINK_CLOSE {
			debugLog("检测到链接关闭命令，停止解析")
			break
		}

		// 检查是否解析有效
		if respLen <= 0 || respLen > len(remainingRaw) {
			debugLog("无效的响应长度: %d，停止解析", respLen)
			break
		}

		// 将body添加到纯净数据缓冲区
		if len(body) > 0 {
			pureDataBuffer = append(pureDataBuffer, []byte(body)...)
			totalBodySize += len(body)
			debugLog("添加body到纯净数据缓冲区，当前body大小: %d 字节，累计纯净数据大小: %d 字节",
				len(body), len(pureDataBuffer))
		} else {
			debugLog("当前段没有body数据，跳过添加")
		}

		// 检查数据是否还有剩余
		segmentSize := proto.RESPONSE_HEAD_LEN + respLen
		if segmentSize >= len(remainingRaw) {
			debugLog("当前段数据处理完毕，无剩余数据")
			break
		}

		// 移除已处理的数据段
		remainingRaw = remainingRaw[segmentSize:]
		debugLog("移除已处理的数据段，剩余数据大小: %d 字节", len(remainingRaw))
	}

	debugLog("响应解析完成，共处理 %d 个数据段，提取纯净数据 %d 字节", processCount, totalBodySize)

	// 将提取的纯净数据转换为字符串并设置到结果中
	if len(pureDataBuffer) > 0 {
		result.PureData = string(pureDataBuffer)
		debugLog("成功提取到纯净数据，总大小: %d 字节", len(result.PureData))
	} else {
		result.PureData = ""
		debugLog("未提取到有效的纯净数据")
	}

	debugLog("数据统计，原始数据: %d 字节, 纯净数据: %d 字节", len(result.RawData), len(result.PureData))
	debugLog("原始数据与纯净数据的比例: %.2f%%", float64(len(result.PureData))/float64(len(result.RawData))*100)

	// 先检测HTTP响应并解析，无论是否要保存文件
	if len(result.PureData) > 0 && (result.HTTPInfo == nil || result.HTTPInfo.Body == nil || len(result.HTTPInfo.Body) == 0) {
		if isHTTPResponse(result.PureData) {
			debugLog("检测到HTTP响应，尝试解析")
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
		} else {
			debugLog("未检测到HTTP响应，将作为二进制数据处理")
		}
	}

	// 详细记录各种数据大小
	debugLog("数据统计信息:")
	debugLog("- 收到数据包: %d 个\n", packetCount)
	debugLog("- 原始数据总大小: %d 字节\n", len(result.RawData))
	debugLog("- 解析后纯净数据大小: %d 字节\n", len(result.PureData))
	if result.HTTPInfo != nil {
		debugLog("- HTTP状态码: %d\n", result.HTTPInfo.StatusCode)
		debugLog("- HTTP响应体大小: %d 字节\n", len(result.HTTPInfo.Body))
		for key, value := range result.HTTPInfo.Headers {
			if key == "Content-Type" || key == "Content-Length" {
				debugLog("- HTTP头: %s: %s\n", key, value)
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
		if err := os.WriteFile(filePath, []byte(contentToSave), 0644); err != nil {
			debugLog("保存文件失败: %v", err)
			return result, fmt.Errorf("保存文件失败: %v", err)
		}

		debugLog("文件保存成功: %s (%d 字节)", filePath, len(contentToSave))
	} else {
		debugLog("不保存文件，仅返回内存中的数据")
	}

	return result, nil
}

// DownloadFile 使用指定的请求下载文件并保存到本地
// saveToFile - 是否保存文件到本地磁盘
func (c *TransferClient) DownloadFile(content string, saveDir string, fileNamePrefix string, saveToFile bool) (string, error) {
	debugLog("使用简化下载函数，保存到目录: %s, 前缀: %s, 是否保存文件: %v", saveDir, fileNamePrefix, saveToFile)

	options := DefaultDownloadOptions()
	options.SaveToFile = saveToFile // 根据参数决定是否保存文件
	options.SaveDir = saveDir
	options.FileNamePrefix = fileNamePrefix
	options.DetectHTTP = false // 默认不检测HTTP协议

	// 增加最大重试次数和读取超时时间，以确保能处理服务器切包返回的情况
	options.MaxRetries = 5
	options.ReadTimeout = 60 * time.Second

	debugLog("设置下载选项: SaveToFile=%v, SaveDir=%s, FileNamePrefix=%s, MaxRetries=%d, ReadTimeout=%v",
		options.SaveToFile, saveDir, fileNamePrefix, options.MaxRetries, options.ReadTimeout)

	result, err := c.SendTransferRequestWithDownload(content, options)
	if err != nil {
		debugLog("下载失败: %v", err)
		return "", err
	}

	// 如果设置了保存文件，返回实际文件路径
	if saveToFile {
		return result.FilePath, nil
	} else {
		// 不保存文件时，返回虚拟路径
		return "memory:" + result.FilePath, nil
	}
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
