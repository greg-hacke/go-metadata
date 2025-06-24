package meta

import (
	"fmt"
	"io"
	"os"

	"greg-hacke/go-metadata/formats"
)

// Field represents a single metadata field
type Field struct {
	Namespace   string      // e.g., "EXIF", "IPTC", "XMP"
	Key         string      // Tag name or ID
	Value       interface{} // Actual value
	Description string      // Human-readable description
}

// ReadMetadata extracts metadata from a file
func ReadMetadata(filename string) ([]Field, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return ReadMetadataFrom(file, filename)
}

// ReadMetadataFrom extracts metadata from an io.ReadSeeker
func ReadMetadataFrom(r io.ReadSeeker, hint string) ([]Field, error) {
	// Determine file format
	format, err := formats.Sniff(r, hint)
	if err != nil {
		return nil, fmt.Errorf("failed to identify format: %w", err)
	}

	// Reset to beginning
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek: %w", err)
	}

	// Get appropriate parser
	parser := formats.GetParser(format)
	if parser == nil {
		return nil, fmt.Errorf("no parser available for format: %s", format)
	}

	// Extract metadata
	return parser.Parse(r)
}
