.PHONY: build gen-tags test clean

# Default ExifTool PM directory (adjust as needed)
EXIFTOOL_PM_DIR ?= /opt/homebrew/Cellar/exiftool/13.25/libexec/lib/perl5/Image/ExifTool

build:
	go build -o bin/gen-tags ./cmd/gen-tags
	go build -o bin/meta-test ./cmd/meta-test

gen-tags: build
	./bin/gen-tags -o tags $(EXIFTOOL_PM_DIR)

test: build
	./bin/meta-test testdata/example.jpg

clean:
	rm -rf bin/
	rm -f tags/*.go

install:
	go install ./cmd/gen-tags
	go install ./cmd/meta-test