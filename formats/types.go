// File: tags/types.go

package tags

// TagTable represents tags from a single PM module/table
type TagTable struct {
	ModuleName string            // e.g. "JPEG", "EXIF", "XMP"
	Tags       map[string]TagDef // Tag ID -> definition
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
