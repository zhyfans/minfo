// Package pixhost 负责 Pixhost 域名选项的规范化。

package pixhost

import (
	"fmt"
	"strings"
)

const (
	// DefaultDomain 是 Pixhost 上传默认使用的主域名。
	DefaultDomain = "pixhost.to"
	// AlternateDomain 是 Pixhost 上传可选使用的备用主域名。
	AlternateDomain = "pixhost.cc"
)

// NormalizeDomain 校验并规范化前端选择的 Pixhost 主域名。
func NormalizeDomain(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return DefaultDomain, nil
	}

	switch trimmed {
	case DefaultDomain, AlternateDomain:
		return trimmed, nil
	default:
		return "", fmt.Errorf("pixhost domain is unsupported: %s", value)
	}
}

func apiURLForDomain(domain string) string {
	return "https://api." + domain + "/images"
}
