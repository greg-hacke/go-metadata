package tags

// TagDef represents a metadata tag definition
type TagDef struct {
	ID          string            // Tag identifier (name or hex ID)
	Name        string            // Human-readable name
	Description string            // Tag description
	Format      string            // Data format (e.g., "int16u", "string")
	Groups      map[string]string // Group memberships
	Values      map[string]string // Value mappings (for enumerated types)
}

// AllTags consolidates all tag tables
var AllTags = make(map[string]map[string]TagDef)

// RegisterTagTable registers a tag table
func RegisterTagTable(namespace string, tags map[string]TagDef) {
	AllTags[namespace] = tags
}

// GetTag retrieves a tag definition by namespace and ID
func GetTag(namespace, id string) (TagDef, bool) {
	if table, ok := AllTags[namespace]; ok {
		tag, found := table[id]
		return tag, found
	}
	return TagDef{}, false
}
