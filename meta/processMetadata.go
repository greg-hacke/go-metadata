package meta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"greg-hacke/go-metadata/tags"
)

// MetadataExtractor handles the actual extraction of metadata
type MetadataExtractor struct {
	data          []byte
	metadata      *Metadata
	tagTables     []*tags.TagTable
	loadedModules map[string]bool // Track which modules we've loaded
}

// NewMetadataExtractor creates a new extractor
func NewMetadataExtractor(data []byte, metadata *Metadata, tagTables []*tags.TagTable) *MetadataExtractor {
	// Build map of already loaded modules
	loadedModules := make(map[string]bool)
	for _, table := range tagTables {
		loadedModules[strings.ToUpper(table.ModuleName)] = true
	}

	return &MetadataExtractor{
		data:          data,
		metadata:      metadata,
		tagTables:     tagTables,
		loadedModules: loadedModules,
	}
}

// loadModuleIfNeeded loads tables for a module if not already loaded
func (e *MetadataExtractor) loadModuleIfNeeded(moduleName string) {
	moduleUpper := strings.ToUpper(moduleName)
	if e.loadedModules[moduleUpper] {
		return
	}

	fmt.Printf("    Dynamically loading %s tables...\n", moduleName)
	count := 0

	// Create a list to show what we're loading
	var loadedNames []string

	for tableName, table := range tags.AllTags {
		// More inclusive matching
		tableUpper := strings.ToUpper(tableName)
		moduleNameUpper := strings.ToUpper(table.ModuleName)

		if strings.Contains(moduleNameUpper, moduleUpper) ||
			strings.Contains(tableUpper, moduleUpper) ||
			(moduleUpper == "EXIF" && strings.Contains(tableUpper, "::MAIN")) {
			e.tagTables = append(e.tagTables, table)
			count++
			// Collect first few table descriptions for debug
			if count <= 3 {
				loadedNames = append(loadedNames, fmt.Sprintf("%s (module: %s, %d tags)", tableName, table.ModuleName, len(table.Tags)))
			}
		}
	}

	if count > 0 {
		e.loadedModules[moduleUpper] = true
		fmt.Printf("    Loaded %d %s tables\n", count, moduleName)
		// Show what we loaded
		for _, name := range loadedNames {
			fmt.Printf("      - %s\n", name)
		}
		if count > 3 {
			fmt.Printf("      ... and %d more\n", count-3)
		}
	}
}

// ExtractAll extracts all types of metadata
func (e *MetadataExtractor) ExtractAll() (bool, bool) {
	foundEmbedded := e.extractEmbeddedMetadata()
	foundContainer := e.extractContainerMetadata()
	return foundEmbedded, foundContainer
}

// extractEmbeddedMetadata looks for any embedded metadata patterns
func (e *MetadataExtractor) extractEmbeddedMetadata() bool {
	found := false

	// Pattern 1: TIFF/EXIF header
	found = e.scanForTIFFHeaders() || found

	// Pattern 2: JPEG segments
	if e.isJPEG() {
		fmt.Println("  Detected JPEG structure")
		found = e.scanJPEGSegments() || found
	}

	// Pattern 3: PNG chunks
	if e.isPNG() {
		fmt.Println("  Detected PNG structure")
		found = e.scanPNGChunks() || found
	}

	// Pattern 4: IPTC data
	found = e.scanForIPTC() || found

	// Pattern 5: XMP data
	found = e.scanForXMP() || found

	return found
}

// extractContainerMetadata looks for container-based metadata patterns
func (e *MetadataExtractor) extractContainerMetadata() bool {
	found := false

	// Pattern 1: ZIP-based files (PK signature)
	if len(e.data) > 4 && e.data[0] == 'P' && e.data[1] == 'K' && e.data[2] == 0x03 && e.data[3] == 0x04 {
		fmt.Println("  Detected ZIP container structure")
		// For now, just note it's a ZIP
		// Full implementation would parse ZIP directory
		found = true
	}

	// Pattern 2: PDF files
	if len(e.data) > 5 && bytes.Equal(e.data[0:4], []byte("%PDF")) {
		fmt.Println("  Detected PDF structure")
		// For now, just note it's a PDF
		// Full implementation would parse PDF objects
		found = true
	}

	// Pattern 3: QuickTime/MP4 atom structure
	if len(e.data) > 12 {
		// Check for ftyp atom
		size := binary.BigEndian.Uint32(e.data[0:4])
		if size > 8 && size < uint32(len(e.data)) && bytes.Equal(e.data[4:8], []byte("ftyp")) {
			fmt.Println("  Detected QuickTime/MP4 atom structure")
			found = true
		}
	}

	return found
}

// Helper methods to identify file types
func (e *MetadataExtractor) isJPEG() bool {
	return len(e.data) > 2 && e.data[0] == 0xFF && e.data[1] == 0xD8
}

func (e *MetadataExtractor) isPNG() bool {
	return len(e.data) > 8 && bytes.Equal(e.data[0:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
}

// scanForTIFFHeaders scans for TIFF/EXIF headers in the data
func (e *MetadataExtractor) scanForTIFFHeaders() bool {
	found := false
	for i := 0; i < len(e.data)-8; i++ {
		if (e.data[i] == 'I' && e.data[i+1] == 'I' && e.data[i+2] == 42 && e.data[i+3] == 0) ||
			(e.data[i] == 'M' && e.data[i+1] == 'M' && e.data[i+2] == 0 && e.data[i+3] == 42) {
			fmt.Printf("  Found TIFF/EXIF header at offset %d\n", i)
			if e.extractTIFFMetadata(e.data[i:], i) {
				found = true
			}
		}
	}
	return found
}

// scanJPEGSegments scans for metadata in JPEG segments
func (e *MetadataExtractor) scanJPEGSegments() bool {
	found := false
	offset := 2 // Skip SOI

	for offset < len(e.data)-4 {
		if e.data[offset] != 0xFF {
			offset++
			continue
		}

		marker := e.data[offset+1]
		offset += 2

		// Skip stuffing bytes and standalone markers
		if marker == 0xFF || marker == 0x00 || marker == 0x01 ||
			(marker >= 0xD0 && marker <= 0xD8) {
			continue
		}

		if marker == 0xDA {
			break // Start of scan - image data follows
		}

		// Read segment length
		if offset+2 > len(e.data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(e.data[offset : offset+2]))
		offset += 2

		if offset+segLen-2 > len(e.data) {
			break
		}

		segData := e.data[offset : offset+segLen-2]

		// Check for known metadata markers
		if marker == 0xE1 && bytes.HasPrefix(segData, []byte("Exif\x00\x00")) {
			fmt.Printf("    APP1/EXIF segment\n")
			e.extractTIFFMetadata(segData[6:], offset)
			found = true
		} else if marker == 0xED && bytes.HasPrefix(segData, []byte("Photoshop 3.0\x00")) {
			fmt.Printf("    APP13/Photoshop segment\n")
			// Load Photoshop tables if needed
			e.loadModuleIfNeeded("Photoshop")
			// Scan for IPTC within Photoshop data
			for i := 14; i < len(segData)-5; i++ {
				if segData[i] == 0x1C {
					e.extractIPTCData(segData[i:], offset+i)
					found = true
					break
				}
			}
		} else if marker == 0xFE {
			// Comment
			if comment := strings.TrimSpace(string(segData)); comment != "" {
				e.metadata.Fields["Comment"] = comment
				found = true
			}
		}

		offset += segLen - 2
	}

	return found
}

// scanPNGChunks scans for metadata in PNG chunks
func (e *MetadataExtractor) scanPNGChunks() bool {
	found := false
	offset := 8 // Skip PNG signature

	for offset < len(e.data)-12 {
		chunkLen := int(binary.BigEndian.Uint32(e.data[offset : offset+4]))
		if offset+8+chunkLen+4 > len(e.data) {
			break
		}

		chunkType := string(e.data[offset+4 : offset+8])
		chunkData := e.data[offset+8 : offset+8+chunkLen]

		// Text chunks
		if chunkType == "tEXt" || chunkType == "zTXt" || chunkType == "iTXt" {
			if nullPos := bytes.IndexByte(chunkData, 0); nullPos > 0 {
				key := string(chunkData[:nullPos])
				value := string(chunkData[nullPos+1:])
				e.metadata.Fields["PNG:"+key] = value
				found = true
			}
		} else if chunkType == "eXIf" {
			// EXIF in PNG
			fmt.Printf("    eXIf chunk\n")
			e.extractTIFFMetadata(chunkData, offset+8)
			found = true
		}

		offset += 8 + chunkLen + 4 // length + type + data + CRC
	}

	return found
}

// scanForIPTC scans for IPTC data
func (e *MetadataExtractor) scanForIPTC() bool {
	// Load IPTC tables if needed
	e.loadModuleIfNeeded("IPTC")

	found := false
	for i := 0; i < len(e.data)-5; i++ {
		if e.data[i] == 0x1C && e.data[i+1] <= 0x0F && e.data[i+2] <= 0xFF {
			// Verify it looks like IPTC
			if i+5 < len(e.data) {
				dataLen := int(binary.BigEndian.Uint16(e.data[i+3 : i+5]))
				if i+5+dataLen <= len(e.data) {
					fmt.Printf("  Found IPTC data at offset %d\n", i)
					if e.extractIPTCData(e.data[i:], i) {
						found = true
					}
					break // Only process first IPTC block
				}
			}
		}
	}
	return found
}

// scanForXMP scans for XMP data
func (e *MetadataExtractor) scanForXMP() bool {
	if xmpStart := bytes.Index(e.data, []byte("<?xpacket begin=")); xmpStart >= 0 {
		if xmpEnd := bytes.Index(e.data[xmpStart:], []byte("<?xpacket end=")); xmpEnd > 0 {
			fmt.Printf("  Found XMP data at offset %d\n", xmpStart)
			xmpData := e.data[xmpStart : xmpStart+xmpEnd]

			// Basic XMP extraction - look for common tags
			// This is simplified - full XMP parsing would parse the XML properly
			e.extractBasicXMP(xmpData)

			e.metadata.Fields["XMPPacket"] = fmt.Sprintf("[%d bytes]", xmpEnd)
			return true
		}
	}
	return false
}

// extractBasicXMP performs basic XMP extraction
func (e *MetadataExtractor) extractBasicXMP(xmpData []byte) {
	// Load XMP tables if needed
	e.loadModuleIfNeeded("XMP")

	// Very basic extraction - look for common patterns
	// Full implementation would use XML parser
	patterns := map[string]string{
		"<dc:creator>":            "Creator",
		"<dc:description>":        "Description",
		"<dc:title>":              "Title",
		"<dc:subject>":            "Subject",
		"<photoshop:Credit>":      "Credit",
		"<photoshop:DateCreated>": "DateCreated",
	}

	for pattern, fieldName := range patterns {
		if idx := bytes.Index(xmpData, []byte(pattern)); idx >= 0 {
			// Look for the closing tag
			endPattern := strings.Replace(pattern, "<", "</", 1)
			if endIdx := bytes.Index(xmpData[idx:], []byte(endPattern)); endIdx > 0 {
				// Extract value between tags
				valueStart := idx + len(pattern)
				value := string(xmpData[valueStart : idx+endIdx])

				// Clean up any XML artifacts
				value = strings.TrimSpace(value)

				// Skip if it's a nested structure
				if !strings.Contains(value, "<") {
					e.metadata.Fields["XMP:"+fieldName] = value
					fmt.Printf("    Found XMP:%s = %.50s\n", fieldName, value)
				}
			}
		}
	}
}

// hasEXIFTables checks if we have EXIF tables loaded
func (e *MetadataExtractor) hasEXIFTables() bool {
	for _, table := range e.tagTables {
		if strings.Contains(strings.ToUpper(table.ModuleName), "EXIF") {
			return true
		}
	}
	return false
}

// findBasicEXIFTable searches for where basic EXIF tags should be
func (e *MetadataExtractor) findBasicEXIFTable() {
	fmt.Println("\n=== Looking for Basic EXIF Tags in ALL tables ===")

	// Search for ImageDescription (0x010E) in ALL tables
	hexKey := "0x010E"
	decKey := "270"

	found := false
	for tableName, table := range tags.AllTags {
		if tag, ok := table.Tags[hexKey]; ok {
			fmt.Printf("  Found 0x010E in %s (module: %s): %s\n", tableName, table.ModuleName, tag.Name)
			found = true
		}
		if tag, ok := table.Tags[decKey]; ok {
			fmt.Printf("  Found 270 in %s (module: %s): %s\n", tableName, table.ModuleName, tag.Name)
			found = true
		}
	}

	if !found {
		fmt.Println("  WARNING: Basic EXIF tag 0x010E (ImageDescription) not found in ANY table!")
		fmt.Println("  This suggests the tag tables were not generated correctly.")
	}

	// Check if there's a table that should contain basic EXIF tags
	for tableName, table := range tags.AllTags {
		if strings.Contains(tableName, "Exif") && strings.Contains(tableName, "Main") {
			fmt.Printf("\n  Checking %s for basic tags:\n", tableName)
			if _, ok := table.Tags["0x010E"]; ok {
				fmt.Println("    Has ImageDescription")
			}
			if _, ok := table.Tags["0x0112"]; ok {
				fmt.Println("    Has Orientation")
			}
			if _, ok := table.Tags["0x011A"]; ok {
				fmt.Println("    Has XResolution")
			}
			// Show first few tags in this table
			count := 0
			for key, tag := range table.Tags {
				if count < 5 {
					fmt.Printf("    Sample tag: %s -> %s\n", key, tag.Name)
					count++
				}
			}
		}
	}
}

// extractTIFFMetadata extracts metadata from TIFF/EXIF structures
func (e *MetadataExtractor) extractTIFFMetadata(data []byte, baseOffset int) bool {
	if len(data) < 8 {
		return false
	}

	// Debug: Find where basic EXIF tags should be
	e.findBasicEXIFTable()

	// Load EXIF tables when we encounter TIFF data
	e.loadModuleIfNeeded("EXIF")
	e.loadModuleIfNeeded("Exif")

	// Debug what's loaded
	e.debugTagTables()

	// Determine byte order
	var byteOrder binary.ByteOrder
	if data[0] == 'I' && data[1] == 'I' {
		byteOrder = binary.LittleEndian
		fmt.Println("    Little-endian byte order")
	} else if data[0] == 'M' && data[1] == 'M' {
		byteOrder = binary.BigEndian
		fmt.Println("    Big-endian byte order")
	} else {
		return false
	}

	// Check magic
	if byteOrder.Uint16(data[2:4]) != 42 {
		return false
	}

	// Get IFD offset
	ifdOffset := byteOrder.Uint32(data[4:8])
	fmt.Printf("    First IFD at offset: %d\n", ifdOffset)

	// Process IFDs
	processedTags := 0
	skippedTags := 0
	for ifdNum := 0; ifdOffset > 0 && ifdOffset < uint32(len(data)) && ifdNum < 10; ifdNum++ {
		numEntries := byteOrder.Uint16(data[ifdOffset : ifdOffset+2])
		fmt.Printf("    IFD%d: %d entries at offset %d\n", ifdNum, numEntries, ifdOffset)
		offset := ifdOffset + 2

		for i := 0; i < int(numEntries) && offset+12 <= uint32(len(data)); i++ {
			tagID := byteOrder.Uint16(data[offset : offset+2])
			dataType := byteOrder.Uint16(data[offset+2 : offset+4])
			count := byteOrder.Uint32(data[offset+4 : offset+8])
			valueOffset := byteOrder.Uint32(data[offset+8 : offset+12])

			// Debug: show tag info
			fmt.Printf("      Tag 0x%04X: type=%d count=%d offset=%d", tagID, dataType, count, valueOffset)

			// Try to decode tag across all tables
			tagInfo := e.findTagInTables(tagID)
			if tagInfo != nil {
				fmt.Printf(" -> %s", tagInfo.Name)
				value := e.extractTagValue(data, dataType, count, valueOffset, byteOrder)
				if value != nil {
					// Apply value mapping if available
					if tagInfo.Values != nil && len(tagInfo.Values) > 0 {
						value = e.applyValueMapping(value, tagInfo)
					}

					key := tagInfo.Name
					if key == "" {
						key = fmt.Sprintf("Tag_%04X", tagID)
					}

					// Ensure unique keys
					if _, exists := e.metadata.Fields[key]; exists {
						key = fmt.Sprintf("%s_%d", key, baseOffset+int(offset))
					}

					e.metadata.Fields[key] = value
					processedTags++
					fmt.Printf(" = %v", value)
				}
			} else {
				fmt.Printf(" -> UNKNOWN")
				skippedTags++
			}
			fmt.Println()

			offset += 12
		}

		// Next IFD
		if offset+4 <= uint32(len(data)) {
			ifdOffset = byteOrder.Uint32(data[offset : offset+4])
		} else {
			break
		}
	}

	fmt.Printf("    Processed %d tags, skipped %d unknown tags\n", processedTags, skippedTags)
	return processedTags > 0
}

// extractIPTCData extracts IPTC metadata
func (e *MetadataExtractor) extractIPTCData(data []byte, baseOffset int) bool {
	found := false
	offset := 0

	fmt.Printf("    Extracting IPTC data from offset %d\n", baseOffset)

	for offset < len(data)-5 {
		if data[offset] != 0x1C {
			break
		}

		record := data[offset+1]
		dataset := data[offset+2]
		offset += 3

		// Get length
		var dataLen int
		if data[offset]&0x80 != 0 {
			// Extended length
			lenBytes := int(data[offset] & 0x7F)
			offset++
			for i := 0; i < lenBytes && offset < len(data); i++ {
				dataLen = (dataLen << 8) | int(data[offset])
				offset++
			}
		} else {
			// Standard length
			if offset+2 > len(data) {
				break
			}
			dataLen = int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
		}

		if offset+dataLen > len(data) {
			break
		}

		// Find tag in tables
		iptcKey := fmt.Sprintf("%d:%d", record, dataset)
		tagInfo := e.findIPTCTagInTables(iptcKey)

		// Debug what we found
		fmt.Printf("      IPTC %s (record=%d, dataset=%d, len=%d)", iptcKey, record, dataset, dataLen)

		if tagInfo != nil {
			value := string(data[offset : offset+dataLen])
			key := tagInfo.Name
			if key == "" {
				key = fmt.Sprintf("IPTC_%s", iptcKey)
			}

			// Special handling for Keywords (2:25) - accumulate them
			if key == "Keywords" {
				if existing, ok := e.metadata.Fields[key]; ok {
					// Append to existing keywords
					if existingStr, ok := existing.(string); ok && existingStr != "" {
						value = existingStr + ", " + value
					}
				}
			}

			e.metadata.Fields[key] = value
			found = true
			fmt.Printf(" -> %s = %.50s\n", key, value)
		} else {
			fmt.Printf(" -> UNKNOWN\n")
		}

		offset += dataLen
	}

	return found
}

// findTagInTables searches for a tag across all loaded tables
func (e *MetadataExtractor) findTagInTables(tagID uint16) *tags.TagDef {
	hexKey := fmt.Sprintf("0x%04X", tagID)
	decKey := fmt.Sprintf("%d", tagID)

	for _, table := range e.tagTables {
		if tag, ok := table.Tags[hexKey]; ok {
			return &tag
		}
		if tag, ok := table.Tags[decKey]; ok {
			return &tag
		}
	}
	return nil
}

// findIPTCTagInTables searches for IPTC tags
func (e *MetadataExtractor) findIPTCTagInTables(key string) *tags.TagDef {
	for _, table := range e.tagTables {
		if strings.Contains(strings.ToUpper(table.ModuleName), "IPTC") {
			if tag, ok := table.Tags[key]; ok {
				return &tag
			}
		}
	}
	return nil
}

// getTypeName returns a readable name for TIFF data types
func (e *MetadataExtractor) getTypeName(dataType uint16) string {
	names := map[uint16]string{
		1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
		6: "SBYTE", 7: "UNDEF", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
		11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
	}
	if name, ok := names[dataType]; ok {
		return name
	}
	return fmt.Sprintf("TYPE%d", dataType)
}

// applyValueMapping applies tag value mappings
func (e *MetadataExtractor) applyValueMapping(value interface{}, tagDef *tags.TagDef) interface{} {
	key := ""
	if v, ok := value.(int); ok {
		key = fmt.Sprintf("%d", v)
	} else if v, ok := value.(string); ok {
		key = v
	} else {
		return value
	}

	if mapped, ok := tagDef.Values[key]; ok {
		return mapped
	}
	return value
}

// extractTagValue extracts value based on TIFF data type
func (e *MetadataExtractor) extractTagValue(data []byte, dataType uint16, count uint32, offset uint32, byteOrder binary.ByteOrder) interface{} {
	typeSizes := map[uint16]uint32{
		1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8,
		13: 4, // IFD
		16: 8, // LONG8
		17: 8, // SLONG8
		18: 8, // IFD8
	}

	size := typeSizes[dataType]
	if size == 0 {
		return nil
	}

	totalSize := size * count
	var valueData []byte

	if totalSize <= 4 {
		buf := make([]byte, 4)
		byteOrder.PutUint32(buf, offset)
		valueData = buf[:totalSize]
	} else {
		if int(offset+totalSize) > len(data) {
			return nil
		}
		valueData = data[offset : offset+totalSize]
	}

	// Handle based on type
	switch dataType {
	case 1: // BYTE
		if count == 1 {
			return int(valueData[0])
		}
		return valueData // Return as []byte for arrays

	case 2: // ASCII
		if end := bytes.IndexByte(valueData, 0); end >= 0 {
			return string(valueData[:end])
		}
		return string(valueData)

	case 3: // SHORT
		if count == 1 {
			return int(byteOrder.Uint16(valueData))
		}
		// Return array
		vals := make([]int, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = int(byteOrder.Uint16(valueData[i*2:]))
		}
		return vals

	case 4, 13: // LONG, IFD
		if count == 1 {
			return int(byteOrder.Uint32(valueData))
		}
		vals := make([]int, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = int(byteOrder.Uint32(valueData[i*4:]))
		}
		return vals

	case 5: // RATIONAL
		if count == 1 {
			num := byteOrder.Uint32(valueData[0:4])
			den := byteOrder.Uint32(valueData[4:8])
			if den == 0 {
				return "inf"
			}
			if num%den == 0 {
				return num / den
			}
			return fmt.Sprintf("%d/%d", num, den)
		}
		// Return array of rationals as strings
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			num := byteOrder.Uint32(valueData[i*8 : i*8+4])
			den := byteOrder.Uint32(valueData[i*8+4 : i*8+8])
			if den == 0 {
				vals[i] = "inf"
			} else if num%den == 0 {
				vals[i] = fmt.Sprintf("%d", num/den)
			} else {
				vals[i] = fmt.Sprintf("%d/%d", num, den)
			}
		}
		return vals

	case 6: // SBYTE
		if count == 1 {
			return int(int8(valueData[0]))
		}
		vals := make([]int, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = int(int8(valueData[i]))
		}
		return vals

	case 7: // UNDEFINED
		return valueData // Always return as []byte

	case 8: // SSHORT
		if count == 1 {
			return int(int16(byteOrder.Uint16(valueData)))
		}
		vals := make([]int, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = int(int16(byteOrder.Uint16(valueData[i*2:])))
		}
		return vals

	case 9: // SLONG
		if count == 1 {
			return int(int32(byteOrder.Uint32(valueData)))
		}
		vals := make([]int, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = int(int32(byteOrder.Uint32(valueData[i*4:])))
		}
		return vals

	case 10: // SRATIONAL
		if count == 1 {
			num := int32(byteOrder.Uint32(valueData[0:4]))
			den := int32(byteOrder.Uint32(valueData[4:8]))
			if den == 0 {
				return "inf"
			}
			if num%den == 0 {
				return num / den
			}
			return fmt.Sprintf("%d/%d", num, den)
		}
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			num := int32(byteOrder.Uint32(valueData[i*8 : i*8+4]))
			den := int32(byteOrder.Uint32(valueData[i*8+4 : i*8+8]))
			if den == 0 {
				vals[i] = "inf"
			} else if num%den == 0 {
				vals[i] = fmt.Sprintf("%d", num/den)
			} else {
				vals[i] = fmt.Sprintf("%d/%d", num, den)
			}
		}
		return vals

	case 11: // FLOAT
		if count == 1 {
			bits := byteOrder.Uint32(valueData)
			return math.Float32frombits(bits)
		}
		vals := make([]float32, count)
		for i := uint32(0); i < count; i++ {
			bits := byteOrder.Uint32(valueData[i*4:])
			vals[i] = math.Float32frombits(bits)
		}
		return vals

	case 12: // DOUBLE
		if count == 1 {
			bits := byteOrder.Uint64(valueData)
			return math.Float64frombits(bits)
		}
		vals := make([]float64, count)
		for i := uint32(0); i < count; i++ {
			bits := byteOrder.Uint64(valueData[i*8:])
			vals[i] = math.Float64frombits(bits)
		}
		return vals

	default:
		// For unknown types, return formatted string
		return fmt.Sprintf("[%s×%d]", e.getTypeName(dataType), count)
	}
}

// debugTagTables helps debug which tables are loaded and where tags are found
func (e *MetadataExtractor) debugTagTables() {
	fmt.Println("\n=== Debug: Tag Tables Content ===")

	// Look for specific tags we know should exist
	testTags := []uint16{0x010E, 0x0112, 0x011A, 0x011B, 0x0128, 0x013B}
	tagNames := []string{"ImageDescription", "Orientation", "XResolution", "YResolution", "ResolutionUnit", "Artist"}

	for i, tagID := range testTags {
		fmt.Printf("\nSearching for tag 0x%04X (%s):\n", tagID, tagNames[i])
		hexKey := fmt.Sprintf("0x%04X", tagID)
		decKey := fmt.Sprintf("%d", tagID)

		found := false
		for _, table := range e.tagTables {
			if tag, ok := table.Tags[hexKey]; ok {
				fmt.Printf("  FOUND in loaded table (module: %s) as hex key\n", table.ModuleName)
				fmt.Printf("    Name: %s\n", tag.Name)
				found = true
			}
			if tag, ok := table.Tags[decKey]; ok {
				fmt.Printf("  FOUND in loaded table (module: %s) as decimal key\n", table.ModuleName)
				fmt.Printf("    Name: %s\n", tag.Name)
				found = true
			}
		}

		if !found {
			// Check ALL tables in tags.AllTags
			for tableName, table := range tags.AllTags {
				if tag, ok := table.Tags[hexKey]; ok {
					fmt.Printf("  EXISTS in tags.AllTags[\"%s\"] (module: %s) but NOT LOADED\n", tableName, table.ModuleName)
					fmt.Printf("    Tag Name: %s\n", tag.Name)
				}
				if tag, ok := table.Tags[decKey]; ok {
					fmt.Printf("  EXISTS in tags.AllTags[\"%s\"] (module: %s) as decimal but NOT LOADED\n", tableName, table.ModuleName)
					fmt.Printf("    Tag Name: %s\n", tag.Name)
				}
			}
		}
	}

	// Show what modules are loaded
	fmt.Println("\n=== Loaded Modules ===")
	for module := range e.loadedModules {
		fmt.Printf("  %s\n", module)
	}

	// Show what tables are actually loaded
	fmt.Printf("\n=== Loaded Tables (%d total) ===\n", len(e.tagTables))
	for i, table := range e.tagTables {
		if i < 10 || strings.Contains(strings.ToUpper(table.ModuleName), "EXIF") {
			fmt.Printf("  Table[%d]: Module=%s, Tags=%d\n", i, table.ModuleName, len(table.Tags))
		}
	}
	if len(e.tagTables) > 10 {
		fmt.Printf("  ... and %d more tables\n", len(e.tagTables)-10)
	}
}
