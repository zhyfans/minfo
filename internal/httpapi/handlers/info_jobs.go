// Package handlers 提供 MediaInfo 和 BDInfo 后台任务的创建、取消与状态查询接口。

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"minfo/internal/httpapi/transport"
)

// InfoJobsHandler 负责创建新的 MediaInfo 或 BDInfo 后台任务，并立即返回任务 ID。
func InfoJobsHandler(w http.ResponseWriter, r *http.Request) {
	if !transport.EnsurePost(w, r) {
		return
	}
	if err := transport.ParseForm(w, r); err != nil {
		writeInfoJobError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer transport.CleanupMultipart(r)

	kind := normalizeInfoJobKind(r.FormValue("kind"))
	if kind == "" {
		writeInfoJobError(w, http.StatusBadRequest, "invalid info job kind")
		return
	}

	inputPath, cleanup, err := transport.InputPath(r)
	if err != nil {
		writeInfoJobError(w, http.StatusBadRequest, err.Error())
		return
	}

	job, err := createInfoJob(kind, inputPath, cleanup, r.FormValue("bdinfo_mode"))
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		writeInfoJobError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInfoJobResponse(w, http.StatusAccepted, job.snapshot())
}

// InfoJobHandler 返回信息类后台任务当前状态，或处理对已有任务的取消请求。
func InfoJobHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleInfoJobGet(w, r)
	case http.MethodDelete:
		handleInfoJobDelete(w, r)
	default:
		writeInfoJobError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleInfoJobGet 返回信息类后台任务当前状态，也会在完成后带上最终输出和日志。
func handleInfoJobGet(w http.ResponseWriter, r *http.Request) {
	jobID := parseInfoJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getInfoJob(jobID)
	if !ok {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	writeInfoJobResponse(w, http.StatusOK, job.snapshot())
}

// handleInfoJobDelete 会请求取消指定的信息类后台任务，并返回取消后的最新快照。
func handleInfoJobDelete(w http.ResponseWriter, r *http.Request) {
	jobID := parseInfoJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getInfoJob(jobID)
	if !ok {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job.requestCancel()
	writeInfoJobResponse(w, http.StatusOK, job.snapshot())
}

// parseInfoJobID 会从请求路径中提取信息类后台任务 ID。
func parseInfoJobID(r *http.Request) string {
	jobID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/info-jobs/"))
	return strings.Trim(jobID, "/")
}

// normalizeInfoJobKind 规范化信息类任务种类；不支持的值返回空字符串。
func normalizeInfoJobKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case infoKindMediaInfo:
		return infoKindMediaInfo
	case infoKindBDInfo:
		return infoKindBDInfo
	default:
		return ""
	}
}

// writeInfoJobResponse 会把信息类后台任务响应编码为 JSON，并显式关闭缓存。
func writeInfoJobResponse(w http.ResponseWriter, status int, payload transport.InfoJobResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeInfoJobError 会把信息类后台任务错误包装成统一 JSON 响应。
func writeInfoJobError(w http.ResponseWriter, status int, message string) {
	writeInfoJobResponse(w, status, transport.InfoJobResponse{
		OK:    false,
		Error: message,
	})
}
