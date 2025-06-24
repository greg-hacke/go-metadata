package parser

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ParsePMFiles recursively parses all .pm files in the given directory
func ParsePMFiles(rootDir string) (*ParsedData, error) {
	data := &ParsedData{
		TagTables: make(map[string]*TagTable),
		FileTypes: make(map[string]string),
		MIMETypes: make(map[string]string),
	}

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Only process .pm files
		if !strings.HasSuffix(path, ".pm") || d.IsDir() {
			return nil
		}

		// Parse the PM file
		if err := parsePMFile(path, data); err != nil {
			// Log error but continue
			fmt.Fprintf(os.Stderr, "Warning: error parsing %s: %v\n", path, err)
		}

		return nil
	})

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
	tagDefStartRe := regexp.MustCompile(`^\s*(?:'([^']+)'|"([^"]+)"|0x([0-9a-fA-F]+)|(\w+))\s*=>\s*[\[\{]?\s*$`)
	tagDefInlineRe := regexp.MustCompile(`^\s*(?:'([^']+)'|"([^"]+)"|0x([0-9a-fA-F]+)|(\w+))\s*=>\s*(.+?),?\s*$`)
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

		// Track bracket/paren depth
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

		// Look for package declaration
		if matches := packageRe.FindStringSubmatch(line); matches != nil {
			packageName = matches[1]
			continue
		}

		// Look for tag table start
		if matches := tagTableRe.FindStringSubmatch(line); matches != nil {
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
			data.TagTables[fullName] = currentTable

			inTagTable = true
			continue
		}

		if !inTagTable {
			continue
		}

		// Check for table end
		if parenDepth == 0 && strings.Contains(line, ");") {
			inTagTable = false
			currentTable = nil
			continue
		}

		// Skip table metadata like NOTES, GROUPS, etc. at table level
		if bracketDepth == 0 && (strings.Contains(line, "NOTES =>") ||
			strings.Contains(line, "GROUPS =>") ||
			strings.Contains(line, "PROCESS_PROC =>") ||
			strings.Contains(line, "VARS =>") ||
			strings.Contains(line, "FIRST_ENTRY =>") ||
			strings.Contains(line, "TAG_PREFIX =>")) {
			continue
		}

		// Look for tag definition start (multiline)
		if !inTagDef && bracketDepth == 0 {
			if matches := tagDefStartRe.FindStringSubmatch(line); matches != nil {
				// Save previous tag
				if currentTag != nil && currentKey != "" {
					currentTable.Tags[currentKey] = currentTag
				}

				// Extract key from matches
				currentKey = extractTagKey(matches)
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
				currentKey = extractTagKey(matches[:5]) // first 4 groups are the key patterns
				value := matches[5]

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
			if bracketDepth == 0 && (strings.Contains(line, "},") || strings.Contains(line, "}]")) {
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

	// Extract file type associations from the module
	extractFileTypes(moduleName, data)

	return scanner.Err()
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

// extractFileTypes extracts file type associations based on module name
func extractFileTypes(moduleName string, data *ParsedData) {
	// Common mappings based on module names
	switch moduleName {
	case "JPEG":
		data.FileTypes[".jpg"] = moduleName
		data.FileTypes[".jpeg"] = moduleName
		data.FileTypes[".jpe"] = moduleName
		data.MIMETypes["image/jpeg"] = moduleName
	case "PNG":
		data.FileTypes[".png"] = moduleName
		data.MIMETypes["image/png"] = moduleName
	case "TIFF":
		data.FileTypes[".tif"] = moduleName
		data.FileTypes[".tiff"] = moduleName
		data.MIMETypes["image/tiff"] = moduleName
	case "GIF":
		data.FileTypes[".gif"] = moduleName
		data.MIMETypes["image/gif"] = moduleName
	case "BMP":
		data.FileTypes[".bmp"] = moduleName
		data.MIMETypes["image/bmp"] = moduleName
	case "PDF":
		data.FileTypes[".pdf"] = moduleName
		data.MIMETypes["application/pdf"] = moduleName
	case "MP3", "ID3":
		data.FileTypes[".mp3"] = moduleName
		data.MIMETypes["audio/mpeg"] = moduleName
	case "MP4", "MOV", "QuickTime":
		data.FileTypes[".mp4"] = moduleName
		data.FileTypes[".m4v"] = moduleName
		data.FileTypes[".m4a"] = moduleName
		data.FileTypes[".mov"] = moduleName
		data.MIMETypes["video/mp4"] = moduleName
		data.MIMETypes["video/quicktime"] = moduleName
		// Add more as needed
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

	// Create safe variable name
	// e.g., "JPEG::Main" -> "JPEGMain"
	varName := strings.ReplaceAll(tableName, "::", "") + "Tags"

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
		varName := strings.ReplaceAll(tableName, "::", "") + "Tags"
		fmt.Fprintf(file, "\t%q: &%s,\n", tableName, varName)
	}

	fmt.Fprintf(file, "}\n")

	return nil
}
