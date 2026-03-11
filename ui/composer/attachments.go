package composer

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Attachment represents a file attached to the outgoing email.
type Attachment struct {
	Path        string // path to file on disk (may be a temp zip)
	DisplayName string // shown in UI
	IsDir       bool   // was originally a directory
	Compressed  bool   // was zipped
	TempFile    bool   // Path is a temp file to clean up after sending
}

// sendName returns the filename to use when sending.
// If anonymize is true, files are named "01", "02", ... with their extension.
func (a *Attachment) sendName(idx int, anonymize bool) string {
	if !anonymize {
		return a.DisplayName
	}
	ext := filepath.Ext(a.DisplayName)
	return fmt.Sprintf("%02d%s", idx+1, ext)
}

// zipDir compresses srcDir into a temp zip file and returns the zip path.
func zipDir(srcDir string) (string, error) {
	name := filepath.Base(srcDir) + ".zip"
	tmp, err := os.CreateTemp("", name)
	if err != nil {
		return "", fmt.Errorf("creating temp zip: %w", err)
	}
	tmpName := tmp.Name()

	w := zip.NewWriter(tmp)

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := w.Create(rel)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(f, src)
		return err
	})

	w.Close()
	tmp.Close()

	if walkErr != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("zipping directory: %w", walkErr)
	}
	return tmpName, nil
}
