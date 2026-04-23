// Package screenshot 提供 DVD mediainfo 相关路径归一化辅助函数。

package screenshot

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveDVDMediaInfoProbePath 选择实际交给 mediainfo 的探测路径；优先使用能映射到 IFO 的路径。
func resolveDVDMediaInfoProbePath(path, probePath string) string {
	for _, candidate := range []string{probePath, path} {
		if resolved, ok := dvdMediaInfoIFOPath(candidate); ok {
			return resolved
		}
	}
	return strings.TrimSpace(path)
}

// resolveDVDMediaInfoVOBPath 返回与当前探测路径对应的标题 VOB 路径。
func resolveDVDMediaInfoVOBPath(path, probePath string) string {
	for _, candidate := range []string{probePath, path} {
		if resolved, ok := dvdMediaInfoTitleVOBPath(candidate); ok {
			return resolved
		}
	}
	return ""
}

// dvdMediaInfoIFOPath 把 DVD 相关路径归一化为对应的 IFO 文件路径。
func dvdMediaInfoIFOPath(path string) (string, bool) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", false
	}

	upperBase := strings.ToUpper(filepath.Base(cleaned))
	switch filepath.Ext(upperBase) {
	case ".IFO":
		if fileExists(cleaned) {
			return cleaned, true
		}
	case ".BUP":
		ifoPath := strings.TrimSuffix(cleaned, filepath.Ext(cleaned)) + ".IFO"
		if fileExists(ifoPath) {
			return ifoPath, true
		}
	case ".VOB":
		if strings.EqualFold(upperBase, "VIDEO_TS.VOB") {
			ifoPath := filepath.Join(filepath.Dir(cleaned), "VIDEO_TS.IFO")
			if fileExists(ifoPath) {
				return ifoPath, true
			}
			return "", false
		}
		if len(upperBase) == len("VTS_00_1.VOB") &&
			strings.HasPrefix(upperBase, "VTS_") &&
			upperBase[6] == '_' &&
			upperBase[8:] == ".VOB" &&
			upperBase[7] >= '1' && upperBase[7] <= '9' {
			ifoPath := filepath.Join(filepath.Dir(cleaned), upperBase[:7]+"0.IFO")
			if fileExists(ifoPath) {
				return ifoPath, true
			}
		}
	}
	return "", false
}

// dvdMediaInfoTitleVOBPath 把 DVD 相关路径归一化为对应的首个标题 VOB 路径。
func dvdMediaInfoTitleVOBPath(path string) (string, bool) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", false
	}

	upperBase := strings.ToUpper(filepath.Base(cleaned))
	switch filepath.Ext(upperBase) {
	case ".VOB":
		if fileExists(cleaned) && looksLikeDVDSource(cleaned) {
			return cleaned, true
		}
	case ".IFO", ".BUP":
		if strings.EqualFold(upperBase, "VIDEO_TS.IFO") || strings.EqualFold(upperBase, "VIDEO_TS.BUP") {
			return "", false
		}
		if len(upperBase) == len("VTS_00_0.IFO") &&
			strings.HasPrefix(upperBase, "VTS_") &&
			upperBase[6] == '_' &&
			upperBase[7] == '0' {
			vobPath := filepath.Join(filepath.Dir(cleaned), upperBase[:7]+"1.VOB")
			if fileExists(vobPath) {
				return vobPath, true
			}
		}
	}
	return "", false
}

// dvdMediaInfoBUPPath 根据 IFO 路径返回对应的 BUP 路径。
func dvdMediaInfoBUPPath(path string) (string, bool) {
	cleaned := strings.TrimSpace(path)
	if !strings.EqualFold(filepath.Ext(cleaned), ".ifo") {
		return "", false
	}
	bupPath := strings.TrimSuffix(cleaned, filepath.Ext(cleaned)) + ".BUP"
	if !fileExists(bupPath) {
		return "", false
	}
	return bupPath, true
}

// fileExists 判断路径是否存在且为普通文件。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
