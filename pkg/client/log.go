package client

import (
	"fmt"
	"log"
	"os"
)

// LogLevel 定义日志级别
type LogLevel int

const (
	// LogLevelNone 不打印日志
	LogLevelNone LogLevel = iota
	// LogLevelError 只打印错误日志
	LogLevelError
	// LogLevelWarning 打印警告和错误日志
	LogLevelWarning
	// LogLevelInfo 打印普通信息、警告和错误日志
	LogLevelInfo
	// LogLevelDebug 打印所有日志，包括调试信息
	LogLevelDebug
)

var (
	// 默认日志级别，标准版为Debug，轻量版为Warning
	currentLogLevel = LogLevelInfo
	// 是否启用轻量版
	isLiteVersion = false
)

// 日志前缀
const (
	prefixError   = "[ERROR] "
	prefixWarning = "[WARN]  "
	prefixInfo    = "[INFO]  "
	prefixDebug   = "[DEBUG] "
)

// EnableLiteMode 启用轻量版模式
func EnableLiteMode() {
	isLiteVersion = true
	currentLogLevel = LogLevelWarning
}

// SetLogLevel 设置日志级别
func SetLogLevel(level LogLevel) {
	currentLogLevel = level
}

// GetLogLevel 获取当前日志级别
func GetLogLevel() LogLevel {
	return currentLogLevel
}

// IsLiteVersion 检查是否为轻量版
func IsLiteVersion() bool {
	return isLiteVersion
}

// LogError 记录错误日志
func LogError(format string, v ...interface{}) {
	if currentLogLevel >= LogLevelError {
		log.Printf(prefixError+format, v...)
	}
}

// LogWarning 记录警告日志
func LogWarning(format string, v ...interface{}) {
	if currentLogLevel >= LogLevelWarning {
		log.Printf(prefixWarning+format, v...)
	}
}

// LogInfo 记录信息日志
func LogInfo(format string, v ...interface{}) {
	if currentLogLevel >= LogLevelInfo {
		log.Printf(prefixInfo+format, v...)
	}
}

// LogDebug 记录调试日志
func LogDebug(format string, v ...interface{}) {
	if currentLogLevel >= LogLevelDebug {
		log.Printf(prefixDebug+format, v...)
	}
}

// LogFatal 记录致命错误并终止程序
func LogFatal(format string, v ...interface{}) {
	log.Fatalf(prefixError+format, v...)
	os.Exit(1)
}

// FormatError 格式化错误，不打印到日志
func FormatError(format string, v ...interface{}) error {
	return fmt.Errorf(format, v...)
}
