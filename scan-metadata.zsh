#!/usr/bin/env zsh

if [[ $# -eq 0 ]]; then
    echo "Usage: $0 <directory>"
    exit 1
fi

META_EXTRACT="./bin/meta-extract"

# Header
printf "%-60s %-10s %-15s %-20s %s\n" "File Path" "Extension" "Format" "Module" "Description"
printf "%s\n" "$(printf '=%.0s' {1..150})"

find "$1" -type f -not -path '*/.*' -print0 | while IFS= read -r -d '' file; do
    output=$("$META_EXTRACT" "$file" 2>&1)
    
    # Parse output line by line
    while IFS= read -r line; do
        case "$line" in
            "File Path:"*) filepath="${line#File Path:*[[:space:]]}" ;;
            "Extension:"*) extension="${line#Extension:*[[:space:]]}" ;;
            "Format:"*) format="${line#Format:*[[:space:]]}" ;;
            "Module:"*) module="${line#Module:*[[:space:]]}" ;;
            "Description:"*) description="${line#Description:*[[:space:]]}" ;;
        esac
    done <<< "$output"
    
    if [[ -n "$format" ]]; then
        # Truncate path if too long
        display_path="$filepath"
        if [[ ${#display_path} -gt 57 ]]; then
            display_path="...${display_path: -54}"
        fi
        
        printf "%-60s %-10s %-15s %-20s %s\n" \
            "$display_path" \
            "$extension" \
            "$format" \
            "$module" \
            "$description"
    fi
done