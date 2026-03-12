package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const screenshotScriptDir = "/opt/minfo/scripts"
const screenshotCount = 4

const (
	screenshotModeZip   = "zip"
	screenshotModeLinks = "links"

	screenshotVariantPNG  = "png"
	screenshotVariantJPG  = "jpg"
	screenshotVariantFast = "fast"
)

func requestedScreenshotMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case screenshotModeLinks:
		return screenshotModeLinks
	default:
		return screenshotModeZip
	}
}

func requestedScreenshotVariant(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case screenshotVariantJPG:
		return screenshotVariantJPG
	case screenshotVariantFast:
		return screenshotVariantFast
	default:
		return screenshotVariantPNG
	}
}

func screenshotVariantArgs(variant string) []string {
	switch variant {
	case screenshotVariantJPG:
		return []string{"-jpg"}
	case screenshotVariantFast:
		return []string{"-fast"}
	default:
		return nil
	}
}

func screenshotScriptName(variant string) string {
	switch variant {
	case screenshotVariantJPG:
		return "screenshots_jpg.sh"
	case screenshotVariantFast:
		return "screenshots_fast.sh"
	default:
		return "screenshots.sh"
	}
}

func resolveScript(envKey, fallbackName string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		info, err := os.Stat(value)
		if err != nil {
			return "", fmt.Errorf("%s not found: %v", envKey, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s must point to a file", envKey)
		}
		return value, nil
	}

	candidate := filepath.Join(screenshotScriptDir, fallbackName)
	info, err := os.Stat(candidate)
	if err == nil && !info.IsDir() {
		return candidate, nil
	}

	return "", fmt.Errorf("%s not found in %s; set %s to override", fallbackName, screenshotScriptDir, envKey)
}

func runScreenshotScript(ctx context.Context, inputPath, outputDir, variant string) ([]string, error) {
	scriptPath, err := resolveScript("SCREENSHOT_SCRIPT", screenshotScriptName(variant))
	if err != nil {
		return nil, err
	}

	timestamps, err := randomScreenshotTimestamps(ctx, inputPath, screenshotCount)
	if err != nil {
		return nil, err
	}

	args := append([]string{scriptPath, inputPath, outputDir}, timestamps...)
	stdout, stderr, err := runCommand(ctx, "bash", args...)
	if err != nil {
		return nil, fmt.Errorf("screenshot generation failed: %s", bestErrorMessage(err, stderr, stdout))
	}

	files, err := listScreenshotFiles(outputDir)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func runScreenshotUpload(ctx context.Context, inputPath, outputDir, variant string) (string, error) {
	autoScript, err := resolveScript("SCREENSHOT_AUTO_SCRIPT", "AutoScreenshot.sh")
	if err != nil {
		return "", err
	}

	timestamps, err := randomScreenshotTimestamps(ctx, inputPath, screenshotCount)
	if err != nil {
		return "", err
	}

	args := append(screenshotVariantArgs(variant), inputPath, outputDir)
	args = append(args, timestamps...)
	stdout, stderr, err := runCommand(ctx, "bash", append([]string{autoScript}, args...)...)
	if err != nil {
		return "", fmt.Errorf("screenshot upload failed: %s", bestErrorMessage(err, stderr, stdout))
	}

	links := extractDirectLinks(stdout)
	if len(links) == 0 {
		output := strings.TrimSpace(stdout)
		if output == "" {
			output = strings.TrimSpace(stderr)
		}
		if output == "" {
			return "", errors.New("pixhost upload completed but returned no links")
		}
		return output, nil
	}

	return strings.Join(links, "\n"), nil
}

func randomScreenshotTimestamps(ctx context.Context, inputPath string, count int) ([]string, error) {
	if count <= 0 {
		count = screenshotCount
	}

	ffprobe, err := resolveBin("FFPROBE_BIN", "ffprobe")
	if err != nil {
		return nil, err
	}

	sourcePath, cleanup, err := resolveScreenshotSource(ctx, inputPath)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	duration, err := probeMediaDuration(ctx, ffprobe, sourcePath)
	if err != nil {
		return nil, err
	}

	seconds := buildRandomTimestampSeconds(duration, count)
	timestamps := make([]string, 0, len(seconds))
	for _, second := range seconds {
		timestamps = append(timestamps, formatScriptTimestamp(second))
	}
	return timestamps, nil
}

func probeMediaDuration(ctx context.Context, ffprobe, path string) (float64, error) {
	stdout, stderr, err := runCommand(ctx, ffprobe,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %s", bestErrorMessage(err, stderr, stdout))
	}

	value := strings.TrimSpace(stdout)
	if value == "" {
		return 0, errors.New("ffprobe returned empty duration")
	}

	duration, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %v", err)
	}
	if duration <= 0 {
		return 0, errors.New("duration must be positive")
	}
	return duration, nil
}

func buildRandomTimestampSeconds(duration float64, count int) []int {
	if count <= 0 {
		count = screenshotCount
	}

	start := 0.0
	end := duration
	if duration > 120 {
		margin := duration * 0.08
		if margin < 15 {
			margin = 15
		}
		if margin > 300 {
			margin = 300
		}
		start = margin
		end = duration - margin
		if end <= start {
			start = 0
			end = duration
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	step := (end - start) / float64(count)
	if step <= 0 {
		step = duration / float64(count+1)
	}

	values := make([]int, 0, count)
	used := make(map[int]struct{}, count)
	for index := 0; index < count; index++ {
		segmentStart := start + step*float64(index)
		segmentEnd := segmentStart + step
		if index == count-1 || segmentEnd > end {
			segmentEnd = end
		}
		if segmentEnd <= segmentStart {
			segmentEnd = segmentStart + 1
		}

		value := int(segmentStart + rng.Float64()*(segmentEnd-segmentStart))
		if value < 0 {
			value = 0
		}
		maxSecond := int(duration)
		if maxSecond > 0 && value >= maxSecond {
			value = maxSecond - 1
		}
		for try := 0; try < 8; try++ {
			if _, exists := used[value]; !exists {
				break
			}
			value++
		}
		used[value] = struct{}{}
		values = append(values, value)
	}

	sort.Ints(values)
	return values
}

func formatScriptTimestamp(totalSeconds int) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func listScreenshotFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		switch ext {
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

func extractDirectLinks(output string) []string {
	lines := strings.Split(output, "\n")
	links := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			continue
		}
		if strings.ContainsAny(line, " []()<>\"") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		links = append(links, line)
	}

	return links
}
