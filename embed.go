package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:embed
var embeddedFS embed.FS

func extractEmbeddedFiles(targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	return fs.WalkDir(embeddedFS, "embed", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel("embed", path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, relPath)

		if _, err := os.Stat(targetPath); err == nil {
			// File exists, skip
			return nil
		}


		data, err := embeddedFS.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(targetPath, data, 0755)
	})
}
