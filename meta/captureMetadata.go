package meta

import (
	"encoding/json"
	"fmt"
	"io"

	"greg-hacke/go-metadata/tags"
)

// Metadata represents extracted metadata
type Metadata struct {
	// Dynamic storage for all metadata
	Fields map[string]interface{} `json:"fields"`
}

// ToJSON converts metadata to JSON string
func (m *Metadata) ToJSON() (string, error) {
	jsonBytes, err := json.MarshalIndent(m.Fields, "", "  ")
	if err != nil {
		return "{}", err
	}
	return string(jsonBytes), nil
}

// CaptureMetadata extracts metadata dynamically based on tag tables
func CaptureMetadata(file io.ReadSeeker, fileType *FileType, tagTables []*tags.TagTable, requested MetadataRequest) (*Metadata, error) {
	fmt.Printf("\n=== Dynamic Metadata Extraction ===\n")

	// Initialize metadata with dynamic fields
	metadata := &Metadata{
		Fields: make(map[string]interface{}),
	}

	// Add basic file type info
	metadata.Fields["FileType"] = fileType.Format
	if fileType.Description != "" {
		metadata.Fields["FileTypeDescription"] = fileType.Description
	}

	// Add MIME type if available
	if mime, ok := tags.ExifToolFileTypes.MimeTypes[fileType.Format]; ok {
		metadata.Fields["MIMEType"] = mime
	}

	// Reset file position
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to start: %w", err)
	}

	// Read file data (limited to 50MB)
	fileData := make([]byte, 50*1024*1024)
	n, err := file.Read(fileData)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	fileData = fileData[:n]

	fmt.Printf("Scanning %d bytes for metadata patterns...\n", len(fileData))

	// Create extractor and process
	extractor := NewMetadataExtractor(fileData, metadata, tagTables)
	foundEmbedded, foundContainer := extractor.ExtractAll()

	if !foundEmbedded && !foundContainer {
		fmt.Println("No metadata patterns found")
	} else {
		fmt.Printf("Extracted %d metadata fields\n", len(metadata.Fields)-3) // -3 for the file type fields
	}

	return metadata, nil
}
