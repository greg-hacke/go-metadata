package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"greg-hacke/go-metadata/tags"
)

// Field represents extracted metadata
type Field struct {
	TableName   string
	TagID       string
	Name        string
	Value       interface{}
	Description string
}

func main() {
	// Parse command line flags
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <file>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Extract and display metadata from files\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	verbose := flag.Bool("v", false, "Verbose output")
	showTables := flag.Bool("tables", false, "Show available tag tables")
	flag.Parse()

	// If showing tables, list them and exit
	if *showTables {
		fmt.Println("Available tag tables:")
		for tableName := range tags.AllTags {
			fmt.Printf("  %s\n", tableName)
		}
		return
	}

	// Check arguments
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	filePath := flag.Arg(0)

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Get file info
	stat, err := file.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting file info: %v\n", err)
		os.Exit(1)
	}

	// Read entire file into memory for dynamic parsing
	data := make([]byte, stat.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Identify format from extension
	ext := strings.ToLower(filepath.Ext(filePath))
	formatName, ok := tags.FileExtensions[ext]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown file extension: %s\n", ext)
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("File: %s\n", filePath)
		fmt.Printf("Format: %s (from extension %s)\n", formatName, ext)
		fmt.Printf("Size: %d bytes\n", len(data))
		fmt.Println()
	}

	// Extract metadata dynamically using tag tables
	fields := extractMetadataFromData(data, formatName, *verbose)

	// Display results
	displayFields(fields, *verbose)
}

// extractMetadataFromData dynamically extracts metadata using tag definitions
func extractMetadataFromData(data []byte, formatName string, verbose bool) []Field {
	var fields []Field

	// Try to find relevant tag tables for this format
	relevantTables := findRelevantTables(formatName)

	if verbose {
		fmt.Printf("Found %d relevant tag tables for format %s\n", len(relevantTables), formatName)
		for _, tableName := range relevantTables {
			fmt.Printf("  - %s\n", tableName)
		}
		fmt.Println()
	}

	// For each relevant table, try to extract tags
	for _, tableName := range relevantTables {
		table := tags.AllTags[tableName]
		if table == nil {
			continue
		}

		// Try different extraction methods based on common patterns
		switch {
		case strings.Contains(tableName, "Exif"):
			// Try TIFF/EXIF style extraction
			if exifFields := tryExtractTIFFStyle(data, tableName, table); len(exifFields) > 0 {
				fields = append(fields, exifFields...)
			}
		case strings.Contains(tableName, "PNG"):
			// Try PNG chunk style extraction
			if pngFields := tryExtractPNGStyle(data, tableName, table); len(pngFields) > 0 {
				fields = append(fields, pngFields...)
			}
		default:
			// Try generic binary search
			if binFields := tryExtractBinarySearch(data, tableName, table); len(binFields) > 0 {
				fields = append(fields, binFields...)
			}
		}
	}

	return fields
}

// findRelevantTables finds tag tables that might apply to this format
func findRelevantTables(formatName string) []string {
	var tables []string

	// Look for tables that contain the format name
	for tableName := range tags.AllTags {
		// Direct format match
		if strings.HasPrefix(tableName, formatName+"::") {
			tables = append(tables, tableName)
			continue
		}

		// Common associations
		switch formatName {
		case "JPEG":
			if strings.Contains(tableName, "Exif") ||
				strings.Contains(tableName, "IPTC") ||
				strings.Contains(tableName, "XMP") {
				tables = append(tables, tableName)
			}
		case "PNG":
			if strings.Contains(tableName, "PNG") {
				tables = append(tables, tableName)
			}
		case "MP3", "ID3":
			if strings.Contains(tableName, "ID3") {
				tables = append(tables, tableName)
			}
		}
	}

	return tables
}

// tryExtractTIFFStyle attempts TIFF/EXIF style extraction
func tryExtractTIFFStyle(data []byte, tableName string, table *tags.TagTable) []Field {
	var fields []Field

	// Look for TIFF header patterns
	for offset := 0; offset < len(data)-8; offset++ {
		// Check for TIFF headers
		if offset+8 > len(data) {
			break
		}

		var byteOrder binary.ByteOrder
		if data[offset] == 'I' && data[offset+1] == 'I' &&
			data[offset+2] == 0x2A && data[offset+3] == 0x00 {
			// Little-endian TIFF
			byteOrder = binary.LittleEndian
		} else if data[offset] == 'M' && data[offset+1] == 'M' &&
			data[offset+2] == 0x00 && data[offset+3] == 0x2A {
			// Big-endian TIFF
			byteOrder = binary.BigEndian
		} else {
			continue
		}

		// Found TIFF header, try to parse IFD
		ifdOffset := byteOrder.Uint32(data[offset+4 : offset+8])
		if ifdFields := parseIFDGeneric(data[offset:], ifdOffset, byteOrder, tableName, table); len(ifdFields) > 0 {
			fields = append(fields, ifdFields...)
		}
	}

	return fields
}

// parseIFDGeneric parses an IFD using any tag table
func parseIFDGeneric(data []byte, ifdOffset uint32, byteOrder binary.ByteOrder, tableName string, table *tags.TagTable) []Field {
	var fields []Field

	if int(ifdOffset) >= len(data)-2 {
		return fields
	}

	// Read entry count
	entryCount := byteOrder.Uint16(data[ifdOffset : ifdOffset+2])
	if entryCount > 1000 { // Sanity check
		return fields
	}

	offset := ifdOffset + 2

	// Process each entry
	for i := 0; i < int(entryCount); i++ {
		if int(offset+12) > len(data) {
			break
		}

		// Read IFD entry
		tagID := byteOrder.Uint16(data[offset : offset+2])
		format := byteOrder.Uint16(data[offset+2 : offset+4])
		count := byteOrder.Uint32(data[offset+4 : offset+8])
		valueOffset := data[offset+8 : offset+12]

		// Look up tag in table
		tagKey := fmt.Sprintf("0x%X", tagID)
		tagDef, found := table.Tags[tagKey]
		if !found {
			// Try decimal format
			tagKey = fmt.Sprintf("%d", tagID)
			tagDef, found = table.Tags[tagKey]
		}

		if found {
			// Extract value
			value := extractValueGeneric(data, valueOffset, format, count, byteOrder)

			field := Field{
				TableName:   tableName,
				TagID:       tagKey,
				Name:        tagDef.Name,
				Value:       value,
				Description: tagDef.Description,
			}

			if field.Name == "" {
				field.Name = tagKey
			}

			// Apply value mappings if available
			if len(tagDef.Values) > 0 {
				if mapped := mapValue(value, tagDef.Values); mapped != "" {
					field.Value = mapped
				}
			}

			fields = append(fields, field)
		}

		offset += 12
	}

	return fields
}

// extractValueGeneric extracts a value based on TIFF format codes
func extractValueGeneric(data []byte, valueBytes []byte, format uint16, count uint32, byteOrder binary.ByteOrder) interface{} {
	// Calculate component size
	var componentSize uint32
	switch format {
	case 1, 2, 6, 7: // BYTE, ASCII, SBYTE, UNDEFINED
		componentSize = 1
	case 3, 8: // SHORT, SSHORT
		componentSize = 2
	case 4, 9, 11: // LONG, SLONG, FLOAT
		componentSize = 4
	case 5, 10, 12: // RATIONAL, SRATIONAL, DOUBLE
		componentSize = 8
	default:
		return fmt.Sprintf("Unknown format %d", format)
	}

	totalSize := componentSize * count

	// Get value data
	var valueData []byte
	if totalSize <= 4 {
		valueData = valueBytes[:totalSize]
	} else {
		// Value at offset
		offset := byteOrder.Uint32(valueBytes)
		if int(offset+totalSize) <= len(data) {
			valueData = data[offset : offset+totalSize]
		} else {
			return "Invalid offset"
		}
	}

	// Parse based on format
	switch format {
	case 2: // ASCII
		if len(valueData) > 0 && valueData[len(valueData)-1] == 0 {
			valueData = valueData[:len(valueData)-1]
		}
		return string(valueData)

	case 3: // SHORT
		if count == 1 && len(valueData) >= 2 {
			return byteOrder.Uint16(valueData)
		}
		return fmt.Sprintf("%d values", count)

	case 4: // LONG
		if count == 1 && len(valueData) >= 4 {
			return byteOrder.Uint32(valueData)
		}
		return fmt.Sprintf("%d values", count)

	case 5: // RATIONAL
		if count == 1 && len(valueData) >= 8 {
			num := byteOrder.Uint32(valueData[0:4])
			den := byteOrder.Uint32(valueData[4:8])
			if den == 0 {
				return "0"
			}
			return fmt.Sprintf("%d/%d", num, den)
		}
		return fmt.Sprintf("%d rationals", count)

	default:
		if count == 1 {
			return fmt.Sprintf("0x%X", valueData)
		}
		return fmt.Sprintf("%d bytes", len(valueData))
	}
}

// tryExtractPNGStyle attempts PNG chunk style extraction
func tryExtractPNGStyle(data []byte, tableName string, table *tags.TagTable) []Field {
	var fields []Field

	// Look for PNG signature
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if len(data) < 8 || !bytes.Equal(data[:8], pngSig) {
		return fields
	}

	offset := 8
	for offset < len(data)-12 {
		// Read chunk length
		length := binary.BigEndian.Uint32(data[offset : offset+4])
		if length > uint32(len(data)) {
			break
		}

		// Read chunk type
		chunkType := string(data[offset+4 : offset+8])

		// Check if we have a tag for this chunk type
		if tagDef, found := table.Tags[chunkType]; found {
			// Extract chunk data
			if int(offset+8+length) <= len(data) {
				chunkData := data[offset+8 : offset+8+length]

				field := Field{
					TableName:   tableName,
					TagID:       chunkType,
					Name:        tagDef.Name,
					Value:       fmt.Sprintf("%d bytes", len(chunkData)),
					Description: tagDef.Description,
				}

				if field.Name == "" {
					field.Name = chunkType
				}

				// For text chunks, try to extract text
				if chunkType == "tEXt" || chunkType == "iTXt" || chunkType == "zTXt" {
					if nullPos := bytes.IndexByte(chunkData, 0); nullPos >= 0 {
						keyword := string(chunkData[:nullPos])
						value := string(chunkData[nullPos+1:])
						field.Name = keyword
						field.Value = value
					}
				}

				fields = append(fields, field)
			}
		}

		// Move to next chunk (length + type + data + CRC)
		offset += 8 + int(length) + 4

		// Check for IEND
		if chunkType == "IEND" {
			break
		}
	}

	return fields
}

// tryExtractBinarySearch attempts generic binary pattern matching
func tryExtractBinarySearch(data []byte, tableName string, table *tags.TagTable) []Field {
	var fields []Field

	// For each tag in the table, search for patterns
	for tagID, tagDef := range table.Tags {
		// Skip if no name
		if tagDef.Name == "" {
			continue
		}

		// Try to find tag ID as hex pattern in data
		if strings.HasPrefix(tagID, "0x") {
			// Convert hex ID to search pattern
			// This is a simplified example - real implementation would be more sophisticated
			continue
		}

		// For string tags, search for the tag name
		pattern := []byte(tagDef.Name)
		if idx := bytes.Index(data, pattern); idx >= 0 {
			// Found the pattern, extract some context
			start := idx - 20
			if start < 0 {
				start = 0
			}
			end := idx + len(pattern) + 20
			if end > len(data) {
				end = len(data)
			}

			field := Field{
				TableName:   tableName,
				TagID:       tagID,
				Name:        tagDef.Name,
				Value:       fmt.Sprintf("Found at offset %d", idx),
				Description: tagDef.Description,
			}

			fields = append(fields, field)
		}
	}

	return fields
}

// mapValue applies value mappings
func mapValue(value interface{}, mappings map[string]string) string {
	key := fmt.Sprint(value)
	if mapped, ok := mappings[key]; ok {
		return mapped
	}
	return ""
}

// displayFields displays extracted fields
func displayFields(fields []Field, verbose bool) {
	if len(fields) == 0 {
		fmt.Println("No metadata found")
		return
	}

	// Group by table name
	grouped := make(map[string][]Field)
	for _, field := range fields {
		grouped[field.TableName] = append(grouped[field.TableName], field)
	}

	// Display each group
	first := true
	for tableName, tableFields := range grouped {
		if !first {
			fmt.Println()
		}
		first = false

		fmt.Printf("=== %s ===\n", tableName)

		// Find max name length for alignment
		maxLen := 0
		for _, f := range tableFields {
			if len(f.Name) > maxLen {
				maxLen = len(f.Name)
			}
		}

		// Display fields
		for _, f := range tableFields {
			if verbose {
				fmt.Printf("%-*s [%s] : %v", maxLen, f.Name, f.TagID, f.Value)
				if f.Description != "" {
					fmt.Printf(" (%s)", f.Description)
				}
				fmt.Println()
			} else {
				fmt.Printf("%-*s : %v\n", maxLen, f.Name, f.Value)
			}
		}
	}
}
