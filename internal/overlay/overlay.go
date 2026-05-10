package overlay

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var ErrNoOverlayFiles = errors.New("no stream overlay files found")

var supportedExtensions = map[string]struct{}{
	".mp4":  {},
	".mov":  {},
	".mkv":  {},
	".webm": {},
}

func Discover(dir string) ([]string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("stream overlay path is not a directory")
	}

	files := make([]string, 0)
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
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	if len(files) == 0 {
		return nil, ErrNoOverlayFiles
	}
	return files, nil
}

func isSupportedVideo(path string) bool {
	_, ok := supportedExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}
