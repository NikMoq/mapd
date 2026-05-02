#!/bin/bash
# Tile validation script — run before creating a release
# Usage: ./validate-tiles.sh <tiles-directory>

set -e

TILES_DIR="${1:-./tiles}"
MANIFEST="$TILES_DIR/manifest.json"
ERRORS=0

echo "=========================================="
echo "  Tile Validation"
echo "=========================================="
echo "Directory: $TILES_DIR"

# 1. Check manifest exists
if [ ! -f "$MANIFEST" ]; then
    echo "❌ manifest.json not found"
    exit 1
fi
echo "✅ manifest.json exists"

# 2. Check manifest structure
if ! jq -e '.version and .generation and .tiles' "$MANIFEST" >/dev/null 2>&1; then
    echo "❌ manifest.json missing required fields"
    exit 1
fi
echo "✅ manifest.json structure valid"

# 3. Check all tiles exist
TOTAL=$(jq '.tiles | length' "$MANIFEST")
FOUND=0
MISSING=0

while IFS= read -r tile_path; do
    tile_file="$TILES_DIR/$tile_path"
    if [ -f "$tile_file" ]; then
        FOUND=$((FOUND + 1))
    else
        echo "❌ Missing tile: $tile_path"
        MISSING=$((MISSING + 1))
        ERRORS=1
    fi
done < <(jq -r '.tiles | keys[]' "$MANIFEST")

echo "✅ Tiles found: $FOUND / $TOTAL (missing: $MISSING)"

# 4. Check tile sizes
MAX_SIZE=65536  # 64KB per tile
OVERSIZED=0

while IFS= read -r tile_path; do
    tile_file="$TILES_DIR/$tile_path"
    if [ -f "$tile_file" ]; then
        SIZE=$(stat -c%s "$tile_file")
        if [ "$SIZE" -gt "$MAX_SIZE" ]; then
            echo "⚠️ Oversized tile: $tile_path ($SIZE bytes > $MAX_SIZE)"
            OVERSIZED=$((OVERSIZED + 1))
        fi
    fi
done < <(jq -r '.tiles | keys[]' "$MANIFEST")

if [ "$OVERSIZED" -gt 0 ]; then
    echo "⚠️ Oversized tiles: $OVERSIZED (should be <= 64KB)"
else
    echo "✅ All tiles under 64KB"
fi

# 5. Check SHA256 hashes
MISMATCH=0

while IFS= read -r tile_path; do
    tile_file="$TILES_DIR/$tile_path"
    if [ -f "$tile_file" ]; then
        EXPECTED_HASH=$(jq -r ".tiles[\"$tile_path\"].hash" "$MANIFEST")
        ACTUAL_HASH=$(sha256sum "$tile_file" | cut -d' ' -f1)
        if [ "$EXPECTED_HASH" != "$ACTUAL_HASH" ]; then
            echo "❌ Hash mismatch: $tile_path"
            MISMATCH=$((MISMATCH + 1))
            ERRORS=1
        fi
    fi
done < <(jq -r '.tiles | keys[]' "$MANIFEST")

if [ "$MISMATCH" -eq 0 ]; then
    echo "✅ All hashes valid"
else
    echo "❌ Hash mismatches: $MISMATCH"
fi

# 6. Summary
echo ""
echo "=========================================="
if [ "$ERRORS" -eq 0 ]; then
    echo "  ✅ VALIDATION PASSED"
else
    echo "  ❌ VALIDATION FAILED"
fi
echo "=========================================="
exit "$ERRORS"
