package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	infoKindMediaInfo = "mediainfo"
	infoKindBDInfo    = "bdinfo"

	infoJobStatusPending   = "pending"
	infoJobStatusRunning   = "running"
	infoJobStatusCanceling = "canceling"
	infoJobStatusSucceeded = "succeeded"
	infoJobStatusFailed    = "failed"
	infoJobStatusCanceled  = "canceled"
	infoJobTTL             = 30 * time.Minute
)

var infoJobs = struct {
	mu    sync.Mutex
	items map[string]*infoJob
}{
	items: make(map[string]*infoJob),
}

type infoJob struct {
	mu          sync.RWMutex
	id          string
	kind        string
	inputPath   string
	bdinfoMode  string
	status      string
	output      string
	errMessage  string
	createdAt   time.Time
	updatedAt   time.Time
	completedAt time.Time
	logger      *infoLogger
	cleanup     func()
	taskContext context.Context
	cancel      context.CancelFunc

	cancelRequested bool
}

// createInfoJob 会创建一个新的信息类后台任务，并立即启动后台执行流程。
func createInfoJob(kind, inputPath string, cleanup func(), bdinfoMode string) (*infoJob, error) {
	pruneInfoJobs(time.Now())

	jobID, err := buildInfoJobID()
	if err != nil {
		return nil, err
	}

	taskContext, cancel := context.WithCancel(context.Background())
	now := time.Now()
	job := &infoJob{
		id:          jobID,
		kind:        kind,
		inputPath:   inputPath,
		bdinfoMode:  bdinfoMode,
		status:      infoJobStatusPending,
		createdAt:   now,
		updatedAt:   now,
		logger:      newInfoLogger(),
		cleanup:     cleanup,
		taskContext: taskContext,
		cancel:      cancel,
	}

	infoJobs.mu.Lock()
	infoJobs.items[jobID] = job
	infoJobs.mu.Unlock()

	go job.run()
	return job, nil
}

// getInfoJob 返回指定任务；如果任务不存在或已过期，则返回 false。
func getInfoJob(jobID string) (*infoJob, bool) {
	pruneInfoJobs(time.Now())

	infoJobs.mu.Lock()
	defer infoJobs.mu.Unlock()

	job, ok := infoJobs.items[jobID]
	if !ok {
		return nil, false
	}
	return job, true
}

// pruneInfoJobs 会删除已完成且超过保留时间的后台任务记录。
func pruneInfoJobs(now time.Time) {
	infoJobs.mu.Lock()
	defer infoJobs.mu.Unlock()

	for jobID, job := range infoJobs.items {
		if !job.expired(now) {
			continue
		}
		delete(infoJobs.items, jobID)
	}
}

// buildInfoJobID 生成适合 URL 使用的随机任务 ID。
func buildInfoJobID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
