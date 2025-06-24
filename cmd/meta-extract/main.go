// File: cmd/meta-extract/main.go

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"greg-hacke/go-metadata/tags"
)

// MetadataOutput represents the JSON output structure
type MetadataOutput struct {
	File     string          `json:"file"`
	Format   string          `json:"format"`
	Size     int64           `json:"size"`
	Metadata []MetadataField `json:"metadata"`
}

// MetadataField represents a single metadata field in JSON
type MetadataField struct {
	Table       string      `json:"table"`
	TagID       string      `json:"tag_id"`
	Name        string      `json:"name"`
	Value       interface{} `json:"value"`
	Description string      `json:"description,omitempty"`
}

func main() {
	// Parse command line flags
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <file>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Extract and display metadata from files as JSON\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	pretty := flag.Bool("pretty", false, "Pretty print JSON output")
	verbose := flag.Bool("v", false, "Verbose output for debugging")
	listTables := flag.Bool("list-tables", false, "List all available tag tables")
	flag.Parse()

	// If listing tables, show them and exit
	if *listTables {
		fmt.Println("Available tag tables:")
		tableNames := make([]string, 0, len(tags.AllTags))
		for name := range tags.AllTags {
			tableNames = append(tableNames, name)
		}
		sort.Strings(tableNames)

		for _, name := range tableNames {
			table := tags.AllTags[name]
			fmt.Printf("  %-40s (%d tags)\n", name, len(table.Tags))
		}

		// Show some common EXIF and IPTC tags
		fmt.Println("\nSample EXIF tags in Exif::Main (if available):")
		if exifMain, ok := tags.AllTags["Exif::Main"]; ok {
			count := 0
			// Show some common tags
			commonTags := []string{"0x10E", "0x10F", "0x110", "0x112", "0x131", "0x132", "0x13B"}
			for _, id := range commonTags {
				if tag, found := exifMain.Tags[id]; found {
					fmt.Printf("  %s: %s\n", id, tag.Name)
					count++
				}
			}
			if count == 0 {
				// Show first 5 tags
				for id, tag := range exifMain.Tags {
					if count < 5 {
						fmt.Printf("  %s: %s\n", id, tag.Name)
						count++
					}
				}
			}
		}

		fmt.Println("\nSample IPTC tags in IPTC::ApplicationRecord (if available):")
		if iptcApp, ok := tags.AllTags["IPTC::ApplicationRecord"]; ok {
			// Show all tags since there are only 3
			for id, tag := range iptcApp.Tags {
				fmt.Printf("  %s: %s\n", id, tag.Name)
			}
			// Also check for common IPTC tags
			fmt.Println("\nChecking for common IPTC tags:")
			commonIPTC := []string{"2:05", "2:25", "2:80", "2:116", "2:120", "2:5", "2:55", "2:90"}
			for _, id := range commonIPTC {
				if tag, found := iptcApp.Tags[id]; found {
					fmt.Printf("  Found %s: %s\n", id, tag.Name)
				} else {
					fmt.Printf("  Missing %s\n", id)
				}
			}
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
		outputError(err, "Error opening file")
	}
	defer file.Close()

	// Get file info
	stat, err := file.Stat()
	if err != nil {
		outputError(err, "Error getting file info")
	}

	// Read entire file into memory
	data := make([]byte, stat.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		outputError(err, "Error reading file")
	}

	// Identify format from extension
	ext := strings.ToLower(filepath.Ext(filePath))
	formatName, ok := tags.FileExtensions[ext]
	if !ok {
		outputError(fmt.Errorf("unknown file extension: %s", ext), "Format error")
	}

	// Extract metadata
	fields := extractMetadataFromData(data, formatName, *verbose)
	if fields == nil {
		fields = []MetadataField{} // Ensure it's an empty array, not null
	}

	// Create output structure
	output := MetadataOutput{
		File:     filePath,
		Format:   formatName,
		Size:     stat.Size(),
		Metadata: fields,
	}

	// Output JSON
	var jsonData []byte
	if *pretty {
		jsonData, err = json.MarshalIndent(output, "", "  ")
	} else {
		jsonData, err = json.Marshal(output)
	}

	if err != nil {
		outputError(err, "Error encoding JSON")
	}

	fmt.Println(string(jsonData))
}

func outputError(err error, message string) {
	errorOutput := struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{
		Error:   message,
		Message: err.Error(),
	}

	jsonData, _ := json.Marshal(errorOutput)
	fmt.Fprintln(os.Stderr, string(jsonData))
	os.Exit(1)
}

// Add this near the top after other function declarations
func listTableTags(tableName string, table *tags.TagTable) {
	fmt.Fprintf(os.Stderr, "Table %s has %d tags:\n", tableName, len(table.Tags))
	count := 0
	for tagID, tagDef := range table.Tags {
		fmt.Fprintf(os.Stderr, "  %s: %s\n", tagID, tagDef.Name)
		count++
		if count >= 10 {
			fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(table.Tags)-10)
			break
		}
	}
}

// extractMetadataFromData dynamically extracts metadata using tag definitions
func extractMetadataFromData(data []byte, formatName string, verbose bool) []MetadataField {
	var fields []MetadataField

	// Find relevant tag tables
	relevantTables := findRelevantTables(formatName)

	if verbose {
		fmt.Fprintf(os.Stderr, "Found %d relevant tables for format %s\n", len(relevantTables), formatName)
		for _, t := range relevantTables {
			fmt.Fprintf(os.Stderr, "  - %s\n", t)
		}
	}

	// For each relevant table, try to extract tags
	for _, tableName := range relevantTables {
		table := tags.AllTags[tableName]
		if table == nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: table %s not found in AllTags\n", tableName)
			}
			continue
		}

		// Try different extraction methods based on common patterns
		lowerTableName := strings.ToLower(tableName)

		// For JPEG format, try TIFF extraction on all tables
		if formatName == "JPEG" {
			if verbose {
				fmt.Fprintf(os.Stderr, "Trying extraction for %s\n", tableName)
			}
			if extractedFields := tryExtractTIFFStyle(data, tableName, table, verbose); len(extractedFields) > 0 {
				if verbose {
					fmt.Fprintf(os.Stderr, "Found %d fields in %s\n", len(extractedFields), tableName)
				}
				fields = append(fields, extractedFields...)
			}
		} else {
			// For other formats, use specific extraction methods
			switch {
			case strings.Contains(lowerTableName, "png"):
				if pngFields := tryExtractPNGStyle(data, tableName, table); len(pngFields) > 0 {
					fields = append(fields, pngFields...)
				}
			default:
				if verbose {
					fmt.Fprintf(os.Stderr, "Trying binary search for %s\n", tableName)
				}
				if binFields := tryExtractBinarySearch(data, tableName, table); len(binFields) > 0 {
					fields = append(fields, binFields...)
				}
			}
		}
	}

	return fields
}

// findRelevantTables finds tag tables that might apply to this format
func findRelevantTables(formatName string) []string {
	var tables []string

	// Always include specific well-known tables for JPEG
	if formatName == "JPEG" {
		// Add all EXIF-related tables
		wellKnownTables := []string{
			"Exif::Main", "EXIF::Main", "exif::Main",
			"Exif::IFD0", "EXIF::IFD0", "exif::IFD0",
			"Exif::SubIFD", "EXIF::SubIFD", "exif::SubIFD",
			"Exif::GPS", "EXIF::GPS", "exif::GPS",
			"IPTC::ApplicationRecord", "iptc::ApplicationRecord",
			"IPTC::EnvelopeRecord", "iptc::EnvelopeRecord",
			"XMP::dc", "xmp::dc",
			"XMP::xmp", "xmp::xmp",
		}

		for _, tableName := range wellKnownTables {
			if _, ok := tags.AllTags[tableName]; ok {
				tables = append(tables, tableName)
			}
		}
	}

	// Then add all other matching tables
	for tableName := range tags.AllTags {
		// Skip if already added
		found := false
		for _, t := range tables {
			if t == tableName {
				found = true
				break
			}
		}
		if found {
			continue
		}

		if strings.HasPrefix(tableName, formatName+"::") {
			tables = append(tables, tableName)
			continue
		}

		// Common associations
		switch formatName {
		case "JPEG":
			lowerTableName := strings.ToLower(tableName)
			if strings.Contains(lowerTableName, "exif") ||
				strings.Contains(lowerTableName, "iptc") ||
				strings.Contains(lowerTableName, "xmp") ||
				strings.Contains(lowerTableName, "jfif") ||
				strings.Contains(lowerTableName, "ifd") ||
				strings.Contains(lowerTableName, "gps") {
				tables = append(tables, tableName)
			}
		case "PNG":
			if strings.Contains(strings.ToLower(tableName), "png") {
				tables = append(tables, tableName)
			}
		case "MP3", "ID3":
			if strings.Contains(strings.ToLower(tableName), "id3") {
				tables = append(tables, tableName)
			}
		}
	}

	return tables
}

// tryExtractTIFFStyle attempts TIFF/EXIF style extraction
func tryExtractTIFFStyle(data []byte, tableName string, table *tags.TagTable, verbose bool) []MetadataField {
	var fields []MetadataField
	lowerTableName := strings.ToLower(tableName)

	// For JPEG files, look for APP segments
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
		// This is a JPEG file
		if verbose {
			fmt.Fprintf(os.Stderr, "Detected JPEG file, looking for APP segments\n")
		}
		offset := 2
		for offset < len(data)-4 {
			if data[offset] != 0xFF {
				break
			}

			marker := data[offset+1]
			if marker == 0xDA {
				// Start of scan - no more headers
				break
			}

			// Read segment length
			if offset+4 > len(data) {
				break
			}
			segmentLength := int(data[offset+2])<<8 | int(data[offset+3])

			// Check for APP1 (EXIF or XMP)
			if marker == 0xE1 && offset+10 <= len(data) {
				// Check for "Exif\x00\x00"
				if string(data[offset+4:offset+10]) == "Exif\x00\x00" {
					if verbose {
						fmt.Fprintf(os.Stderr, "Found EXIF APP1 segment at offset %d\n", offset)
					}
					// EXIF data starts after "Exif\x00\x00"
					exifData := data[offset+10:]
					// Only process EXIF data with non-IPTC tables
					if !strings.Contains(strings.ToLower(tableName), "iptc") {
						if exifFields := extractTIFFData(exifData, tableName, table, verbose); len(exifFields) > 0 {
							fields = append(fields, exifFields...)
						}
					}
				} else if offset+35 <= len(data) && string(data[offset+4:offset+33]) == "http://ns.adobe.com/xap/1.0/\x00" {
					// XMP data
					if verbose {
						fmt.Fprintf(os.Stderr, "Found XMP APP1 segment at offset %d\n", offset)
					}
					if strings.Contains(lowerTableName, "xmp") {
						// For now, just note we found it - XMP parsing would be added here
						if verbose {
							fmt.Fprintf(os.Stderr, "XMP parsing not yet implemented for table %s\n", tableName)
						}
					}
				}
			}

			// Check for APP13 (IPTC)
			if marker == 0xED && offset+18 <= len(data) && segmentLength >= 14 {
				// Check for "Photoshop 3.0\x00"
				if string(data[offset+4:offset+18]) == "Photoshop 3.0\x00" {
					if verbose {
						fmt.Fprintf(os.Stderr, "Found IPTC APP13 segment at offset %d, trying with table %s\n", offset, tableName)
					}
					// Parse IPTC data - only try on IPTC tables
					if strings.Contains(strings.ToLower(tableName), "iptc") {
						iptcData := data[offset+18 : offset+2+segmentLength]
						if iptcFields := extractIPTCData(iptcData, tableName, table, verbose); len(iptcFields) > 0 {
							fields = append(fields, iptcFields...)
						}
					}
				}
			}

			// Move to next segment
			offset += 2 + segmentLength
		}
	} else {
		// Not a JPEG, try direct TIFF extraction
		if exifFields := extractTIFFData(data, tableName, table, verbose); len(exifFields) > 0 {
			fields = append(fields, exifFields...)
		}
	}

	return fields
}

// extractIPTCData extracts IPTC metadata
func extractIPTCData(data []byte, tableName string, table *tags.TagTable, verbose bool) []MetadataField {
	var fields []MetadataField
	offset := 0

	if verbose {
		fmt.Fprintf(os.Stderr, "extractIPTCData called for table %s, data size %d\n", tableName, len(data))
	}

	// Keep track of repeating tags (like keywords)
	tagValues := make(map[string][]string)

	// Parse Photoshop Image Resources
	for offset < len(data)-12 {
		// Look for 8BIM marker
		if offset+12 > len(data) || string(data[offset:offset+4]) != "8BIM" {
			offset++
			continue
		}

		// Resource ID
		resourceID := int(data[offset+4])<<8 | int(data[offset+5])

		// Skip name (pascal string)
		nameLen := int(data[offset+6])
		offset += 7 + nameLen
		if nameLen%2 == 0 {
			offset++ // Padding
		}

		// Resource data size
		if offset+4 > len(data) {
			break
		}
		dataSize := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		offset += 4

		// IPTC data is in resource 0x0404
		if resourceID == 0x0404 && offset+dataSize <= len(data) {
			if verbose {
				fmt.Fprintf(os.Stderr, "Found IPTC resource 0x0404, size %d\n", dataSize)
			}

			// Parse IPTC records
			iptcOffset := offset
			for iptcOffset < offset+dataSize-5 {
				// IPTC record marker
				if data[iptcOffset] != 0x1C {
					iptcOffset++
					continue
				}

				record := int(data[iptcOffset+1])
				dataSet := int(data[iptcOffset+2])
				recordSize := int(data[iptcOffset+3])<<8 | int(data[iptcOffset+4])

				if iptcOffset+5+recordSize > offset+dataSize {
					break
				}

				// Create tag key like "2:25" for record 2, dataset 25
				tagKey := fmt.Sprintf("%d:%d", record, dataSet)

				// Also try with leading zeros for single digits
				var altKey string
				if dataSet < 10 {
					altKey = fmt.Sprintf("%d:0%d", record, dataSet)
				}

				// Try different formats
				tagDef, found := table.Tags[tagKey]
				if !found && altKey != "" {
					tagDef, found = table.Tags[altKey]
				}
				if !found {
					// Try hex format
					tagKeyHex := fmt.Sprintf("0x%02X%02X", record, dataSet)
					tagDef, found = table.Tags[tagKeyHex]
				}

				if found {
					value := string(data[iptcOffset+5 : iptcOffset+5+recordSize])

					if verbose {
						fmt.Fprintf(os.Stderr, "Found IPTC tag %d:%d (%s): %s\n", record, dataSet, tagDef.Name, value)
					}

					// Collect repeating tags
					fieldKey := fmt.Sprintf("%s:%d:%d", tableName, record, dataSet)
					tagValues[fieldKey] = append(tagValues[fieldKey], value)
				} else if verbose {
					fmt.Fprintf(os.Stderr, "IPTC tag %d:%d not found in table %s (tried keys: %s, %s)\n", record, dataSet, tableName, tagKey, altKey)
					// Debug: show what tags ARE in the table
					if len(table.Tags) < 10 {
						fmt.Fprintf(os.Stderr, "  Available tags in %s: ", tableName)
						for k := range table.Tags {
							fmt.Fprintf(os.Stderr, "%s ", k)
						}
						fmt.Fprintf(os.Stderr, "\n")
					}
				}

				iptcOffset += 5 + recordSize
			}
		}

		offset += dataSize
		if dataSize%2 == 1 {
			offset++ // Padding
		}
	}

	// Convert collected values to fields
	for fieldKey, values := range tagValues {
		parts := strings.Split(fieldKey, ":")
		if len(parts) >= 3 {
			record, _ := strconv.Atoi(parts[len(parts)-2])
			dataset, _ := strconv.Atoi(parts[len(parts)-1])
			tagKey := fmt.Sprintf("%d:%d", record, dataset)

			tagDef, found := table.Tags[tagKey]
			if !found && dataset < 10 {
				// Try with leading zero
				tagKey = fmt.Sprintf("%d:0%d", record, dataset)
				tagDef, found = table.Tags[tagKey]
			}

			if found {
				var fieldValue interface{}
				if len(values) == 1 {
					fieldValue = values[0]
				} else {
					fieldValue = values
				}

				field := MetadataField{
					Table:       tableName,
					TagID:       fmt.Sprintf("%d:%d", record, dataset),
					Name:        tagDef.Name,
					Value:       fieldValue,
					Description: tagDef.Description,
				}

				if field.Name == "" {
					field.Name = fmt.Sprintf("IPTC:%d:%d", record, dataset)
				}

				fields = append(fields, field)
			}
		}
	}

	return fields
}

// extractTIFFData extracts metadata from TIFF-formatted data
func extractTIFFData(data []byte, tableName string, table *tags.TagTable, verbose bool) []MetadataField {
	var fields []MetadataField

	if len(data) < 8 {
		return fields
	}

	var byteOrder binary.ByteOrder
	if data[0] == 'I' && data[1] == 'I' &&
		data[2] == 0x2A && data[3] == 0x00 {
		byteOrder = binary.LittleEndian
		if verbose {
			fmt.Fprintf(os.Stderr, "Found little-endian TIFF header\n")
		}
	} else if data[0] == 'M' && data[1] == 'M' &&
		data[2] == 0x00 && data[3] == 0x2A {
		byteOrder = binary.BigEndian
		if verbose {
			fmt.Fprintf(os.Stderr, "Found big-endian TIFF header\n")
		}
	} else {
		return fields
	}

	// Found TIFF header, parse IFD
	ifdOffset := byteOrder.Uint32(data[4:8])
	if verbose {
		fmt.Fprintf(os.Stderr, "IFD offset: %d\n", ifdOffset)
	}
	if ifdFields := parseIFDGeneric(data, ifdOffset, byteOrder, tableName, table, verbose); len(ifdFields) > 0 {
		fields = append(fields, ifdFields...)
	}

	return fields
}

// parseIFDGeneric parses an IFD using any tag table
func parseIFDGeneric(data []byte, ifdOffset uint32, byteOrder binary.ByteOrder, tableName string, table *tags.TagTable, verbose bool) []MetadataField {
	var fields []MetadataField

	if int(ifdOffset) >= len(data)-2 {
		return fields
	}

	entryCount := byteOrder.Uint16(data[ifdOffset : ifdOffset+2])
	if entryCount > 1000 {
		return fields
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "IFD has %d entries\n", entryCount)
	}

	offset := ifdOffset + 2

	for i := 0; i < int(entryCount); i++ {
		if int(offset+12) > len(data) {
			break
		}

		tagID := byteOrder.Uint16(data[offset : offset+2])
		format := byteOrder.Uint16(data[offset+2 : offset+4])
		count := byteOrder.Uint32(data[offset+4 : offset+8])
		valueOffset := data[offset+8 : offset+12]

		// Look up tag
		tagKey := fmt.Sprintf("0x%X", tagID)
		tagDef, found := table.Tags[tagKey]
		if !found {
			tagKey = fmt.Sprintf("%d", tagID)
			tagDef, found = table.Tags[tagKey]
		}

		if verbose && !found {
			fmt.Fprintf(os.Stderr, "Tag %s (0x%X) not found in table %s\n", tagKey, tagID, tableName)
		}

		if found {
			value := extractValueGeneric(data, valueOffset, format, count, byteOrder)

			if verbose {
				fmt.Fprintf(os.Stderr, "Found tag %s: %v\n", tagDef.Name, value)
			}

			field := MetadataField{
				Table:       tableName,
				TagID:       tagKey,
				Name:        tagDef.Name,
				Value:       value,
				Description: tagDef.Description,
			}

			if field.Name == "" {
				field.Name = tagKey
			}

			// Apply value mappings
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

	var valueData []byte
	if totalSize <= 4 {
		valueData = valueBytes[:totalSize]
	} else {
		offset := byteOrder.Uint32(valueBytes)
		if int(offset+totalSize) <= len(data) {
			valueData = data[offset : offset+totalSize]
		} else {
			return "Invalid offset"
		}
	}

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
		vals := make([]uint16, 0, count)
		for i := uint32(0); i < count && int(i*2+2) <= len(valueData); i++ {
			vals = append(vals, byteOrder.Uint16(valueData[i*2:]))
		}
		if len(vals) == 1 {
			return vals[0]
		}
		return vals

	case 4: // LONG
		if count == 1 && len(valueData) >= 4 {
			return byteOrder.Uint32(valueData)
		}
		vals := make([]uint32, 0, count)
		for i := uint32(0); i < count && int(i*4+4) <= len(valueData); i++ {
			vals = append(vals, byteOrder.Uint32(valueData[i*4:]))
		}
		if len(vals) == 1 {
			return vals[0]
		}
		return vals

	case 5: // RATIONAL
		if count == 1 && len(valueData) >= 8 {
			num := byteOrder.Uint32(valueData[0:4])
			den := byteOrder.Uint32(valueData[4:8])
			if den == 0 {
				return 0.0
			}
			return float64(num) / float64(den)
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
func tryExtractPNGStyle(data []byte, tableName string, table *tags.TagTable) []MetadataField {
	var fields []MetadataField

	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if len(data) < 8 || !bytes.Equal(data[:8], pngSig) {
		return fields
	}

	offset := 8
	for offset < len(data)-12 {
		length := binary.BigEndian.Uint32(data[offset : offset+4])
		if length > uint32(len(data)) {
			break
		}

		chunkType := string(data[offset+4 : offset+8])

		if tagDef, found := table.Tags[chunkType]; found {
			if offset+8+int(length) <= len(data) {
				chunkData := data[offset+8 : offset+8+int(length)]

				field := MetadataField{
					Table:       tableName,
					TagID:       chunkType,
					Name:        tagDef.Name,
					Value:       fmt.Sprintf("%d bytes", len(chunkData)),
					Description: tagDef.Description,
				}

				if field.Name == "" {
					field.Name = chunkType
				}

				// For text chunks, extract text
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

		offset += 8 + int(length) + 4

		if chunkType == "IEND" {
			break
		}
	}

	return fields
}

// tryExtractBinarySearch attempts generic binary pattern matching
func tryExtractBinarySearch(data []byte, tableName string, table *tags.TagTable) []MetadataField {
	var fields []MetadataField
	// Simplified for now - could be enhanced
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
