package scanner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var ErrNoSupportedFiles = errors.New("no supported files found")

var supportedExtensions = map[string]struct{}{
	".mp4":  {},
	".mov":  {},
	".mkv":  {},
	".webm": {},
}

func Scan(inputDir string, recursive bool) ([]string, error) {
	root, err := filepath.Abs(inputDir)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("input path is not a directory")
	}

	files := make([]string, 0)
	if recursive {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != root && isHidden(d.Name()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if isSupportedVideo(path) {
				files = append(files, path)
			}
			return nil
		})
	} else {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if isHidden(entry.Name()) {
				continue
			}
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if isSupportedVideo(path) {
				files = append(files, path)
			}
		}
	}
	if err != nil {
		return nil, err
	}

	slices.Sort(files)
	if len(files) == 0 {
		return nil, ErrNoSupportedFiles
	}
	return files, nil
}

func isSupportedVideo(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := supportedExtensions[ext]
	return ok
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}
