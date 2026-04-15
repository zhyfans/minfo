// Package config 提供环境变量和超时等基础配置解析。

package config

import (
	"log"
	"os"
	"strings"
	"time"
)

const (
	DefaultPort           = "28080"
	DefaultRoot           = "/media"
	MaxUploadBytes        = int64(8 << 30)
	MaxMemoryBytes        = int64(32 << 20)
	MountTimeout          = 30 * time.Second
	UmountTimeout         = 30 * time.Second
	DefaultRequestTimeout = 20 * time.Minute
)

// RequestTimeout 保存当前服务处理单个请求时使用的统一超时时间。
var RequestTimeout = DurationFromEnv("REQUEST_TIMEOUT", DefaultRequestTimeout)

// FFmpegSSECompat 控制是否为 FFmpeg 注入 SSE 兼容环境变量，默认关闭。
var FFmpegSSECompat = BoolFromEnv("FFMPEG_SSE_COMPAT", false)

// Getenv 返回环境变量 key 的值；当结果为空字符串时返回 fallback。
func Getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// DurationFromEnv 解析时长环境变量；当变量缺失、格式非法或结果非正数时返回 fallback。
func DurationFromEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		log.Printf("invalid %s=%q; fallback to %s", key, value, fallback)
		return fallback
	}
	return duration
}

// BoolFromEnv 解析布尔环境变量；缺失或非法时返回 fallback。
func BoolFromEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("invalid %s=%q; fallback to %t", key, value, fallback)
		return fallback
	}
}
