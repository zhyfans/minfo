// Package delivery 提供截图结果文件整理、打包与下载缓存能力。

package delivery

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListImageFiles 会列出截图文件，并按当前规则返回排序后的结果列表。
func ListImageFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp":
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no screenshots were generated")
	}

	sort.Strings(files)
	return files, nil
}
