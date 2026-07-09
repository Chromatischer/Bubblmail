package util

import (
	"path/filepath"
	"strings"
)

// SafeAttachmentFilename returns a local filesystem-safe basename for an
// attachment, falling back to a stable generic name when the source is empty.
func SafeAttachmentFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "attachment"
	}
	return name
}
