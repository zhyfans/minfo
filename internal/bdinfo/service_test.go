// Package bdinfo 验证 BDInfo 运行时配置。

package bdinfo

import "testing"

// TestDefaultBinaryPath 验证默认 BDInfo 路径已经统一到 /usr/local/bin/bdinfo。
func TestDefaultBinaryPath(t *testing.T) {
	const want = "/usr/local/bin/bdinfo"
	if defaultBinaryPath != want {
		t.Fatalf("defaultBinaryPath = %q, want %q", defaultBinaryPath, want)
	}
}
