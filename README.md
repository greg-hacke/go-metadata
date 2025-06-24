# go-metadata

**A Go-native implementation of ExifTool-style metadata extraction**

---

## 🎯 Objective

`go-metadata` is a pure Go library for extracting metadata from a wide range of file types including images, audio, video, documents, and archives. It replicates the core functionality of ExifTool, but is written entirely in Go — no external binaries, no CGO, and suitable for embedding into Go applications, CLI tools, or services.

---

## ⚙️ How It Works

### 1. Parsing ExifTool `.pm` Tag Definitions

- ExifTool maintains tag metadata definitions in Perl `.pm` files.
- We provide a CLI tool, `cmd/gen-tags`, that:
  - Parses these `.pm` files
  - Extracts structured tag maps
  - Outputs Go-compatible source files under the `tags/` directory
- These Go tag files are then used across the codebase for field names, formats, decoding rules, and hierarchies.

### 2. Format-Specific Parsers

- Located in `formats/`, each file format (e.g., EXIF, ID3, PDF, MP4) has its own parser.
- Each parser:
  - Implements a sniffer to identify compatible files
  - Extracts relevant metadata from the binary structure
  - References tag definitions from the `tags/` package

### 3. Public API for Metadata Extraction

- The `meta` package exposes a clean interface:
  
  ```go
  fields, err := meta.ReadMetadata("example.jpg")
  for _, f := range fields {
      fmt.Printf("[%s] %s = %v\n", f.Namespace, f.Key, f.Value)
  }
  ```

- This allows your Go application to easily extract metadata without having to understand file internals.

---

## 🔁 Updating to New ExifTool Releases

If ExifTool publishes an updated set of `.pm` tag files:

1. Replace or update the source `.pm` files in your project
2. Run `cmd/gen-tags` again to regenerate the Go tag definitions
3. Recompile your project — your app now uses the updated tag definitions automatically

This makes `go-metadata` resilient and extensible to future format and tag changes.

---

## 🧱 Project Structure Overview

```
go-metadata
├── cmd/               # CLI tools: tag generation and testing
│   ├── gen-tags       # Parses .pm files → Go tag definitions
│   └── meta-test      # Runs metadata extraction on test files
├── formats/           # Format-specific parsers and sniffers
├── internal/          # Internal utility packages
├── meta/              # Public metadata reader API
├── parser/            # .pm file parser logic
├── tags/              # Auto-generated tag maps (do not edit)
├── testdata/          # Example files and expected output
├── LICENSE
├── go.mod
└── README.md
```

---

## ✅ Key Features

- Pure Go (no dependencies on ExifTool binaries or CGO)
- Supports EXIF, ID3, PDF, PNG, MP4, Office formats, and more
- Easily embeddable in CLI, server, and desktop applications
- Auto-regeneratable tag maps from upstream ExifTool `.pm` files

---

## 🧪 Get Started

```bash
go install github.com/greg-hacke/go-metadata/cmd/gen-tags@latest
go install github.com/greg-hacke/go-metadata/cmd/meta-test@latest

# Regenerate tags
gen-tags /path/to/exiftool/<version>/libexec/lib/perl5/Image/ExifTool

# Test metadata extraction
meta-test ./testdata/example.jpg
```

---

## 📄 License

This project is licensed under the GNU GPL v2.0 License.