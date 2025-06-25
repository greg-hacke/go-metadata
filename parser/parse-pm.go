// File: parser/parse-pm.go

package parser

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// isNumeric checks if a string contains only digits
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// ParsePMFiles recursively parses all .pm files in the given directory
func ParsePMFiles(rootDir string) (*ParsedData, error) {
	data := &ParsedData{
		TagTables: make(map[string]*TagTable),
		FileTypes: make(map[string]string),
		MIMETypes: make(map[string]string),
	}

	// First pass: parse tag tables
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Only process .pm files
		if !strings.HasSuffix(path, ".pm") || d.IsDir() {
			return nil
		}

		// Parse the PM file for tags
		if err := parsePMFile(path, data); err != nil {
			// Log error but continue
			fmt.Fprintf(os.Stderr, "Warning: error parsing %s: %v\n", path, err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Second pass: parse for file types
	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Only process .pm files
		if !strings.HasSuffix(path, ".pm") || d.IsDir() {
			return nil
		}

		// Parse the PM file for file types
		if err := parsePMFileForFileTypes(path, data); err != nil {
			// Log error but continue
			fmt.Fprintf(os.Stderr, "Warning: error parsing file types from %s: %v\n", path, err)
		}

		return nil
	})

	// Also look for the main ExifTool.pm file which contains the master file type list
	exifToolPM := filepath.Join(rootDir, "..", "..", "ExifTool.pm")
	if _, err := os.Stat(exifToolPM); err == nil {
		parseMainExifToolPM(exifToolPM, data)
	}

	return data, err
}

// parsePMFile parses a single PM file and adds data to ParsedData
func parsePMFile(path string, data *ParsedData) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Extract module name from path
	// e.g., .../Image/ExifTool/JPEG.pm -> JPEG
	baseName := filepath.Base(path)
	moduleName := strings.TrimSuffix(baseName, ".pm")

	scanner := bufio.NewScanner(file)
	var currentTable *TagTable
	var currentTag *TagDef
	var currentKey string
	var inTagTable bool
	var inTagDef bool
	var bracketDepth int
	var parenDepth int
	var packageName string
	var collectingValue bool
	var valueBuffer strings.Builder

	// Regex patterns
	packageRe := regexp.MustCompile(`^\s*package\s+(.+?)\s*;`)
	tagTableRe := regexp.MustCompile(`^\s*%([A-Za-z0-9_:]+)\s*=\s*\(`)
	// Updated patterns to be more flexible with whitespace and brackets
	tagDefStartRe := regexp.MustCompile(`^\s*(?:'([^']+)'|"([^"]+)"|0x([0-9a-fA-F]+)|(\d+)|(\w+))\s*=>\s*\{`)
	tagDefInlineRe := regexp.MustCompile(`^\s*(?:'([^']+)'|"([^"]+)"|0x([0-9a-fA-F]+)|(\d+)|(\w+))\s*=>\s*(.+?)(?:,\s*)?$`)
	nameRe := regexp.MustCompile(`Name\s*=>\s*'([^']+)'`)
	descRe := regexp.MustCompile(`Description\s*=>\s*'([^']+)'`)
	notesRe := regexp.MustCompile(`Notes\s*=>\s*(?:'([^']+)'|q\{([^}]+)\})`)
	formatRe := regexp.MustCompile(`Format\s*=>\s*'([^']+)'`)
	writableRe := regexp.MustCompile(`Writable\s*=>\s*(\d+|'[^']+')`)
	groupsRe := regexp.MustCompile(`Groups\s*=>\s*\{([^}]+)\}`)
	printConvRe := regexp.MustCompile(`PrintConv\s*=>\s*[\{\[]`)
	conditionRe := regexp.MustCompile(`Condition\s*=>\s*'([^']+)'`)
	subDirRe := regexp.MustCompile(`SubDirectory\s*=>\s*\{[^}]*TagTable\s*=>\s*'([^']+)'`)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		// Look for package declaration
		if matches := packageRe.FindStringSubmatch(line); matches != nil {
			packageName = matches[1]
			continue
		}

		// Look for tag table start
		if matches := tagTableRe.FindStringSubmatch(line); matches != nil {
			// Save any pending tag
			if currentTable != nil && currentTag != nil && currentKey != "" {
				currentTable.Tags[currentKey] = currentTag
				currentTag = nil
				currentKey = ""
			}

			tableName := matches[1]
			// Extract the actual table name from the full name
			// e.g., Image::ExifTool::JPEG::Main -> Main
			parts := strings.Split(tableName, "::")
			shortName := parts[len(parts)-1]

			currentTable = &TagTable{
				ModuleName:  moduleName,
				PackageName: packageName,
				Tags:        make(map[string]*TagDef),
			}

			// Store with full name for uniqueness
			fullName := moduleName + "::" + shortName

			// Debug IPTC tables
			if strings.Contains(moduleName, "IPTC") {
				fmt.Fprintf(os.Stderr, "DEBUG: Found IPTC table: %s (module: %s, package: %s)\n", fullName, moduleName, packageName)
			}

			// Check for duplicates and make unique if needed
			if _, exists := data.TagTables[fullName]; exists {
				// Add package suffix to make unique
				packageSuffix := strings.ReplaceAll(packageName, "Image::ExifTool::", "")
				packageSuffix = strings.ReplaceAll(packageSuffix, "::", "_")
				fullName = moduleName + "::" + shortName + "_" + packageSuffix
			}

			data.TagTables[fullName] = currentTable

			inTagTable = true
			inTagDef = false
			bracketDepth = 0
			parenDepth = 1 // We just saw the opening paren
			continue
		}

		if !inTagTable {
			continue
		}

		// Track bracket/paren depth (do this after table detection)
		for _, ch := range line {
			switch ch {
			case '{', '[':
				bracketDepth++
			case '}', ']':
				bracketDepth--
			case '(':
				parenDepth++
			case ')':
				parenDepth--
			}
		}

		// Debug IPTC lines
		if currentTable != nil && strings.Contains(currentTable.ModuleName, "IPTC") &&
			strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			// Show first few characters of the line for debugging
			preview := line
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			fmt.Fprintf(os.Stderr, "DEBUG: IPTC line (depth %d/%d, inDef=%t): %s\n", bracketDepth, parenDepth, inTagDef, preview)
		}

		// Check for table end
		if parenDepth == 0 && strings.Contains(line, ");") {
			inTagTable = false
			currentTable = nil
			continue
		}

		// Skip table metadata like NOTES, GROUPS, etc. at table level
		if !inTagDef && bracketDepth == 0 && (strings.Contains(line, "NOTES =>") ||
			strings.Contains(line, "GROUPS =>") ||
			strings.Contains(line, "PROCESS_PROC =>") ||
			strings.Contains(line, "VARS =>") ||
			strings.Contains(line, "FIRST_ENTRY =>") ||
			strings.Contains(line, "TAG_PREFIX =>") ||
			strings.Contains(line, "WRITE_PROC =>") ||
			strings.Contains(line, "CHECK_PROC =>") ||
			strings.Contains(line, "WRITABLE =>")) {
			continue
		}

		// Look for tag definition start (multiline)
		if !inTagDef {
			if matches := tagDefStartRe.FindStringSubmatch(line); matches != nil {
				// Save previous tag
				if currentTag != nil && currentKey != "" {
					currentTable.Tags[currentKey] = currentTag
				}

				// Extract key from matches
				currentKey = extractTagKey(matches)

				// Debug IPTC parsing
				if strings.Contains(moduleName, "IPTC") && currentKey != "" {
					fmt.Fprintf(os.Stderr, "DEBUG: IPTC tag found: %s (from line: %s)\n", currentKey, line)
				}

				// Special handling for IPTC tags
				if currentTable != nil && strings.Contains(currentTable.ModuleName, "IPTC") && isNumeric(currentKey) {
					// Convert single number to IPTC format (record:dataset)
					num, _ := strconv.Atoi(currentKey)
					if num < 256 {
						// Single byte - assume record 2
						currentKey = fmt.Sprintf("2:%d", num)
					} else {
						// Two bytes encoded
						record := num >> 8
						dataset := num & 0xFF
						currentKey = fmt.Sprintf("%d:%d", record, dataset)
					}
					fmt.Fprintf(os.Stderr, "DEBUG: IPTC tag converted to: %s\n", currentKey)
				}

				currentTag = &TagDef{
					ID:     currentKey,
					Groups: make(map[string]string),
					Values: make(map[string]string),
				}
				inTagDef = true
				continue
			}

			// Look for inline tag definition
			if matches := tagDefInlineRe.FindStringSubmatch(line); matches != nil {
				// Save previous tag
				if currentTag != nil && currentKey != "" {
					currentTable.Tags[currentKey] = currentTag
				}

				// Extract key from matches
				currentKey = extractTagKey(matches[:6]) // first 5 groups are the key patterns
				value := matches[6]

				// Debug IPTC parsing
				if strings.Contains(moduleName, "IPTC") && currentKey != "" {
					fmt.Fprintf(os.Stderr, "DEBUG: IPTC inline tag found: %s = %s (from line: %s)\n", currentKey, value, line)
				}

				// Special handling for IPTC tags
				if currentTable != nil && strings.Contains(currentTable.ModuleName, "IPTC") && isNumeric(currentKey) {
					// Convert single number to IPTC format (record:dataset)
					num, _ := strconv.Atoi(currentKey)
					if num < 256 {
						// Single byte - assume record 2
						currentKey = fmt.Sprintf("2:%d", num)
					} else {
						// Two bytes encoded
						record := num >> 8
						dataset := num & 0xFF
						currentKey = fmt.Sprintf("%d:%d", record, dataset)
					}
					fmt.Fprintf(os.Stderr, "DEBUG: IPTC tag converted to: %s\n", currentKey)
				}

				currentTag = &TagDef{
					ID:     currentKey,
					Groups: make(map[string]string),
					Values: make(map[string]string),
				}

				// Handle simple string values
				if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
					currentTag.Name = strings.Trim(value, "'")
				} else if value == "{" || value == "[{" {
					// Starting a complex definition
					inTagDef = true
					continue
				}

				// Save simple tag
				currentTable.Tags[currentKey] = currentTag
				currentTag = nil
				currentKey = ""
			}
		}

		// Parse tag properties
		if inTagDef && currentTag != nil {
			// Handle PrintConv collections
			if collectingValue {
				valueBuffer.WriteString(line + "\n")
				// Check if we've closed the PrintConv
				if (strings.Contains(line, "}") || strings.Contains(line, "]")) && bracketDepth <= 1 {
					collectingValue = false
					parseValueMappings(valueBuffer.String(), currentTag)
					valueBuffer.Reset()
				}
				continue
			}

			// Parse various tag properties
			if matches := nameRe.FindStringSubmatch(line); matches != nil {
				currentTag.Name = matches[1]
			} else if matches := descRe.FindStringSubmatch(line); matches != nil {
				currentTag.Description = matches[1]
			} else if matches := notesRe.FindStringSubmatch(line); matches != nil {
				if matches[1] != "" {
					currentTag.Description = matches[1]
				} else if matches[2] != "" {
					currentTag.Description = matches[2]
				}
			} else if matches := formatRe.FindStringSubmatch(line); matches != nil {
				currentTag.Format = matches[1]
			} else if matches := conditionRe.FindStringSubmatch(line); matches != nil {
				// Store condition as a special property
				if currentTag.Groups == nil {
					currentTag.Groups = make(map[string]string)
				}
				currentTag.Groups["_condition"] = matches[1]
			} else if matches := subDirRe.FindStringSubmatch(line); matches != nil {
				currentTag.SubIFD = matches[1]
			} else if matches := writableRe.FindStringSubmatch(line); matches != nil {
				// Store writable flag
				if currentTag.Groups == nil {
					currentTag.Groups = make(map[string]string)
				}
				currentTag.Groups["_writable"] = matches[1]
			} else if matches := groupsRe.FindStringSubmatch(line); matches != nil {
				// Parse groups
				parseGroups(matches[1], currentTag)
			} else if printConvRe.MatchString(line) {
				// Start collecting PrintConv values
				collectingValue = true
				valueBuffer.WriteString(line + "\n")
			}

			// Check if tag definition is complete
			if bracketDepth == 0 && (strings.Contains(line, "},") || strings.Contains(line, "}")) {
				inTagDef = false
				if currentTag != nil && currentKey != "" {
					currentTable.Tags[currentKey] = currentTag
				}
				currentTag = nil
				currentKey = ""
			}
		}
	}

	// Save last tag if any
	if currentTable != nil && currentTag != nil && currentKey != "" {
		currentTable.Tags[currentKey] = currentTag
	}

	// Debug: Report IPTC tag counts
	if strings.Contains(moduleName, "IPTC") && currentTable != nil {
		fmt.Fprintf(os.Stderr, "DEBUG: IPTC table %s has %d tags\n", moduleName, len(currentTable.Tags))
		if len(currentTable.Tags) > 0 {
			count := 0
			for id, tag := range currentTable.Tags {
				fmt.Fprintf(os.Stderr, "  Tag %s: %s\n", id, tag.Name)
				count++
				if count >= 10 {
					fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(currentTable.Tags)-10)
					break
				}
			}
		}
	}

	return scanner.Err()
}

// parsePMFileForFileTypes parses a PM file specifically for file type information
func parsePMFileForFileTypes(path string, data *ParsedData) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Read entire file content
	content, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	// Extract module name
	baseName := filepath.Base(path)
	moduleName := strings.TrimSuffix(baseName, ".pm")

	// Look for file type definitions in various patterns

	// Pattern 1: %fileTypeLookup or similar hashes
	fileTypeLookupRe := regexp.MustCompile(`%(?:fileTypeLookup|fileType|supportedExtensions)\s*=\s*\([^)]+\)`)
	if matches := fileTypeLookupRe.FindAllString(string(content), -1); len(matches) > 0 {
		for _, match := range matches {
			parseFileTypeLookup(match, moduleName, data)
		}
	}

	// Pattern 2: File extensions in comments
	// # Supported: NEF, NRW
	commentExtRe := regexp.MustCompile(`#.*?(?:Supported|Extensions?|Files?).*?:\s*([\w\s,\.]+)`)
	if matches := commentExtRe.FindAllStringSubmatch(string(content), -1); len(matches) > 0 {
		for _, match := range matches {
			if len(match) > 1 {
				parseCommentExtensions(match[1], moduleName, data)
			}
		}
	}

	// Pattern 3: Extension checks in code
	// $ext eq '.nef' or $file =~ /\.nef$/i
	extCheckRe := regexp.MustCompile(`(?:\$ext\s*eq\s*['"](\.\w+)['"]|\$\w+\s*=~\s*/\\\.(\w+)\$/)`)
	if matches := extCheckRe.FindAllStringSubmatch(string(content), -1); len(matches) > 0 {
		for _, match := range matches {
			ext := ""
			if match[1] != "" {
				ext = match[1]
			} else if match[2] != "" {
				ext = "." + match[2]
			}
			if ext != "" {
				data.FileTypes[strings.ToLower(ext)] = moduleName
			}
		}
	}

	// Pattern 4: MIME type definitions
	mimeTypeRe := regexp.MustCompile(`['"]?([\w/\+\-\.]+)['"]?\s*=>\s*['"]?(\w+)['"]?`)
	if strings.Contains(string(content), "MIMEType") || strings.Contains(string(content), "mime") {
		if matches := mimeTypeRe.FindAllStringSubmatch(string(content), -1); len(matches) > 0 {
			for _, match := range matches {
				if len(match) > 2 && strings.Contains(match[1], "/") {
					// This looks like a MIME type
					data.MIMETypes[match[1]] = moduleName
				}
			}
		}
	}

	// Pattern 5: For specific known modules, add their extensions
	// This handles cases where the PM file doesn't explicitly list extensions
	addKnownModuleExtensions(moduleName, data)

	return nil
}

// parseMainExifToolPM parses the main ExifTool.pm file for file type mappings
func parseMainExifToolPM(path string, data *ParsedData) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	// Look for %fileTypeLookup hash
	// This is the master list of file extensions in ExifTool
	fileTypeLookupRe := regexp.MustCompile(`%fileTypeLookup\s*=\s*\(([\s\S]*?)\);`)
	matches := fileTypeLookupRe.FindSubmatch(content)
	if len(matches) > 1 {
		lookupContent := string(matches[1])

		// Parse entries like: NEF => ['NEF', 'Nikon Electronic Format'],
		entryRe := regexp.MustCompile(`(\w+)\s*=>\s*\[['"](\w+)['"](?:,\s*['"][^'"]*['"])?\]`)
		entries := entryRe.FindAllStringSubmatch(lookupContent, -1)

		for _, entry := range entries {
			if len(entry) > 2 {
				ext := "." + strings.ToLower(entry[1])
				moduleName := entry[2]
				data.FileTypes[ext] = moduleName
			}
		}
	}

	// Also look for %mimeType hash
	mimeTypeHashRe := regexp.MustCompile(`%mimeType\s*=\s*\(([\s\S]*?)\);`)
	matches = mimeTypeHashRe.FindSubmatch(content)
	if len(matches) > 1 {
		mimeContent := string(matches[1])

		// Parse entries like: 'image/jpeg' => 'JPEG',
		mimeEntryRe := regexp.MustCompile(`['"]([^'"]+)['"]\s*=>\s*['"](\w+)['"]`)
		entries := mimeEntryRe.FindAllStringSubmatch(mimeContent, -1)

		for _, entry := range entries {
			if len(entry) > 2 {
				data.MIMETypes[entry[1]] = entry[2]
			}
		}
	}

	return nil
}

// parseFileTypeLookup parses a fileTypeLookup hash
func parseFileTypeLookup(hashContent string, moduleName string, data *ParsedData) {
	// Parse entries like: 'NEF' => ['NEF', 'Nikon Electronic Format'],
	entryRe := regexp.MustCompile(`['"](\w+)['"]\s*=>\s*\[['"](\w+)['"]`)
	matches := entryRe.FindAllStringSubmatch(hashContent, -1)
	for _, match := range matches {
		if len(match) > 1 {
			ext := "." + strings.ToLower(match[1])
			// Map to the current module or the specified type
			targetModule := moduleName
			if len(match) > 2 && match[2] != "" {
				// Sometimes the second field indicates the module
				targetModule = match[2]
			}
			data.FileTypes[ext] = targetModule
		}
	}
}

// parseCommentExtensions parses extensions from comments
func parseCommentExtensions(extList string, moduleName string, data *ParsedData) {
	// Split by common delimiters
	exts := regexp.MustCompile(`[,\s]+`).Split(extList, -1)
	for _, ext := range exts {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		// Add dot if missing
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		data.FileTypes[strings.ToLower(ext)] = moduleName
	}
}

// addKnownModuleExtensions adds extensions for known modules
// This is a bridge until we can extract all extensions from PM files
func addKnownModuleExtensions(moduleName string, data *ParsedData) {
	// Map of module names to their known extensions
	// This is based on ExifTool documentation and common usage
	knownExtensions := map[string][]string{
		"Nikon":       {".nef", ".nrw"},
		"Canon":       {".crw", ".cr2", ".cr3", ".crm"},
		"Sony":        {".arw", ".sr2", ".srf"},
		"Olympus":     {".orf"},
		"Panasonic":   {".rw2"},
		"Pentax":      {".pef", ".dng"},
		"FujiFilm":    {".raf"},
		"Minolta":     {".mrw"},
		"Sigma":       {".x3f"},
		"FLAC":        {".flac"},
		"DNG":         {".dng"},
		"HEIF":        {".heif", ".heic"},
		"WebP":        {".webp"},
		"AVIF":        {".avif"},
		"Opus":        {".opus"},
		"Vorbis":      {".ogg", ".oga"},
		"Theora":      {".ogv"},
		"Matroska":    {".mkv", ".mka", ".mks", ".webm"},
		"ASF":         {".asf", ".wmv", ".wma"},
		"Real":        {".rm", ".rmvb", ".ra"},
		"MPEG":        {".mpg", ".mpeg", ".m1v", ".m2v"},
		"M2TS":        {".m2ts", ".mts", ".m2t", ".ts"},
		"DV":          {".dv"},
		"SWF":         {".swf"},
		"FLV":         {".flv", ".f4v"},
		"OGG":         {".ogg", ".ogv", ".oga", ".ogx", ".spx"},
		"MXF":         {".mxf"},
		"GIF":         {".gif"},
		"BMP":         {".bmp", ".dib"},
		"TIFF":        {".tif", ".tiff"},
		"PSD":         {".psd", ".psb"},
		"EPS":         {".eps", ".epsf", ".ps"},
		"XMP":         {".xmp"},
		"ICC_Profile": {".icc", ".icm"},
		"VCard":       {".vcf"},
		"HTML":        {".html", ".htm", ".xhtml"},
		"XML":         {".xml"},
		"JSON":        {".json"},
		"ZIP":         {".zip"},
		"RAR":         {".rar"},
		"GZIP":        {".gz", ".gzip"},
		"BZIP2":       {".bz2"},
		"TAR":         {".tar"},
		"LNK":         {".lnk"},
		"Font":        {".ttf", ".otf", ".ttc"},
		"FITS":        {".fits", ".fit", ".fts"},
		"MIFF":        {".miff", ".mif"},
		"PCX":         {".pcx"},
		"PICT":        {".pict", ".pct"},
		"WPG":         {".wpg"},
		"XBM":         {".xbm"},
		"XPM":         {".xpm"},
		"OpenEXR":     {".exr"},
		"DPX":         {".dpx"},
		"JPEG2000":    {".jp2", ".jpf", ".jpx", ".j2k", ".jpc"},
		"DJVU":        {".djvu", ".djv"},
		"AIFF":        {".aiff", ".aif", ".aifc"},
		"APE":         {".ape"},
		"MOI":         {".moi"},
		"ITC":         {".itc"},
		"ISO":         {".iso"},
		"EXE":         {".exe", ".dll"},
		"CHM":         {".chm"},
		"LIF":         {".lif"},
		"PDB":         {".pdb", ".prc"},
		"Torrent":     {".torrent"},
	}

	if exts, ok := knownExtensions[moduleName]; ok {
		for _, ext := range exts {
			data.FileTypes[ext] = moduleName
		}
	}

	// Also check for common MIME types
	knownMIMETypes := map[string]map[string]string{
		"Nikon":    {"image/x-nikon-nef": "NEF"},
		"Canon":    {"image/x-canon-cr2": "CR2", "image/x-canon-crw": "CRW"},
		"FLAC":     {"audio/flac": "FLAC", "audio/x-flac": "FLAC"},
		"WebP":     {"image/webp": "WebP"},
		"HEIF":     {"image/heif": "HEIF", "image/heic": "HEIC"},
		"Opus":     {"audio/opus": "Opus"},
		"Matroska": {"video/x-matroska": "MKV", "video/webm": "WebM"},
	}

	if mimes, ok := knownMIMETypes[moduleName]; ok {
		for mime, _ := range mimes {
			data.MIMETypes[mime] = moduleName
		}
	}
}

// extractTagKey extracts the tag key from regex matches
func extractTagKey(matches []string) string {
	if matches[1] != "" {
		return matches[1]
	} else if matches[2] != "" {
		return matches[2]
	} else if matches[3] != "" {
		return "0x" + strings.ToUpper(matches[3])
	} else if matches[4] != "" {
		return matches[4]
	} else if matches[5] != "" {
		return matches[5]
	}
	return ""
}

// parseGroups parses the Groups specification
func parseGroups(groupsStr string, tag *TagDef) {
	// Parse groups like: 0 => 'APP1', 1 => 'Parrot', 2 => 'Preview'
	groupRe := regexp.MustCompile(`(\d+)\s*=>\s*'([^']+)'`)
	matches := groupRe.FindAllStringSubmatch(groupsStr, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			tag.Groups[match[1]] = match[2]
		}
	}
}

// parseValueMappings extracts PrintConv value mappings
func parseValueMappings(content string, tag *TagDef) {
	// Handle BITMASK
	if strings.Contains(content, "BITMASK =>") {
		// Extract bitmask values
		bitmaskRe := regexp.MustCompile(`(\d+)\s*=>\s*'([^']+)'`)
		matches := bitmaskRe.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				tag.Values["bit"+match[1]] = match[2]
			}
		}
		return
	}

	// Numeric values: 0 => 'None', 1 => 'Standard'
	numRe := regexp.MustCompile(`(\d+)\s*=>\s*'([^']+)'`)
	for _, match := range numRe.FindAllStringSubmatch(content, -1) {
		if len(match) >= 3 {
			tag.Values[match[1]] = match[2]
		}
	}

	// Hex values: 0x01 => 'Mode 1'
	hexRe := regexp.MustCompile(`0x([0-9a-fA-F]+)\s*=>\s*'([^']+)'`)
	for _, match := range hexRe.FindAllStringSubmatch(content, -1) {
		if len(match) >= 3 {
			tag.Values["0x"+strings.ToUpper(match[1])] = match[2]
		}
	}

	// String values: 'A' => 'Auto'
	strRe := regexp.MustCompile(`'([^']+)'\s*=>\s*'([^']+)'`)
	for _, match := range strRe.FindAllStringSubmatch(content, -1) {
		if len(match) >= 3 {
			tag.Values[match[1]] = match[2]
		}
	}
}

// GenerateGoFiles generates Go source files from parsed data
func GenerateGoFiles(data *ParsedData, outputDir string) error {
	// Generate a file for each tag table
	for tableName, table := range data.TagTables {
		if err := generateTagFile(tableName, table, outputDir); err != nil {
			return fmt.Errorf("error generating file for %s: %w", tableName, err)
		}
	}

	// Generate format mappings file
	if err := generateFormatsFile(data, outputDir); err != nil {
		return fmt.Errorf("error generating formats file: %w", err)
	}

	// Generate init file to register all tables
	if err := generateInitFile(data, outputDir); err != nil {
		return fmt.Errorf("error generating init file: %w", err)
	}

	return nil
}

// generateTagFile generates a Go file for a tag table
func generateTagFile(tableName string, table *TagTable, outputDir string) error {
	// Create safe filename from table name
	// e.g., "JPEG::Main" -> "jpeg_main.go"
	filename := strings.ToLower(strings.ReplaceAll(tableName, "::", "_")) + ".go"
	filepath := filepath.Join(outputDir, filename)

	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create variable name that matches the unique table name
	// This ensures no conflicts when we have similar table names
	varName := generateVarName(tableName)

	fmt.Fprintf(file, "// Code generated by gen-tags. DO NOT EDIT.\n\n")
	fmt.Fprintf(file, "package tags\n\n")

	fmt.Fprintf(file, "// %s contains tag definitions from %s\n", varName, table.PackageName)
	fmt.Fprintf(file, "var %s = TagTable{\n", varName)
	fmt.Fprintf(file, "\tModuleName: %q,\n", table.ModuleName)
	fmt.Fprintf(file, "\tTags: map[string]TagDef{\n")

	// Write tag definitions
	for id, tag := range table.Tags {
		fmt.Fprintf(file, "\t\t%q: {\n", id)
		fmt.Fprintf(file, "\t\t\tID:          %q,\n", tag.ID)

		if tag.Name != "" {
			fmt.Fprintf(file, "\t\t\tName:        %q,\n", tag.Name)
		}
		if tag.Description != "" {
			fmt.Fprintf(file, "\t\t\tDescription: %q,\n", tag.Description)
		}
		if tag.Format != "" {
			fmt.Fprintf(file, "\t\t\tFormat:      %q,\n", tag.Format)
		}
		if tag.SubIFD != "" {
			fmt.Fprintf(file, "\t\t\tSubIFD:      %q,\n", tag.SubIFD)
		}

		// Write groups if any
		if len(tag.Groups) > 0 {
			fmt.Fprintf(file, "\t\t\tGroups: map[string]string{\n")
			for k, v := range tag.Groups {
				fmt.Fprintf(file, "\t\t\t\t%q: %q,\n", k, v)
			}
			fmt.Fprintf(file, "\t\t\t},\n")
		}

		// Write value mappings if any
		if len(tag.Values) > 0 {
			fmt.Fprintf(file, "\t\t\tValues: map[string]string{\n")
			for k, v := range tag.Values {
				fmt.Fprintf(file, "\t\t\t\t%q: %q,\n", k, v)
			}
			fmt.Fprintf(file, "\t\t\t},\n")
		}

		fmt.Fprintf(file, "\t\t},\n")
	}

	fmt.Fprintf(file, "\t},\n")
	fmt.Fprintf(file, "}\n")

	return nil
}

// generateFormatsFile generates the format mappings file
func generateFormatsFile(data *ParsedData, outputDir string) error {
	filepath := filepath.Join(outputDir, "formats.go")

	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintf(file, "// Code generated by gen-tags. DO NOT EDIT.\n\n")
	fmt.Fprintf(file, "package tags\n\n")

	// File extension mappings
	fmt.Fprintf(file, "// FileExtensions maps file extensions to module names\n")
	fmt.Fprintf(file, "var FileExtensions = map[string]string{\n")
	for ext, module := range data.FileTypes {
		fmt.Fprintf(file, "\t%q: %q,\n", ext, module)
	}
	fmt.Fprintf(file, "}\n\n")

	// MIME type mappings
	fmt.Fprintf(file, "// MIMETypes maps MIME types to module names\n")
	fmt.Fprintf(file, "var MIMETypes = map[string]string{\n")
	for mime, module := range data.MIMETypes {
		fmt.Fprintf(file, "\t%q: %q,\n", mime, module)
	}
	fmt.Fprintf(file, "}\n")

	return nil
}

// generateInitFile generates the init.go file
func generateInitFile(data *ParsedData, outputDir string) error {
	filepath := filepath.Join(outputDir, "init.go")

	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintf(file, "// Code generated by gen-tags. DO NOT EDIT.\n\n")
	fmt.Fprintf(file, "package tags\n\n")

	fmt.Fprintf(file, "// AllTags contains all loaded tag tables\n")
	fmt.Fprintf(file, "var AllTags = map[string]*TagTable{\n")

	for tableName := range data.TagTables {
		varName := generateVarName(tableName)
		fmt.Fprintf(file, "\t%q: &%s,\n", tableName, varName)
	}

	fmt.Fprintf(file, "}\n")

	return nil
}

// generateVarName generates a unique variable name from a table name
func generateVarName(tableName string) string {
	// To ensure uniqueness, we keep underscores between major parts
	// NikonCustom::SettingsD500 -> NikonCustom_SettingsD500_Tags
	// Nikon::CustomSettingsD500 -> Nikon_CustomSettingsD500_Tags

	// Replace :: with _
	safeName := strings.ReplaceAll(tableName, "::", "_")

	// Convert each part to have initial capital
	parts := strings.Split(safeName, "_")
	result := []string{}
	for _, part := range parts {
		if len(part) > 0 {
			// Capitalize first letter
			capitalized := strings.ToUpper(part[:1])
			if len(part) > 1 {
				capitalized += part[1:]
			}
			result = append(result, capitalized)
		}
	}

	// Join with underscores and add Tags suffix
	return strings.Join(result, "_") + "_Tags"
}
