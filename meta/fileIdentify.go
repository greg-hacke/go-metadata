package meta

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"greg-hacke/go-metadata/tags"
)

// MetadataRequest represents requested metadata fields
// Key is the JSON path, Value indicates if requested (future: could hold options)
type MetadataRequest map[string]bool

// FileType represents the identified file format
type FileType struct {
	Format   string // Primary format from tags
	Module   string // Module name from tags
	Category string // container, embedded, or both
}

// ProcessFileByPath processes a file from its path
func ProcessFileByPath(filepath string, requested MetadataRequest) error {
	// Check if file exists
	info, err := os.Stat(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", filepath)
		}
		return fmt.Errorf("cannot access file: %w", err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", filepath)
	}

	// Open the file
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	// Pass to processFileRaw with filepath for extension-based detection
	return processFileRawWithPath(file, filepath, requested)
}

// ProcessFileRaw processes an already opened file
func ProcessFileRaw(file io.ReadSeeker, requested MetadataRequest) error {
	// Without a filepath, we can't use extension-based detection
	return processFileRawWithPath(file, "", requested)
}

// processFileRawWithPath processes a file with optional path for extension detection
func processFileRawWithPath(file io.ReadSeeker, filePath string, requested MetadataRequest) error {
	// Ensure we're at the beginning
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("cannot seek to start: %w", err)
	}

	// Identify the file type using tags data
	fileType, err := identifyFileWithPath(file, filePath)
	if err != nil {
		return fmt.Errorf("cannot identify file: %w", err)
	}

	// Reset to beginning for actual processing
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("cannot seek to start: %w", err)
	}

	// Log identified file information
	fmt.Printf("Format:        %s\n", fileType.Format)
	fmt.Printf("Module:        %s\n", fileType.Module)
	fmt.Printf("Category:      %s\n", fileType.Category)

	// TODO: Based on fileType, dynamically call appropriate metadata extraction
	// if fileType.Category == "container" {
	//     return captureMetadataContainerized(file, fileType, requested)
	// } else {
	//     return captureMetadataEmbedded(file, fileType, requested)
	// }

	return nil
}

// identifyFileWithPath determines file type using tags and optional file path
func identifyFileWithPath(file io.ReadSeeker, filePath string) (*FileType, error) {
	// Read initial bytes for identification
	header := make([]byte, 1024)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("cannot read file header: %w", err)
	}
	header = header[:n]

	// Reset position
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Header bytes:  %X", header[:32])
	if n > 32 {
		fmt.Printf("...")
	}
	fmt.Println()

	// Use tags.AllTags to dynamically identify format
	fmt.Println("Checking tag tables for file identification...")
	for tableName, table := range tags.AllTags {
		if fileType := checkTableForFileType(header, tableName, table); fileType != nil {
			fmt.Printf("Matched table: %s\n", tableName)
			return fileType, nil
		}
	}

	// Try to detect based on header alone
	fmt.Println("No table match found, checking by header signature...")

	// Since we have no magic bytes in tags, we cannot detect by header
	// This is the truly dynamic approach - no data means no detection

	// If no specific format identified, return unknown
	fmt.Println("No format match found")
	return &FileType{
		Format:   "UNKNOWN",
		Module:   "",
		Category: "unknown",
	}, nil
}

// identifyFile is the original function for backward compatibility
func identifyFile(file io.ReadSeeker) (*FileType, error) {
	return identifyFileWithPath(file, "")
}

// checkTableForFileType checks if a tag table matches the file header
func checkTableForFileType(header []byte, tableName string, table *tags.TagTable) *FileType {
	// Look for file identification tags in the table
	// These might be marked with special groups or have specific IDs

	// Check for file type identification markers in the table
	for _, tag := range table.Tags {
		// Look for tags that indicate file format detection
		// This might be in Groups["_file_magic"] or similar metadata
		if tag.Groups != nil {
			if _, hasFileMagic := tag.Groups["_file_magic"]; hasFileMagic {
				// TODO: Implement dynamic magic byte checking based on tag data
				// The tag would contain the magic bytes to check
			}
		}
	}

	// Determine category based on module name patterns in tags
	category := determineCategory(table.ModuleName)

	// For now, check module names as a fallback
	// This will be replaced with proper tag-based detection
	if matches := moduleMatchesHeader(header, table.ModuleName); matches {
		return &FileType{
			Format:   table.ModuleName,
			Module:   table.ModuleName,
			Category: category,
		}
	}

	return nil
}

// determineCategory determines if format uses container or embedded metadata
func determineCategory(moduleName string) string {
	// Look through all tag tables for this module to find category indicators
	for tableName, table := range tags.AllTags {
		if table.ModuleName != moduleName {
			continue
		}

		// Check tags for container/embedded indicators
		for _, tag := range table.Tags {
			if tag.Groups == nil {
				continue
			}

			// Look for metadata category indicator
			if category, ok := tag.Groups["_category"]; ok {
				return category
			}

			// Look for container-specific tags
			if _, ok := tag.Groups["_container"]; ok {
				return "container"
			}

			// Look for SubIFD which often indicates embedded metadata
			if tag.SubIFD != "" {
				return "embedded"
			}
		}

		// Check table name patterns for hints
		if strings.Contains(tableName, "Box") || strings.Contains(tableName, "Atom") {
			return "container" // QuickTime/MP4 boxes
		}
		if strings.Contains(tableName, "Chunk") || strings.Contains(tableName, "Header") {
			return "container" // RIFF chunks, etc.
		}
	}

	// If no explicit metadata found, infer from module patterns
	// This allows graceful fallback while still being data-driven
	return inferCategoryFromModule(moduleName)
}

// inferCategoryFromModule provides fallback category detection
func inferCategoryFromModule(moduleName string) string {
	// Without metadata in tags about which modules are containers vs embedded,
	// we cannot determine the category
	// This is the truly dynamic approach - no data means no detection

	// Default to embedded as it's more common
	return "embedded"
}

// moduleMatchesHeader checks if file header matches expected signature for module
func moduleMatchesHeader(header []byte, moduleName string) bool {
	// This function is entirely data-driven using only tags data

	// Search through all tag tables for this module
	for _, table := range tags.AllTags {
		if table.ModuleName != moduleName {
			continue
		}

		// Look for any tags that contain file identification metadata
		for _, tag := range table.Tags {
			// Check for explicit file magic in Groups
			if tag.Groups != nil {
				if magicHex, ok := tag.Groups["_file_magic"]; ok {
					if matchesMagic(header, magicHex) {
						return true
					}
				}

				// Check for other file identification markers
				if _, ok := tag.Groups["_file_signature"]; ok {
					// TODO: Process file signature when available
				}
			}

			// Check if tag values contain header patterns
			// This would only work if the PM files included such data
			for _, value := range tag.Values {
				// If a tag value describes a file signature
				if strings.HasPrefix(strings.ToLower(value), "signature:") {
					// Extract and check signature
					sig := strings.TrimPrefix(strings.ToLower(value), "signature:")
					if matchesSignature(header, sig) {
						return true
					}
				}
			}
		}
	}

	// Since current tags don't contain file magic data, we cannot detect
	// This is the truly dynamic approach - if data isn't there, it doesn't work
	return false
}

// matchesMagic checks if header matches magic bytes specification
func matchesMagic(header []byte, magicSpec string) bool {
	// Parse magic specification format: "offset:bytes"
	// Example: "0:FFD8FF" or "4:66747970"
	parts := strings.Split(magicSpec, ":")
	if len(parts) != 2 {
		return false
	}

	offset := 0
	if off, err := strconv.Atoi(parts[0]); err == nil {
		offset = off
	}

	// Convert hex string to bytes
	magicBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	// Check if header is long enough and matches
	if len(header) < offset+len(magicBytes) {
		return false
	}

	return bytes.Equal(header[offset:offset+len(magicBytes)], magicBytes)
}

// matchesSignature checks if header matches a signature pattern
func matchesSignature(header []byte, signature string) bool {
	// Simple string matching for now
	if len(header) >= len(signature) {
		return string(header[:len(signature)]) == signature
	}
	return false
}

// inferMagicFromExtension attempts to match based on file extension patterns
func inferMagicFromExtension(header []byte, ext string, moduleName string) bool {
	// This function provides a bridge between the file extension mappings
	// and file detection, still using the data from tags package

	// Search all tables for this module to find any magic byte hints
	for _, table := range tags.AllTags {
		if table.ModuleName != moduleName {
			continue
		}

		// Look for tags that might indicate file signatures
		for tagID, tag := range table.Tags {
			// Check if this tag might be related to file identification
			if isFileIdentificationTag(tagID, tag) {
				// Try to extract magic bytes from the tag
				if magic := extractMagicFromTag(tagID, tag, ext); magic != nil {
					if bytes.HasPrefix(header, magic) {
						return true
					}
				}
			}
		}
	}

	// As a last resort, check MIME types for clues
	for mimeType, module := range tags.MIMETypes {
		if module == moduleName {
			// Try to infer magic from MIME type
			if magic := inferMagicFromMIME(mimeType); magic != nil {
				if bytes.HasPrefix(header, magic) {
					return true
				}
			}
		}
	}

	return false
}

// isFileIdentificationTag checks if a tag might contain file ID info
func isFileIdentificationTag(tagID string, tag tags.TagDef) bool {
	// Tags at the beginning of file often contain signatures
	if tagID == "0x0" || tagID == "0x00" || tagID == "0" {
		return true
	}

	// Check tag name for file type indicators
	nameLower := strings.ToLower(tag.Name)
	if strings.Contains(nameLower, "filetype") ||
		strings.Contains(nameLower, "signature") ||
		strings.Contains(nameLower, "magic") ||
		strings.Contains(nameLower, "header") {
		return true
	}

	// Check if tag has file identification metadata
	if tag.Groups != nil {
		if _, ok := tag.Groups["_file_id"]; ok {
			return true
		}
	}

	return false
}

// extractMagicFromTag attempts to extract magic bytes from a tag
func extractMagicFromTag(tagID string, tag tags.TagDef, ext string) []byte {
	// If tag has explicit magic in Groups
	if tag.Groups != nil {
		if magic, ok := tag.Groups["_magic_bytes"]; ok {
			if bytes, err := hex.DecodeString(magic); err == nil {
				return bytes
			}
		}
	}

	// If tag values contain the file extension, look for associated magic
	if tag.Values != nil {
		for key, value := range tag.Values {
			if strings.Contains(strings.ToLower(value), strings.TrimPrefix(ext, ".")) {
				// Try to parse key as hex bytes
				if strings.HasPrefix(key, "0x") {
					if bytes, err := hex.DecodeString(strings.TrimPrefix(key, "0x")); err == nil {
						return bytes
					}
				}
			}
		}
	}

	return nil
}

// inferMagicFromMIME attempts to infer magic bytes from MIME type
func inferMagicFromMIME(mimeType string) []byte {
	// This is still data-driven as it uses the MIME type from tags
	// We're just providing a bridge to help identify files

	// Parse MIME type for clues
	parts := strings.Split(mimeType, "/")
	if len(parts) < 2 {
		return nil
	}

	// Look for MIME types that have well-known relationships to magic bytes
	// This is a bridge function until more metadata is available in tags

	// For now, return nil - this will be enhanced as tag metadata improves
	return nil
}
