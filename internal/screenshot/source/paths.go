// Package source 提供截图流程需要的蓝光/DVD 路径与播放列表探测辅助函数。

package source

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var clipIDPattern = regexp.MustCompile(`[0-9]{5}M2TS`)

// LooksLikeDVDSource 通过路径特征判断输入是否看起来像 DVD VIDEO_TS 源。
func LooksLikeDVDSource(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	base := strings.ToLower(filepath.Base(lower))
	parent := filepath.Base(filepath.Dir(path))
	if strings.Contains(lower, "/video_ts/") || strings.EqualFold(parent, "VIDEO_TS") {
		return true
	}
	if strings.HasSuffix(base, ".ifo") || strings.HasSuffix(base, ".vob") || strings.HasSuffix(base, ".bup") {
		return strings.EqualFold(base, "video_ts.ifo") ||
			strings.EqualFold(base, "video_ts.vob") ||
			strings.EqualFold(base, "video_ts.bup") ||
			strings.HasPrefix(base, "vts_")
	}
	return false
}

// FindBlurayRootFromVideo 从视频路径向上回溯，定位对应的蓝光根目录。
func FindBlurayRootFromVideo(videoPath string) (string, bool) {
	current := filepath.Dir(videoPath)
	for {
		if current == "/" || current == "." || current == "" {
			return "", false
		}
		if info, err := os.Stat(filepath.Join(current, "BDMV", "STREAM")); err == nil && info.IsDir() {
			return current, true
		}
		if strings.EqualFold(filepath.Base(current), "BDMV") {
			if info, err := os.Stat(filepath.Join(current, "STREAM")); err == nil && info.IsDir() {
				return filepath.Dir(current), true
			}
		}
		next := filepath.Dir(current)
		if next == current {
			return "", false
		}
		current = next
	}
}

type playlistScore struct {
	Name      string
	Contains  bool
	TotalSize int64
	ClipCount int
	FileSize  int64
}

// ListBlurayPlaylistsRanked 按片段命中情况和总大小为蓝光播放列表排序。
func ListBlurayPlaylistsRanked(root, clip string) []string {
	playlistDir := filepath.Join(root, "BDMV", "PLAYLIST")
	streamDir := filepath.Join(root, "BDMV", "STREAM")

	playlistEntries, err := os.ReadDir(playlistDir)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(streamDir); err != nil || !info.IsDir() {
		return nil
	}

	scores := make([]playlistScore, 0)
	for _, entry := range playlistEntries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".mpls") {
			continue
		}

		path := filepath.Join(playlistDir, entry.Name())
		clipIDs := extractMPLSClipIDs(path)
		if len(clipIDs) == 0 {
			continue
		}

		totalSize := int64(0)
		contains := false
		for _, clipID := range clipIDs {
			if clipID == clip {
				contains = true
			}
			if info, err := os.Stat(filepath.Join(streamDir, clipID+".m2ts")); err == nil {
				totalSize += info.Size()
			}
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		scores = append(scores, playlistScore{
			Name:      strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Contains:  contains,
			TotalSize: totalSize,
			ClipCount: len(clipIDs),
			FileSize:  info.Size(),
		})
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Contains != scores[j].Contains {
			return scores[i].Contains
		}
		if scores[i].TotalSize != scores[j].TotalSize {
			return scores[i].TotalSize > scores[j].TotalSize
		}
		if scores[i].ClipCount != scores[j].ClipCount {
			return scores[i].ClipCount > scores[j].ClipCount
		}
		if scores[i].FileSize != scores[j].FileSize {
			return scores[i].FileSize > scores[j].FileSize
		}
		return scores[i].Name < scores[j].Name
	})

	playlists := make([]string, 0, len(scores))
	for _, score := range scores {
		playlists = append(playlists, score.Name)
	}
	return playlists
}

func extractMPLSClipIDs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	matches := clipIDPattern.FindAllString(string(data), -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		clipID := strings.TrimSuffix(match, "M2TS")
		if _, ok := seen[clipID]; ok {
			continue
		}
		seen[clipID] = struct{}{}
		ids = append(ids, clipID)
	}
	return ids
}
