package handlers

import (
	"context"
	"errors"

	"minfo/internal/config"
	"minfo/internal/system"
)

// run 会在后台执行具体的信息类任务，并在结束后更新任务状态和结果。
func (j *infoJob) run() {
	defer func() {
		if j.cancel != nil {
			j.cancel()
		}
		if j.cleanup != nil {
			j.cleanup()
		}
		if j.logger != nil {
			j.logger.Close()
		}
	}()

	if !j.beginRun() {
		if j.isCancellationRequested() {
			j.finishCanceled()
		}
		return
	}

	ctx, cancel := context.WithTimeout(j.taskContext, config.RequestTimeout)
	defer cancel()

	switch j.kind {
	case infoKindMediaInfo:
		bin, err := system.ResolveBin(system.MediaInfoBinaryPath)
		if err != nil {
			j.logger.Logf("[mediainfo] 未找到可执行文件: %s", err.Error())
			j.fail(err)
			return
		}
		j.logger.Logf("[mediainfo] 输入路径: %s", j.inputPath)
		j.logger.Logf("[mediainfo] 使用命令: %s", bin)

		output, err := runMediaInfo(ctx, j.inputPath, j.logger, bin)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed(output)
	case infoKindBDInfo:
		j.logger.Logf("[bdinfo] 输入路径: %s", j.inputPath)
		output, err := runBDInfo(ctx, j.inputPath, j.bdinfoMode, j.logger)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed(output)
	default:
		j.fail(errors.New("unsupported info job kind"))
	}
}
