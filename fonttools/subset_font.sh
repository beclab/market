#!/bin/bash

# Script parameter check
if [ "$#" -ne 4 ]; then
    echo "Usage: $0 <input_font_file> <text_file> <output_font_file>"
    exit 1
fi

# Get script parameters
INPUT_FONT_FILE="$1"
TEXT_FILE="$2"
OUTPUT_FONT_FILE="$3"
OUTPUT_FONT_PATH="$4"

# Ensure pyftsubset is available
if ! command -v pyftsubset &> /dev/null; then
    echo "pyftsubset not found. Please install fontTools first."
    exit 1
fi

# Crop fonts using pyftsubset
pyftsubset "$INPUT_FONT_FILE" --unicodes-file="$TEXT_FILE" --output-file="$OUTPUT_FONT_FILE" --no-layout-closure --flavor=woff2

# Check if the output file was successfully generated
if [ -f "$OUTPUT_FONT_FILE" ]; then
    echo "Font subsetted successfully to $OUTPUT_FONT_FILE"
else
    echo "Failed to subset font."
    exit 1
fi

# Copy files to the specified directory
cp $OUTPUT_FONT_FILE "$OUTPUT_FONT_PATH"

# Script ends
exit 0
