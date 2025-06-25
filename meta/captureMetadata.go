package meta

import (
	"fmt"
	"io"
	"strings"

	"greg-hacke/go-metadata/tags"
)

// MetadataStorage represents how metadata is stored in a file
type MetadataStorage int

const (
	StorageUnknown   MetadataStorage = iota
	StorageContainer                 // Container-level metadata (e.g., PDF, ZIP)
	StorageEmbedded                  // Embedded tag-based metadata (e.g., EXIF in JPEG)
	StorageHybrid                    // Both container and embedded (e.g., TIFF with EXIF)
)

// Metadata represents extracted metadata
type Metadata struct {
	// Placeholder for now - will be expanded
	Tags map[string]interface{}
}

// ToJSON converts metadata to JSON string
func (m *Metadata) ToJSON() (string, error) {
	// Placeholder implementation
	return "{}", nil
}

// CaptureMetadata extracts metadata based on file type and available tag tables
func CaptureMetadata(file io.ReadSeeker, fileType *FileType, tagTables []*tags.TagTable, requested MetadataRequest) (*Metadata, error) {
	fmt.Printf("\n=== Metadata Capture Analysis ===\n")
	fmt.Printf("File Type: %s (Module: %s)\n", fileType.Format, fileType.Module)
	fmt.Printf("Total Available Tag Tables: %d\n", len(tagTables))

	// Determine storage type based on file format
	storageType := determineStorageType(fileType)
	fmt.Printf("Storage Type: %s\n", storageTypeString(storageType))

	// Group tables by metadata type for analysis
	tablesByType := make(map[string]int)
	for _, table := range tagTables {
		tablesByType[table.ModuleName]++
	}

	fmt.Printf("\nAvailable Tables by Module:\n")
	// Show summary of what's available
	relevantModules := 0
	for module, count := range tablesByType {
		if isRelevantModule(module, fileType, storageType) {
			fmt.Printf("  %s: %d tables\n", module, count)
			relevantModules++
		}
	}
	fmt.Printf("Total relevant module types: %d\n", relevantModules)

	// TODO: Parse file to determine which metadata types are actually present
	fmt.Printf("\nNext Steps:\n")
	switch storageType {
	case StorageEmbedded:
		fmt.Printf("  1. Parse JPEG/PNG segments to find APP markers\n")
		fmt.Printf("  2. Identify which metadata types are present (EXIF, IPTC, XMP)\n")
		fmt.Printf("  3. Use appropriate tables to decode each type\n")
	case StorageContainer:
		fmt.Printf("  1. Parse container structure (PDF objects, ZIP entries, etc.)\n")
		fmt.Printf("  2. Extract metadata from appropriate locations\n")
		fmt.Printf("  3. Use format-specific tables to decode\n")
	case StorageHybrid:
		fmt.Printf("  1. Parse both container structure and embedded metadata\n")
		fmt.Printf("  2. Handle IFD chains for TIFF-based formats\n")
		fmt.Printf("  3. Extract from multiple metadata locations\n")
	}

	// For now, return empty metadata
	return &Metadata{
		Tags: make(map[string]interface{}),
	}, nil
}

// isRelevantModule checks if a module is potentially relevant for the file type
func isRelevantModule(module string, fileType *FileType, storageType MetadataStorage) bool {
	moduleUpper := strings.ToUpper(module)
	formatUpper := strings.ToUpper(fileType.Format)

	// Always show the file's own format
	if moduleUpper == formatUpper {
		return true
	}

	// For embedded storage, show standard metadata formats
	if storageType == StorageEmbedded || storageType == StorageHybrid {
		standardFormats := []string{"EXIF", "IPTC", "XMP", "GPS", "PHOTOSHOP", "JFIF", "ICC_PROFILE"}
		for _, std := range standardFormats {
			if strings.Contains(moduleUpper, std) {
				return true
			}
		}
	}

	// For specific formats, show related modules
	switch formatUpper {
	case "PDF":
		return moduleUpper == "PDF" || moduleUpper == "XMP"
	case "TIFF", "TIF":
		// TIFF uses EXIF for its metadata
		return strings.Contains(moduleUpper, "EXIF") ||
			strings.Contains(moduleUpper, "IPTC") ||
			strings.Contains(moduleUpper, "XMP")
	case "MP4", "MOV":
		return strings.Contains(moduleUpper, "QUICKTIME") ||
			strings.Contains(moduleUpper, "MP4") ||
			moduleUpper == "XMP"
	}

	return false
}

// determineStorageType identifies how metadata is stored for a file type
func determineStorageType(fileType *FileType) MetadataStorage {
	format := strings.ToUpper(fileType.Format)

	// CASE statement for known formats
	switch format {
	// Container-based formats
	case "PDF", "ZIP", "RAR", "7Z", "TAR", "GZIP":
		return StorageContainer
	case "MP4", "MOV", "AVI", "MKV", "WEBM":
		return StorageContainer
	case "MP3", "OGG", "FLAC", "WAV":
		return StorageContainer
	case "DOC", "DOCX", "XLS", "XLSX", "PPT", "PPTX":
		return StorageContainer

	// Embedded metadata formats
	case "JPEG", "JPG":
		return StorageEmbedded
	case "PNG":
		return StorageEmbedded
	case "GIF":
		return StorageEmbedded
	case "BMP":
		return StorageEmbedded

	// Hybrid formats (both container and embedded)
	case "TIFF", "TIF":
		return StorageHybrid
	case "PSD":
		return StorageHybrid
	case "EPS":
		return StorageHybrid
	case "HEIC", "HEIF":
		return StorageHybrid

	// RAW image formats (mostly embedded/TIFF-based)
	case "DNG", "CR2", "CR3", "NEF", "ARW", "RAF", "ORF", "RW2":
		return StorageEmbedded
	case "X3F":
		return StorageContainer // Sigma X3F is container-based

	default:
		// Check if it's a known RAW format by module
		if strings.Contains(fileType.Module, "Canon") ||
			strings.Contains(fileType.Module, "Nikon") ||
			strings.Contains(fileType.Module, "Sony") ||
			strings.Contains(fileType.Module, "Panasonic") {
			return StorageEmbedded
		}
		return StorageUnknown
	}
}

// storageTypeString returns a string representation of the storage type
func storageTypeString(st MetadataStorage) string {
	switch st {
	case StorageContainer:
		return "Container-level metadata"
	case StorageEmbedded:
		return "Embedded tag-based metadata"
	case StorageHybrid:
		return "Hybrid (container + embedded)"
	default:
		return "Unknown"
	}
}
