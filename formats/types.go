// formats/types.go
package formats

import (
	"io"

	"greg-hacke/go-metadata/meta"
)

// Format represents a file format
type Format string

const (
	FormatJPEG    Format = "JPEG"
	FormatPNG     Format = "PNG"
	FormatMP3     Format = "MP3"
	FormatMP4     Format = "MP4"
	FormatPDF     Format = "PDF"
	FormatUnknown Format = "Unknown"
)

// Parser is the interface for format-specific metadata parsers
type Parser interface {
	Parse(r io.ReadSeeker) ([]meta.Field, error)
}

// parsers holds registered parsers
var parsers = make(map[Format]Parser)

// RegisterParser registers a parser for a format
func RegisterParser(format Format, parser Parser) {
	parsers[format] = parser
}

// GetParser returns the parser for a format
func GetParser(format Format) Parser {
	return parsers[format]
}
