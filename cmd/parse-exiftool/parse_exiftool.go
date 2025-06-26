package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type FileTypeData struct {
	Extensions   map[string][]string // extension -> [type, description]
	MagicNumbers map[string]string   // type -> magic pattern
	TestOrder    []string            // order to test files
	ModuleNames  map[string]string   // type -> module name
	MimeTypes    map[string]string   // type -> mime type
}

func parseExifTool(exifToolPath string) (*FileTypeData, error) {
	content, err := os.ReadFile(exifToolPath)
	if err != nil {
		return nil, err
	}

	data := &FileTypeData{
		Extensions:   make(map[string][]string),
		MagicNumbers: make(map[string]string),
		ModuleNames:  make(map[string]string),
		MimeTypes:    make(map[string]string),
	}

	text := string(content)

	// Parse %fileTypeLookup
	data.Extensions = parseFileTypeLookup(text)

	// Parse %magicNumber
	data.MagicNumbers = parseMagicNumbers(text)

	// Parse @fileTypes
	data.TestOrder = parseFileTypes(text)

	// Parse %moduleName
	data.ModuleNames = parseModuleNames(text)

	// Parse %mimeType
	data.MimeTypes = parseMimeTypes(text)

	return data, nil
}

func parseFileTypeLookup(content string) map[string][]string {
	result := make(map[string][]string)

	start := strings.Index(content, "%fileTypeLookup = (")
	if start == -1 {
		fmt.Println("Warning: Could not find %fileTypeLookup")
		return result
	}

	// Find the closing );
	end := start + len("%fileTypeLookup = (")
	openParen := 1

	for i := end; i < len(content); i++ {
		switch content[i] {
		case '(':
			openParen++
		case ')':
			openParen--
			if openParen == 0 {
				end = i
				break
			}
		}
		if openParen == 0 {
			break
		}
	}

	section := content[start:end]

	// Debug output for DOCX
	if strings.Contains(section, "DOCX =>") {
		docxStart := strings.Index(section, "DOCX =>")
		docxEnd := strings.Index(section[docxStart:], ",\n")
		if docxEnd > 0 {
			docxLine := section[docxStart : docxStart+docxEnd]
			fmt.Printf("DEBUG: DOCX line: %q\n", docxLine)
		}
	}

	// Split into lines for easier parsing
	lines := strings.Split(section, "\n")

	for _, line := range lines {
		// Skip empty lines and the opening line
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%fileTypeLookup") {
			continue
		}

		// Match entries - be more flexible with the ending
		re := regexp.MustCompile(`^(\w+)\s*=>\s*(.+?)(?:,\s*)?$`)
		match := re.FindStringSubmatch(line)

		if len(match) >= 3 {
			ext := match[1]
			value := strings.TrimSpace(match[2])

			// Remove trailing comma if present
			value = strings.TrimSuffix(value, ",")

			if strings.HasPrefix(value, "[[") {
				// Complex nested array format: [['ZIP','FPX'], 'Office Open XML Document']
				// Debug output
				if ext == "DOCX" {
					fmt.Printf("DEBUG: Processing DOCX value: %q\n", value)
				}

				// Find the end of the first array
				firstArrayEnd := strings.Index(value, "]")
				if firstArrayEnd > 0 {
					// Get the types array
					typesStr := value[2:firstArrayEnd] // Skip [[ to get 'ZIP','FPX'

					// Extract first type
					typeRe := regexp.MustCompile(`'([^']+)'`)
					typeMatches := typeRe.FindStringSubmatch(typesStr)
					fileType := ""
					if len(typeMatches) > 1 {
						fileType = typeMatches[1]
					}

					// Find description - it should be after ],
					remainingStr := value[firstArrayEnd+1:]
					// Remove the closing bracket of the nested array
					if strings.HasPrefix(remainingStr, "]") {
						remainingStr = remainingStr[1:]
					}
					// Skip comma and whitespace
					remainingStr = strings.TrimLeft(remainingStr, ", ")

					// Extract description
					desc := ""
					if strings.HasPrefix(remainingStr, "'") || strings.HasPrefix(remainingStr, "\"") {
						// Find the matching quote
						quote := remainingStr[0:1]
						endQuote := strings.Index(remainingStr[1:], quote)
						if endQuote > 0 {
							desc = remainingStr[1 : endQuote+1]
						}
					}

					if ext == "DOCX" {
						fmt.Printf("DEBUG: DOCX parsed - Type: %q, Desc: %q\n", fileType, desc)
					}

					result[ext] = []string{fileType, desc}
				}
			} else if strings.HasPrefix(value, "[") && !strings.HasPrefix(value, "[[") {
				// Simple array format: ['MOV', 'description']
				// Remove the brackets
				value = strings.Trim(value, "[]")

				// Split by comma, respecting quotes
				parts := splitRespectingQuotes(value, ',')

				fileType := ""
				desc := ""

				if len(parts) >= 1 {
					fileType = strings.Trim(parts[0], " '\"")
				}
				if len(parts) >= 2 {
					desc = strings.Trim(parts[1], " '\"")
				}

				result[ext] = []string{fileType, desc}
			} else {
				// Simple reference: 'JPEG'
				fileType := strings.Trim(value, "'\"")
				result[ext] = []string{fileType, ""}
			}
		}
	}

	fmt.Printf("Found %d extensions\n", len(result))

	// Debug: Show some parsed values
	if docx, ok := result["DOCX"]; ok {
		fmt.Printf("DOCX parsed as: Type=%q, Desc=%q\n", docx[0], docx[1])
	}

	return result
}

// splitRespectingQuotes splits a string by delimiter, but respects quoted sections
func splitRespectingQuotes(s string, delim rune) []string {
	var result []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range s {
		switch {
		case !inQuote && (r == '\'' || r == '"'):
			inQuote = true
			quoteChar = r
			current.WriteRune(r)
		case inQuote && r == quoteChar:
			inQuote = false
			current.WriteRune(r)
		case !inQuote && r == delim:
			result = append(result, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

func parseMagicNumbers(content string) map[string]string {
	result := make(map[string]string)

	start := strings.Index(content, "%magicNumber = (")
	if start == -1 {
		fmt.Println("Warning: Could not find %magicNumber")
		return result
	}

	// Find the closing );
	depth := 0
	end := start
	for i := start; i < len(content); i++ {
		if content[i] == '(' {
			depth++
		} else if content[i] == ')' {
			depth--
			if depth == 0 && i > start && i+1 < len(content) && content[i+1] == ';' {
				end = i + 2
				break
			}
		}
	}

	section := content[start:end]

	// Parse line by line
	lines := strings.Split(section, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=>") {
			continue
		}

		// Match patterns with magic bytes
		re := regexp.MustCompile(`^\s*(\w+)\s*=>\s*'([^']+)'`)
		match := re.FindStringSubmatch(line)
		if len(match) >= 3 {
			// Only include if it looks like a magic number pattern
			pattern := match[2]
			if strings.Contains(pattern, "\\x") || strings.Contains(pattern, "\\0") ||
				len(pattern) <= 10 || pattern == "RawConv" {
				result[match[1]] = pattern
			}
		}
	}

	fmt.Printf("Found %d magic numbers\n", len(result))
	return result
}

func parseFileTypes(content string) []string {
	var result []string

	start := strings.Index(content, "@fileTypes = qw(")
	if start == -1 {
		fmt.Println("Warning: Could not find @fileTypes")
		return result
	}

	end := strings.Index(content[start:], ");")
	if end == -1 {
		return result
	}

	section := content[start : start+end]

	// Remove the "@fileTypes = qw(" part
	section = strings.TrimPrefix(section, "@fileTypes = qw(")

	// Split by whitespace and filter
	parts := strings.Fields(section)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !strings.Contains(part, "=") {
			result = append(result, part)
		}
	}

	fmt.Printf("Found %d file types in test order\n", len(result))
	return result
}

func parseModuleNames(content string) map[string]string {
	result := make(map[string]string)

	// Look for "my %moduleName"
	start := strings.Index(content, "my %moduleName = (")
	if start == -1 {
		// Try without "my"
		start = strings.Index(content, "%moduleName = (")
		if start == -1 {
			fmt.Println("Warning: Could not find %moduleName")
			return result
		}
	}

	// Find the closing );
	depth := 0
	end := start
	for i := start; i < len(content); i++ {
		if content[i] == '(' {
			depth++
		} else if content[i] == ')' {
			depth--
			if depth == 0 && i > start && i+1 < len(content) && content[i+1] == ';' {
				end = i + 2
				break
			}
		}
	}

	section := content[start:end]

	// Parse entries - they may use different quote styles
	re := regexp.MustCompile(`(?m)^\s*(\w+)\s*=>\s*(?:'([^']*)'|"([^"]*)"|\b(\w+)\b)`)
	matches := re.FindAllStringSubmatch(section, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			key := match[1]
			value := ""
			// Check which capture group matched
			for i := 2; i < len(match); i++ {
				if match[i] != "" {
					value = match[i]
					break
				}
			}
			if value != "0" && value != "" { // Skip '0' values
				result[key] = value
			}
		}
	}

	fmt.Printf("Found %d module names\n", len(result))
	return result
}

func parseMimeTypes(content string) map[string]string {
	result := make(map[string]string)

	// Look for %mimeType hash
	start := strings.Index(content, "%mimeType = (")
	if start == -1 {
		fmt.Println("Warning: Could not find %mimeType")
		return result
	}

	// Find the closing );
	depth := 0
	end := start
	for i := start; i < len(content); i++ {
		if content[i] == '(' {
			depth++
		} else if content[i] == ')' {
			depth--
			if depth == 0 && i > start && i+1 < len(content) && content[i+1] == ';' {
				end = i + 2
				break
			}
		}
	}

	section := content[start:end]

	// Parse entries like: DOCX => 'application/vnd...',
	lines := strings.Split(section, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=>") {
			continue
		}

		re := regexp.MustCompile(`^\s*(\w+)\s*=>\s*'([^']+)'`)
		match := re.FindStringSubmatch(line)
		if len(match) >= 3 {
			result[match[1]] = match[2]
		}
	}

	fmt.Printf("Found %d MIME types\n", len(result))
	return result
}

func generateGoFile(data *FileTypeData, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)

	// Write header
	fmt.Fprintln(w, "// Code generated by parse_exiftool.go; DO NOT EDIT.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "package tags")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "// ExifToolFileTypes contains file identification data extracted from ExifTool")
	fmt.Fprintln(w, "var ExifToolFileTypes = struct {")
	fmt.Fprintln(w, "    Extensions   map[string]FileTypeInfo")
	fmt.Fprintln(w, "    MagicNumbers map[string]string")
	fmt.Fprintln(w, "    TestOrder    []string")
	fmt.Fprintln(w, "    ModuleNames  map[string]string")
	fmt.Fprintln(w, "    MimeTypes    map[string]string")
	fmt.Fprintln(w, "}{")

	// Write Extensions
	fmt.Fprintln(w, "    Extensions: map[string]FileTypeInfo{")
	for ext, info := range data.Extensions {
		desc := ""
		if len(info) > 1 {
			desc = info[1]
		}
		fmt.Fprintf(w, "        %q: {Type: %q, Description: %q},\n", ext, info[0], desc)
	}
	fmt.Fprintln(w, "    },")

	// Write MagicNumbers - convert Perl regex to Go-friendly format
	fmt.Fprintln(w, "    MagicNumbers: map[string]string{")
	for fileType, pattern := range data.MagicNumbers {
		// Keep the original pattern for now - will need conversion logic
		fmt.Fprintf(w, "        %q: %q,\n", fileType, pattern)
	}
	fmt.Fprintln(w, "    },")

	// Write TestOrder
	fmt.Fprintln(w, "    TestOrder: []string{")
	for _, fileType := range data.TestOrder {
		fmt.Fprintf(w, "        %q,\n", fileType)
	}
	fmt.Fprintln(w, "    },")

	// Write ModuleNames
	fmt.Fprintln(w, "    ModuleNames: map[string]string{")
	for fileType, module := range data.ModuleNames {
		fmt.Fprintf(w, "        %q: %q,\n", fileType, module)
	}
	fmt.Fprintln(w, "    },")

	// Write MimeTypes
	fmt.Fprintln(w, "    MimeTypes: map[string]string{")
	for fileType, mime := range data.MimeTypes {
		fmt.Fprintf(w, "        %q: %q,\n", fileType, mime)
	}
	fmt.Fprintln(w, "    },")

	fmt.Fprintln(w, "}")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "type FileTypeInfo struct {")
	fmt.Fprintln(w, "    Type        string")
	fmt.Fprintln(w, "    Description string")
	fmt.Fprintln(w, "}")

	return w.Flush()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: parse_exiftool <path-to-exiftool>")
		os.Exit(1)
	}

	data, err := parseExifTool(os.Args[1])
	if err != nil {
		fmt.Printf("Error parsing ExifTool: %v\n", err)
		os.Exit(1)
	}

	// Show what we parsed for DOCX
	if docx, ok := data.Extensions["DOCX"]; ok {
		fmt.Printf("\nFinal DOCX data: Type=%q, Description=%q\n", docx[0], docx[1])
	}
	if mime, ok := data.MimeTypes["DOCX"]; ok {
		fmt.Printf("DOCX MIME type: %q\n", mime)
	}
	if module, ok := data.ModuleNames["DOCX"]; ok {
		fmt.Printf("DOCX Module: %q\n", module)
	}

	err = generateGoFile(data, "tags/exiftool_identify.go")
	if err != nil {
		fmt.Printf("Error generating Go file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nGenerated tags/exiftool_identify.go")
}
