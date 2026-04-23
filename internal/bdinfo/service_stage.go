// Package bdinfo 负责调用 BDInfo 可执行文件并整理输出结果。

package bdinfo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"minfo/internal/config"
	"minfo/internal/system"
)

type stagedSource struct {
	workDir   string
	scanInput string
	cleanup   func()
}

// stageSource 为 BDInfo 准备工作目录和扫描目标，并返回对应的清理函数。
func stageSource(ctx context.Context, sourcePath string, options RunOptions) (stagedSource, error) {
	workDir, err := os.MkdirTemp("", "minfo-bdinfo-*")
	if err != nil {
		return stagedSource{}, err
	}

	cleanupFns := make([]func(), 0, 2)
	cleanup := func() {
		for index := len(cleanupFns) - 1; index >= 0; index-- {
			cleanupFns[index]()
		}
		_ = os.RemoveAll(workDir)
	}

	stage := stagedSource{
		workDir:   workDir,
		scanInput: sourcePath,
		cleanup:   cleanup,
	}

	sourceBDMV := detectBDMVSource(sourcePath)
	if sourceBDMV != "" {
		bindDir := filepath.Join(workDir, "source")
		bindTarget := filepath.Join(bindDir, "BDMV")
		if err := os.MkdirAll(bindTarget, 0o755); err != nil {
			cleanup()
			return stagedSource{}, err
		}
		if mounted, mountCleanup, mountErr := bindMountPath(ctx, sourceBDMV, bindTarget); mountErr == nil && mounted {
			cleanupFns = append(cleanupFns, mountCleanup)
			stage.scanInput = bindDir
			options.logf("[bdinfo] 已用 bind 挂载包装 BDMV 根: %s -> %s", sourceBDMV, bindTarget)
			return stage, nil
		}
		if err := os.Remove(bindTarget); err == nil {
			if err := os.Symlink(sourceBDMV, bindTarget); err == nil {
				stage.scanInput = bindDir
				options.logf("[bdinfo] bind 挂载不可用，已回退软链接包装 BDMV 根: %s -> %s", sourceBDMV, bindTarget)
				return stage, nil
			}
		}
		options.logf("[bdinfo] 包装 BDMV 根失败，直接使用原路径: %s", sourcePath)
		return stage, nil
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		cleanup()
		return stagedSource{}, err
	}
	if !info.IsDir() {
		return stage, nil
	}

	bindDir := filepath.Join(workDir, "source")
	if err := os.MkdirAll(bindDir, 0o755); err != nil {
		cleanup()
		return stagedSource{}, err
	}
	if mounted, mountCleanup, mountErr := bindMountPath(ctx, sourcePath, bindDir); mountErr == nil && mounted {
		cleanupFns = append(cleanupFns, mountCleanup)
		stage.scanInput = bindDir
		options.logf("[bdinfo] 已用 bind 挂载包装输入目录: %s -> %s", sourcePath, bindDir)
		return stage, nil
	}

	linkPath := filepath.Join(bindDir, "link")
	if err := os.Symlink(sourcePath, linkPath); err == nil {
		stage.scanInput = linkPath
		options.logf("[bdinfo] bind 挂载不可用，已回退软链接包装输入目录: %s -> %s", sourcePath, linkPath)
		return stage, nil
	}

	options.logf("[bdinfo] 无法包装输入目录，直接使用原路径: %s", sourcePath)
	return stage, nil
}

// detectBDMVSource 返回可直接交给 BDInfo 的 BDMV 根目录；找不到时返回空字符串。
func detectBDMVSource(path string) string {
	if directoryExists(filepath.Join(path, "BDMV")) {
		return filepath.Join(path, "BDMV")
	}
	if directoryExists(path) && strings.EqualFold(filepath.Base(path), "BDMV") {
		return path
	}
	return ""
}

// directoryExists 判断路径是否存在且为目录。
func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// bindMountPath 将源目录只读绑定挂载到目标路径，并返回卸载清理函数。
func bindMountPath(ctx context.Context, sourcePath, targetPath string) (bool, func(), error) {
	mountBin, err := system.ResolveBin(system.MountBinaryPath)
	if err != nil {
		return false, nil, err
	}
	umountBin, err := system.ResolveBin(system.UmountBinaryPath)
	if err != nil {
		return false, nil, err
	}

	mountCtx, cancel := context.WithTimeout(ctx, config.MountTimeout)
	defer cancel()

	if _, stderr, err := system.RunCommand(mountCtx, mountBin, "--bind", sourcePath, targetPath); err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = err.Error()
		}
		return false, nil, errors.New(message)
	}

	cleanup := func() {
		umountCtx, cancel := context.WithTimeout(context.Background(), config.UmountTimeout)
		defer cancel()
		if _, _, err := system.RunCommand(umountCtx, umountBin, targetPath); err != nil {
			_, _, _ = system.RunCommand(umountCtx, umountBin, "-l", targetPath)
		}
	}
	return true, cleanup, nil
}

// findReportFile 会查找报告文件，并在多个候选项中返回最合适的结果。
func findReportFile(workDir string) (string, error) {
	type candidate struct {
		path    string
		modTime time.Time
		isText  bool
	}

	candidates := make([]candidate, 0, 4)
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path:    filepath.Join(workDir, entry.Name()),
			modTime: info.ModTime(),
			isText:  strings.EqualFold(filepath.Ext(entry.Name()), ".txt"),
		})
	}
	if len(candidates) == 0 {
		return "", errors.New("bdinfo: no report file produced")
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].isText != candidates[j].isText {
			return candidates[i].isText
		}
		if candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].path, nil
}
