// Package handlers 提供 MediaInfo 和 BDInfo 信息接口。

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"minfo/internal/bdinfo"
	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/media"
	"minfo/internal/system"
)

// MediaInfoHandler 返回处理 MediaInfo 请求的 HTTP Handler，并在候选源之间重试直到拿到有效输出。
func MediaInfoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !transport.EnsurePost(w, r) {
			return
		}
		if err := transport.ParseForm(w, r); err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer transport.CleanupMultipart(r)

		logger := newInfoLogger()
		defer logger.Close()

		path, cleanup, err := transport.InputPath(r)
		if err != nil {
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		defer cleanup()
		logger.Logf("[mediainfo] 输入路径: %s", path)

		bin, err := system.ResolveBin(system.MediaInfoBinaryPath)
		if err != nil {
			logger.Logf("[mediainfo] 未找到可执行文件: %s", err.Error())
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		logger.Logf("[mediainfo] 使用命令: %s", bin)

		ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
		defer cancel()

		output, err := runMediaInfo(ctx, path, logger, bin)
		if err != nil {
			writeInfoError(w, http.StatusInternalServerError, err.Error(), logger)
			return
		}

		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{
			OK:         true,
			Output:     output,
			Logs:       logger.String(),
			LogEntries: logger.Entries(),
		})
	}
}

// BDInfoHandler 返回处理 BDInfo 请求的 HTTP Handler，并把报告内容按前端模式整理成统一 JSON 响应。
func BDInfoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !transport.EnsurePost(w, r) {
			return
		}
		if err := transport.ParseForm(w, r); err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer transport.CleanupMultipart(r)

		logger := newInfoLogger()
		defer logger.Close()

		path, cleanup, err := transport.InputPath(r)
		if err != nil {
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		defer cleanup()
		logger.Logf("[bdinfo] 输入路径: %s", path)

		ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
		defer cancel()

		output, err := runBDInfo(ctx, path, r.FormValue("bdinfo_mode"), logger)
		if err != nil {
			writeInfoError(w, http.StatusInternalServerError, err.Error(), logger)
			return
		}
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{
			OK:         true,
			Output:     output,
			Logs:       logger.String(),
			LogEntries: logger.Entries(),
		})
	}
}

// shouldExtractBDInfoCode 会根据表单里的输出模式判断是否只返回精简代码块。
func shouldExtractBDInfoCode(mode string) bool {
	return strings.TrimSpace(strings.ToLower(mode)) != "full"
}

// runMediaInfo 会执行完整的 MediaInfo 探测流程，并返回最终输出文本。
func runMediaInfo(ctx context.Context, path string, logger *infoLogger, bin string) (string, error) {
	candidates, sourceCleanup, err := media.ResolveMediaInfoCandidates(ctx, path, media.MediaInfoCandidateLimit)
	if err != nil {
		logger.Logf("[mediainfo] 解析候选源失败: %s", err.Error())
		return "", err
	}
	defer sourceCleanup()
	logger.Logf("[mediainfo] 候选源数量: %d", len(candidates))

	var lastErr string
	for idx, sourcePath := range candidates {
		sourceDir := filepath.Dir(sourcePath)
		sourceName := filepath.Base(sourcePath)
		logger.Logf("[mediainfo] 尝试 %d/%d: %s", idx+1, len(candidates), sourcePath)
		logger.Logf("[mediainfo] 执行命令: cwd=%s | %s", sourceDir, formatCommand(bin, sourceName))

		stdout, stderr, err := system.RunCommandInDirLive(ctx, sourceDir, bin, logger.CommandOutput("mediainfo"), sourceName)
		if err != nil {
			lastErr = system.BestErrorMessage(err, stderr, stdout)
			logger.LogMultiline("[mediainfo][error] ", lastErr)
			continue
		}

		output := system.CombineCommandOutput(stdout, stderr)
		if output == "" {
			lastErr = fmt.Sprintf("mediainfo returned empty output for: %s", sourcePath)
			logger.Logf("[mediainfo] 返回空输出: %s", sourcePath)
			continue
		}

		logger.Logf("[mediainfo] 完成: %s", sourcePath)
		return output, nil
	}

	if lastErr == "" {
		lastErr = "mediainfo returned empty output"
	}
	return "", fmt.Errorf("%s", lastErr)
}

// runBDInfo 会执行完整的 BDInfo 探测流程，并按请求模式返回精简或完整输出。
func runBDInfo(ctx context.Context, path, mode string, logger *infoLogger) (string, error) {
	result, err := bdinfo.Run(ctx, path, bdinfo.RunOptions{
		CommandOutput: logger.CommandOutput("bdinfo"),
		Logf:          logger.Logf,
	})
	if err != nil {
		logger.LogMultiline("[bdinfo][error] ", err.Error())
		return "", err
	}

	output := result.Output
	if shouldExtractBDInfoCode(mode) {
		logger.Logf("[bdinfo] 输出模式: 精简报告")
		output = bdinfo.ExtractCodeBlock(output)
	} else {
		logger.Logf("[bdinfo] 输出模式: 完整报告")
	}

	logger.Logf("[bdinfo] 完成: %s", result.ResolvedPath)
	return output, nil
}

// formatCommand 会把命令和参数格式化成便于日志展示的可读字符串。
func formatCommand(bin string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(bin))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

// quoteArg 会在需要时为单个命令参数补上引号和转义。
func quoteArg(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\r\n\"'\\") {
		return strconv.Quote(value)
	}
	return value
}
