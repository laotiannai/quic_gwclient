package client

import (
	"log"
	"os"
	"time"
)

// 库初始化时执行
func init() {
	// 检查是否为轻量版模式
	if isLiteVersion {
		// 轻量版只显示警告和错误
		SetLogLevel(LogLevelWarning)
	} else {
		// 标准版显示所有日志
		SetLogLevel(LogLevelDebug)
	}

	// 设置日志格式
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// 检查环境变量是否有日志级别设置
	logLevelEnv := os.Getenv("QUIC_GW_LOG_LEVEL")
	if logLevelEnv != "" {
		switch logLevelEnv {
		case "none":
			SetLogLevel(LogLevelNone)
		case "error":
			SetLogLevel(LogLevelError)
		case "warning", "warn":
			SetLogLevel(LogLevelWarning)
		case "info":
			SetLogLevel(LogLevelInfo)
		case "debug":
			SetLogLevel(LogLevelDebug)
		}
	}

	// 检查是否有轻量版环境变量设置
	liteModeEnv := os.Getenv("QUIC_GW_LITE_MODE")
	if liteModeEnv == "1" || liteModeEnv == "true" || liteModeEnv == "yes" {
		EnableLiteMode()
	}

	// 初始化日志
	if !isLiteVersion {
		LogInfo("QUIC Gateway Client 初始化 - 标准版")
		LogInfo("当前日志级别: %v", currentLogLevel)
		LogInfo("当前时间: %v", time.Now().Format(time.RFC3339))
	} else {
		LogWarning("QUIC Gateway Client 初始化 - 轻量版")
	}
}
