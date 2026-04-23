// Package screenshot 提供 DVD mediainfo 缓存逻辑。

package screenshot

import (
	"strings"

	screenshotdvdinfo "minfo/internal/screenshot/dvdinfo"
	screenshotsource "minfo/internal/screenshot/source"
)

// ensureDVDMediaInfoResult 在 DVD 场景下只探测一次 mediainfo 结果，并缓存供后续字幕与比例逻辑复用。
func (r *screenshotRunner) ensureDVDMediaInfoResult() (dvdMediaInfoResult, bool, error) {
	if r == nil || strings.TrimSpace(r.tools.MediaInfoBin) == "" {
		return dvdMediaInfoResult{}, false, nil
	}
	if !screenshotsource.LooksLikeDVDSource(r.sourcePath) {
		return dvdMediaInfoResult{}, false, nil
	}
	if r.subtitleState.HasDVDMediaInfoResult {
		return r.subtitleState.DVDMediaInfoResult, true, nil
	}

	result, err := screenshotdvdinfo.Probe(r.ctx, r.tools.MediaInfoBin, r.sourcePath, r.dvdMediaInfoPath)
	if err != nil {
		return dvdMediaInfoResult{}, false, err
	}
	r.subtitleState.DVDMediaInfoResult = result
	r.subtitleState.HasDVDMediaInfoResult = true
	return result, true, nil
}
