package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"greg-hacke/go-metadata/meta"
)

func main() {
	// Check command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <path/to/file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s image.jpg\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s /path/to/document.pdf\n", os.Args[0])
		os.Exit(1)
	}

	filePath := os.Args[1]

	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Fatalf("Error accessing file: %v", err)
	}

	// Get absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Fatalf("Error getting absolute path: %v", err)
	}

	// Print file system information
	fmt.Println("=== File System Information ===")
	fmt.Printf("File Name:     %s\n", fileInfo.Name())
	fmt.Printf("File Path:     %s\n", absPath)
	fmt.Printf("File Size:     %d bytes\n", fileInfo.Size())
	fmt.Printf("Permissions:   %s\n", fileInfo.Mode())
	fmt.Printf("Modified Time: %s\n", fileInfo.ModTime().Format(time.RFC3339))
	fmt.Printf("Is Directory:  %v\n", fileInfo.IsDir())

	// If it's a directory, exit
	if fileInfo.IsDir() {
		fmt.Println("\nError: Path points to a directory, not a file")
		os.Exit(1)
	}

	// Add file extension info
	ext := filepath.Ext(filePath)
	if ext != "" {
		fmt.Printf("Extension:     %s\n", ext)
	}

	// Process the file for metadata
	fmt.Println("\n=== Processing Metadata ===")

	// For now, pass nil for requested metadata (get all)
	err = meta.ProcessFileByPath(filePath, nil)
	if err != nil {
		log.Fatalf("Error processing file: %v", err)
	}

	fmt.Println("\n=== Processing Complete ===")
}
