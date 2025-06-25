package meta

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"greg-hacke/go-metadata/tags"
)

// MetadataRequest represents requested metadata fields
type MetadataRequest map[string]bool

// FileType represents the identified file format
type FileType struct {
	Format      string // Primary format (e.g., "JPEG", "TIFF")
	Module      string // Module name from ExifTool
	Description string // Format description
}

// ProcessFileByPath processes a file from its path
func ProcessFileByPath(filePath string, requested MetadataRequest) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	return processFileRawWithPath(file, filePath, requested)
}

// ProcessFileRaw processes an already opened file
func ProcessFileRaw(file io.ReadSeeker, requested MetadataRequest) error {
	return processFileRawWithPath(file, "", requested)
}

// processFileRawWithPath processes a file with optional path for extension detection
func processFileRawWithPath(file io.ReadSeeker, filePath string, requested MetadataRequest) error {
	// Identify the file type
	fileType, err := identifyFileWithPath(file, filePath)
	if err != nil {
		return fmt.Errorf("cannot identify file: %w", err)
	}

	// Reset to beginning for processing
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("cannot seek to start: %w", err)
	}

	// Log identified file information
	fmt.Printf("Format:        %s\n", fileType.Format)
	fmt.Printf("Module:        %s\n", fileType.Module)
	fmt.Printf("Description:   %s\n", fileType.Description)

	// Find appropriate tag tables for this file type
	tagTables := findTagTablesForFileType(fileType)
	fmt.Printf("Tag Tables:    %d found\n", len(tagTables))

	// Extract metadata based on file type
	fmt.Println("\n=== Extracting Metadata ===")
	metadata, err := CaptureMetadata(file, fileType, tagTables, requested)
	if err != nil {
		return fmt.Errorf("metadata extraction error: %w", err)
	}

	// Output as JSON
	jsonOutput, err := metadata.ToJSON()
	if err != nil {
		return fmt.Errorf("JSON conversion error: %w", err)
	}

	fmt.Println("\n=== Metadata (JSON) ===")
	fmt.Println(jsonOutput)

	return nil
}

// findTagTablesForFileType finds the appropriate tag tables for a file type
func findTagTablesForFileType(fileType *FileType) []*tags.TagTable {
	var tables []*tags.TagTable

	// Collect all potentially relevant tables
	for _, table := range tags.AllTags {
		// Skip tables without a module name
		if table.ModuleName == "" {
			continue
		}

		// For now, collect ALL tables and let CaptureMetadata decide which ones to use
		// based on what's actually present in the file
		tables = append(tables, table)
	}

	fmt.Printf("Found %d total tag tables available\n", len(tables))

	return tables
}

// identifyFileWithPath determines file type using ExifTool data
func identifyFileWithPath(file io.ReadSeeker, filePath string) (*FileType, error) {
	// Read header for magic byte detection
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

	// Debug: show first 16 bytes
	fmt.Printf("Header bytes: %s\n", hex.EncodeToString(header[:16]))

	// Try magic byte detection first (in TestOrder)
	// This ensures we check in the priority order ExifTool uses
	for _, fileType := range tags.ExifToolFileTypes.TestOrder {
		pattern, hasPattern := tags.ExifToolFileTypes.MagicNumbers[fileType]
		if !hasPattern {
			continue
		}

		matched, err := matchPerlPattern(header, pattern)
		if err != nil {
			continue
		}

		if matched {
			fmt.Printf("Magic match: %s (pattern: %s)\n", fileType, pattern)

			// For formats that have many variants (like TIFF-based RAW files),
			// check if we can get more specific info from the extension
			if shouldUseExtensionForSpecificity(fileType, filePath) {
				ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filePath), "."))
				if extInfo, ok := tags.ExifToolFileTypes.Extensions[ext]; ok {
					// Only use extension if it maps to the same base type
					if extInfo.Type == fileType || resolveBaseType(extInfo.Type) == fileType {
						fmt.Printf("Using extension for specific format: %s\n", ext)
						return resolveFileType(extInfo.Type, ext)
					}
				}
			}

			return resolveFileType(fileType, "")
		}
	}

	// Fall back to extension if available
	if filePath != "" {
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filePath), "."))
		fmt.Printf("Extension fallback: %s\n", ext)
		if extInfo, ok := tags.ExifToolFileTypes.Extensions[ext]; ok {
			return resolveFileType(extInfo.Type, ext)
		}
	}

	return &FileType{Format: "UNKNOWN", Module: "", Description: "Unknown format"}, nil
}

// shouldUseExtensionForSpecificity checks if we should prefer extension for certain base types
func shouldUseExtensionForSpecificity(fileType string, filePath string) bool {
	if filePath == "" {
		return false
	}

	// Dynamically determine if this fileType has multiple variants
	// by counting how many extensions map to this type
	variantCount := 0
	for _, extInfo := range tags.ExifToolFileTypes.Extensions {
		if extInfo.Type == fileType {
			variantCount++
			if variantCount > 3 { // If more than 3 extensions use this type, it's likely a container
				return true
			}
		}
	}

	return false
}

// resolveBaseType follows type aliases to find the base type
func resolveBaseType(fileType string) string {
	seen := make(map[string]bool)
	current := fileType

	for {
		if seen[current] {
			break
		}
		seen[current] = true

		if extInfo, ok := tags.ExifToolFileTypes.Extensions[current]; ok {
			if extInfo.Type != current && extInfo.Description == "" {
				current = extInfo.Type
				continue
			}
		}
		break
	}

	return current
}

// resolveFileType resolves a file type with proper description tracking
func resolveFileType(fileType string, originalExt string) (*FileType, error) {
	// Keep track of the description from the original extension
	var description string
	var preferredModule string

	if originalExt != "" {
		if extInfo, ok := tags.ExifToolFileTypes.Extensions[originalExt]; ok {
			description = extInfo.Description
		}

		// Check if there's a specific module for this extension
		if module, ok := tags.ExifToolFileTypes.ModuleNames[originalExt]; ok && module != "" && module != "0" {
			preferredModule = module
		}
	}

	// Resolve type aliases
	resolvedType := resolveBaseType(fileType)

	// Get module name - prefer the original file type's module first
	module := ""
	if m, ok := tags.ExifToolFileTypes.ModuleNames[fileType]; ok && m != "" && m != "0" {
		module = m
	} else if preferredModule != "" {
		module = preferredModule
	} else if m, ok := tags.ExifToolFileTypes.ModuleNames[resolvedType]; ok {
		module = m
		if module == "0" || module == "" {
			module = resolvedType
		}
	} else {
		module = resolvedType
	}

	// If still no description, find one for the resolved type
	if description == "" {
		// Look for the canonical entry
		if extInfo, ok := tags.ExifToolFileTypes.Extensions[resolvedType]; ok {
			description = extInfo.Description
		}
	}

	return &FileType{
		Format:      resolvedType,
		Module:      module,
		Description: description,
	}, nil
}

// matchPerlPattern matches data against a Perl-style regex pattern
func matchPerlPattern(data []byte, pattern string) (bool, error) {
	// Skip non-pattern entries
	if pattern == "RawConv" {
		return false, nil
	}

	// Try to parse as a simple byte pattern first
	if simpleMatch, matched := trySimpleByteMatch(data, pattern); matched {
		return simpleMatch, nil
	}

	// For complex patterns, we need to be more careful about regex matching
	// Many patterns should only match at the start of the file

	// Convert pattern to Go regex
	goPattern, err := convertPerlToGoRegex(pattern)
	if err != nil {
		return false, err
	}

	re, err := regexp.Compile(goPattern)
	if err != nil {
		return false, nil // Silently fail on bad regex
	}

	// For patterns with alternatives separated by |, we need to check each part
	// Some parts might be too generic (like \s*< in PLIST pattern)
	if strings.Contains(pattern, "|") {
		// Split alternatives and check each one more carefully
		// But for now, just ensure we match at position 0
		loc := re.FindIndex(data)
		return loc != nil && loc[0] == 0, nil
	}

	// For patterns with optional prefix like (....)?, check both positions
	if strings.Contains(pattern, ")?") {
		// These patterns can match at offset 0 or after the optional part
		matches := re.FindAllIndex(data, -1)
		for _, match := range matches {
			// Only accept matches at expected positions
			if match[0] == 0 || (strings.HasPrefix(pattern, "(....)") && match[0] == 4) {
				return true, nil
			}
		}
		return false, nil
	}

	// Standard match - must be at start
	loc := re.FindIndex(data)
	return loc != nil && loc[0] == 0, nil
}

// trySimpleByteMatch attempts to match simple byte patterns without regex
func trySimpleByteMatch(data []byte, pattern string) (matches bool, handled bool) {
	// Check for offset patterns like ".{4}\x57\x90\x75\x36"
	if strings.HasPrefix(pattern, ".{") {
		endIdx := strings.Index(pattern, "}")
		if endIdx > 2 {
			offsetStr := pattern[2:endIdx]
			offset, err := strconv.Atoi(offsetStr)
			if err == nil && endIdx+1 < len(pattern) {
				// Parse the bytes after the offset
				remaining := pattern[endIdx+1:]
				if matchBytes, ok := parseSimpleBytes(remaining); ok {
					if len(data) >= offset+len(matchBytes) {
						return bytes.Equal(data[offset:offset+len(matchBytes)], matchBytes), true
					}
					return false, true
				}
			}
		}
	}

	// Try to parse as simple byte sequence
	if matchBytes, ok := parseSimpleBytes(pattern); ok {
		return bytes.HasPrefix(data, matchBytes), true
	}

	return false, false
}

// parseSimpleBytes tries to parse a pattern as a simple byte sequence
func parseSimpleBytes(pattern string) ([]byte, bool) {
	// Quick check for regex metacharacters
	if strings.ContainsAny(pattern, "()[]{}|?*+^$.") {
		return nil, false
	}

	result := []byte{}
	i := 0

	for i < len(pattern) {
		if i+1 < len(pattern) && pattern[i] == '\\' {
			switch pattern[i+1] {
			case 'x':
				if i+3 < len(pattern) {
					if b, err := strconv.ParseUint(pattern[i+2:i+4], 16, 8); err == nil {
						result = append(result, byte(b))
						i += 4
						continue
					}
				}
				return nil, false
			case '0':
				result = append(result, 0)
				i += 2
				continue
			case 'r':
				result = append(result, '\r')
				i += 2
				continue
			case 'n':
				result = append(result, '\n')
				i += 2
				continue
			case 's', 'S', 'd', 'D', 'w', 'W':
				// These are regex constructs
				return nil, false
			default:
				// Escaped character
				result = append(result, pattern[i+1])
				i += 2
				continue
			}
		} else {
			result = append(result, pattern[i])
			i++
		}
	}

	return result, true
}

// convertPerlToGoRegex converts a Perl regex pattern to Go regex
func convertPerlToGoRegex(pattern string) (string, error) {
	// First pass: convert hex escapes and special sequences
	result := ""
	i := 0

	for i < len(pattern) {
		if i+3 < len(pattern) && pattern[i] == '\\' && pattern[i+1] == 'x' {
			// Convert \xHH to Go format
			if b, err := strconv.ParseUint(pattern[i+2:i+4], 16, 8); err == nil {
				// Use hex format in Go regex
				result += fmt.Sprintf("\\x%02x", b)
				i += 4
				continue
			}
		}

		if i+1 < len(pattern) && pattern[i] == '\\' && pattern[i+1] == '0' {
			// Convert \0 to \x00
			result += "\\x00"
			i += 2
			continue
		}

		// Handle character classes that need conversion
		if i+1 < len(pattern) && pattern[i] == '\\' {
			switch pattern[i+1] {
			case 's':
				result += `\s`
				i += 2
				continue
			case 'S':
				result += `\S`
				i += 2
				continue
			case 'd':
				result += `\d`
				i += 2
				continue
			case 'D':
				result += `\D`
				i += 2
				continue
			}
		}

		// Copy other characters as-is
		result += string(pattern[i])
		i++
	}

	// Don't add anchors - let the match function handle positioning

	return result, nil
}
