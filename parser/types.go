package parser

// ParsedData contains all data parsed from PM files
type ParsedData struct {
	// TagTables maps table name to its tag table
	// e.g. "JPEG::Main" -> JPEG Main table
	TagTables map[string]*TagTable

	// FileTypes maps file extensions to module names
	// e.g. ".jpg" -> "JPEG"
	FileTypes map[string]string

	// MIMETypes maps MIME types to module names
	// e.g. "image/jpeg" -> "JPEG"
	MIMETypes map[string]string
}

// TagTable represents tags from a single PM module/table
type TagTable struct {
	ModuleName  string             // e.g. "JPEG", "EXIF", "XMP"
	PackageName string             // Full Perl package name
	Tags        map[string]*TagDef // Tag ID -> definition
}

// TagDef represents a single tag definition
type TagDef struct {
	ID          string            // Tag ID (hex or name)
	Name        string            // Human-readable name
	Description string            // Tag description
	Format      string            // Data format
	Groups      map[string]string // Group memberships
	Values      map[string]string // Value mappings (enums)
	SubIFD      string            // For EXIF SubIFD pointers
}
