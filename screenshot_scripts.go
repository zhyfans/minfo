package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const screenshotScriptDir = "/opt/minfo/scripts"

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

	stdout, stderr, err := runCommand(ctx, "bash", append([]string{scriptPath, inputPath, outputDir}, screenshotVariantArgs(variant)...)...)
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

	stdout, stderr, err := runCommand(ctx, "bash", append([]string{autoScript, inputPath, outputDir}, screenshotVariantArgs(variant)...)...)
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
