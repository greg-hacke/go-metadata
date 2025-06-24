package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"greg-hacke/go-metadata/parser"
)

func main() {
	// Define command line flags
	var outputDir string
	flag.StringVar(&outputDir, "o", "tags", "Output directory for generated Go files")
	flag.Parse()

	// Check arguments
	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [-o output_dir] <exiftool_pm_dir>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  %s /opt/homebrew/Cellar/exiftool/13.25/libexec/lib/perl5/Image/ExifTool\n", os.Args[0])
		os.Exit(1)
	}

	// Get ExifTool PM directory path
	pmDir := flag.Arg(0)

	// Verify the directory exists
	if info, err := os.Stat(pmDir); os.IsNotExist(err) {
		log.Fatalf("Error: directory %s does not exist", pmDir)
	} else if err != nil {
		log.Fatalf("Error accessing directory %s: %v", pmDir, err)
	} else if !info.IsDir() {
		log.Fatalf("Error: %s is not a directory", pmDir)
	}

	// Convert to absolute paths
	absPMDir, err := filepath.Abs(pmDir)
	if err != nil {
		log.Fatalf("Error resolving PM directory path: %v", err)
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		log.Fatalf("Error resolving output directory path: %v", err)
	}

	// Print what we're doing
	fmt.Printf("Parsing ExifTool PM files from: %s\n", absPMDir)
	fmt.Printf("Output directory: %s\n", absOutputDir)

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(absOutputDir, 0755); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	// Parse all PM files recursively
	fmt.Println("\nParsing PM files...")
	parsedData, err := parser.ParsePMFiles(absPMDir)
	if err != nil {
		log.Fatalf("Error parsing PM files: %v", err)
	}

	// Generate Go files from parsed data
	fmt.Println("\nGenerating Go files...")
	err = parser.GenerateGoFiles(parsedData, absOutputDir)
	if err != nil {
		log.Fatalf("Error generating Go files: %v", err)
	}

	fmt.Println("\nDone! Tag files have been generated.")
}
