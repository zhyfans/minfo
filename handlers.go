package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func infoHandler(envKey, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ensurePost(w, r) {
			return
		}
		if err := parseForm(w, r); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanupMultipart(r)

		path, cleanup, err := inputPath(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanup()

		bin, err := resolveBin(envKey, fallback)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()

		stdout, stderr, err := runCommand(ctx, bin, path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, bestErrorMessage(err, stderr, stdout))
			return
		}

		output := strings.TrimSpace(stdout)
		if strings.TrimSpace(stderr) != "" {
			if output != "" {
				output += "\n\n"
			}
			output += strings.TrimSpace(stderr)
		}
		if output == "" {
			writeError(w, http.StatusInternalServerError, "mediainfo returned empty output")
			return
		}

		writeJSON(w, http.StatusOK, infoResponse{OK: true, Output: output})
	}
}

func mediainfoHandler(envKey, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ensurePost(w, r) {
			return
		}
		if err := parseForm(w, r); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanupMultipart(r)

		path, cleanup, err := inputPath(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanup()

		bin, err := resolveBin(envKey, fallback)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()

		candidates, sourceCleanup, err := resolveMediaInfoCandidates(ctx, path, mediaInfoCandidateLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer sourceCleanup()

		var lastErr string
		for _, sourcePath := range candidates {
			stdout, stderr, err := runCommand(ctx, bin, sourcePath)
			if err != nil {
				lastErr = bestErrorMessage(err, stderr, stdout)
				continue
			}

			output := strings.TrimSpace(stdout)
			if strings.TrimSpace(stderr) != "" {
				if output != "" {
					output += "\n\n"
				}
				output += strings.TrimSpace(stderr)
			}
			if output == "" {
				lastErr = fmt.Sprintf("mediainfo returned empty output for: %s", sourcePath)
				continue
			}

			writeJSON(w, http.StatusOK, infoResponse{OK: true, Output: output})
			return
		}

		if lastErr == "" {
			lastErr = "mediainfo returned empty output"
		}
		writeError(w, http.StatusInternalServerError, lastErr)
	}
}

func bdinfoHandler(envKey, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ensurePost(w, r) {
			return
		}
		if err := parseForm(w, r); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanupMultipart(r)

		path, cleanup, err := inputPath(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanup()

		bin, err := resolveBin(envKey, fallback)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()

		bdPath, bdCleanup, err := resolveBDInfoSource(ctx, path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer bdCleanup()

		stdout, stderr, err := runCommand(ctx, bin, bdPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, bestErrorMessage(err, stderr, stdout))
			return
		}

		output := strings.TrimSpace(stdout)
		if strings.TrimSpace(stderr) != "" {
			if output != "" {
				output += "\n\n"
			}
			output += strings.TrimSpace(stderr)
		}

		writeJSON(w, http.StatusOK, infoResponse{OK: true, Output: output})
	}
}

func screenshotsHandler(w http.ResponseWriter, r *http.Request) {
	if !ensurePost(w, r) {
		return
	}
	if err := parseForm(w, r); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanupMultipart(r)

	path, cleanup, err := inputPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanup()
	mode := requestedScreenshotMode(r.FormValue("mode"))
	variant := requestedScreenshotVariant(r.FormValue("variant"))

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "minfo-shots-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	if mode == screenshotModeLinks {
		output, err := runScreenshotUpload(ctx, path, tempDir, variant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, infoResponse{OK: true, Output: output})
		return
	}

	files, err := runScreenshotScript(ctx, path, tempDir, variant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	zipBytes, err := zipFiles(files)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"screenshots.zip\"")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(zipBytes); err != nil {
		log.Printf("write response: %v", err)
	}
}

func pathSuggestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePathError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	roots, err := resolveRoots(mediaRoots())
	if err != nil {
		writePathError(w, http.StatusBadRequest, err.Error())
		return
	}
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	prefix = strings.Trim(prefix, "\"")

	items, root, err := suggestPaths(roots, prefix, maxSuggestions)
	if err != nil {
		writePathError(w, http.StatusBadRequest, err.Error())
		return
	}

	writePathJSON(w, http.StatusOK, pathResponse{
		OK:    true,
		Root:  root,
		Roots: roots,
		Items: items,
	})
}

func suggestPaths(roots []string, prefix string, limit int) ([]string, string, error) {
	if len(roots) == 0 {
		return nil, "", errors.New("no MEDIA_ROOT configured")
	}
	resolvedRoots := roots

	if prefix == "" {
		if len(resolvedRoots) == 1 {
			items, err := listDir(resolvedRoots[0], "", limit)
			return items, resolvedRoots[0], err
		}
		items := make([]string, 0, len(resolvedRoots))
		for _, root := range resolvedRoots {
			items = append(items, withDirSuffix(root))
		}
		return items, "", nil
	}

	cleaned := filepath.Clean(prefix)
	selectedRoot := ""
	var absPrefix string
	if filepath.IsAbs(cleaned) {
		absPrefix = cleaned
		matchedRoot, ok := findContainingRoot(resolvedRoots, absPrefix)
		if !ok {
			return nil, "", errors.New("path is outside MEDIA_ROOTS")
		}
		selectedRoot = matchedRoot
	} else {
		if len(resolvedRoots) != 1 {
			return nil, "", errors.New("relative path requires a single MEDIA_ROOT")
		}
		selectedRoot = resolvedRoots[0]
		absPrefix = filepath.Join(selectedRoot, cleaned)
	}

	sep := string(filepath.Separator)
	if strings.HasSuffix(prefix, sep) || strings.HasSuffix(prefix, "/") || strings.HasSuffix(prefix, "\\") {
		if !isSubpath(selectedRoot, absPrefix) {
			return nil, "", errors.New("path is outside MEDIA_ROOTS")
		}
		items, err := listDir(absPrefix, "", limit)
		return items, selectedRoot, err
	}

	dir := filepath.Dir(absPrefix)
	base := filepath.Base(absPrefix)
	if !isSubpath(selectedRoot, dir) {
		return nil, "", errors.New("path is outside MEDIA_ROOTS")
	}
	items, err := listDir(dir, base, limit)
	return items, selectedRoot, err
}

func resolveRoots(roots []string) ([]string, error) {
	resolved := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		clean := filepath.Clean(strings.TrimSpace(root))
		if clean == "" {
			continue
		}
		absRoot, err := filepath.Abs(clean)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[absRoot]; ok {
			continue
		}
		seen[absRoot] = struct{}{}
		resolved = append(resolved, absRoot)
	}
	if len(resolved) == 0 {
		return nil, errors.New("no MEDIA_ROOT configured")
	}
	return resolved, nil
}

func findContainingRoot(roots []string, path string) (string, bool) {
	best := ""
	for _, root := range roots {
		if !isSubpath(root, path) {
			continue
		}
		if len(root) > len(best) {
			best = root
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

func withDirSuffix(path string) string {
	if strings.HasSuffix(path, string(filepath.Separator)) {
		return path
	}
	return path + string(filepath.Separator)
}

func listDir(dir, base string, limit int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	baseLower := strings.ToLower(base)
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if baseLower != "" && !strings.Contains(strings.ToLower(name), baseLower) {
			continue
		}
		full := filepath.Join(dir, name)
		if entry.IsDir() {
			full += string(filepath.Separator)
		}
		items = append(items, full)
		if limit > 0 && len(items) >= limit {
			break
		}
	}

	return items, nil
}

func isSubpath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
