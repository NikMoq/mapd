// +build ignore

// Example CLI integration for NikMoq/mapd fork.
// Add this as cmd/generate.go in the fork, or integrate into existing CLI.

package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"pfeifer.dev/mapd/maps"
	"pfeifer.dev/mapd/cereal/offline"
)

func main() {
	var (
		osmPath     = flag.String("osm", "", "Path to OSM PBF file")
		camerasPath = flag.String("cameras", "", "Path to Rus.radar.txt")
		outDir      = flag.String("out", "./tiles", "Output directory for tiles")
	)
	flag.Parse()

	if *osmPath == "" || *camerasPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: mapd generate --osm <file> --cameras <file> --out <dir>\n")
		os.Exit(1)
	}

	// Parse cameras
	fmt.Println("Parsing speed cameras...")
	rawCams, err := maps.ParseSpeedcamFromFile(*camerasPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: parse cameras: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Parsed %d cameras\n", len(rawCams))

	// Parse OSM ways
	fmt.Println("Parsing OSM ways...")
	ways, err := maps.ParseOSM(*osmPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: parse OSM: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Parsed %d ways\n", len(ways))

	// Generate tiles
	fmt.Println("Generating camera tiles...")
	generation := uint32(time.Now().Unix())
	tiles, err := maps.GenerateCameraTiles(rawCams, ways, generation)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: generate tiles: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Generated %d tiles\n", len(tiles))

	// Save tiles
	fmt.Println("Saving tiles...")
	if err := saveTiles(tiles, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: save tiles: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Done!")
}

func saveTiles(tiles []offline.CameraTile, outDir string) error {
	for _, tile := range tiles {
		latIdx := int(tile.MinLat())
		lonIdx := int(tile.MinLon())
		tileDir := filepath.Join(outDir, "offline", strconv.Itoa(latIdx))
		if err := os.MkdirAll(tileDir, 0o755); err != nil {
			return errors.Wrap(err, "mkdir")
		}

		tilePath := filepath.Join(tileDir, fmt.Sprintf("%d.tar.gz", lonIdx))

		// Create tar.gz with serialized tile
		f, err := os.Create(tilePath)
		if err != nil {
			return errors.Wrap(err, "create file")
		}

		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)

		// Serialize using maps.WriteCameraTile
		tmpFile := filepath.Join(outDir, ".tmp-tile")
		if err := maps.WriteCameraTile(tile, tmpFile); err != nil {
			f.Close()
			return errors.Wrap(err, "write tile")
		}
		data, err := os.ReadFile(tmpFile)
		if err != nil {
			f.Close()
			return errors.Wrap(err, "read tmp")
		}
		os.Remove(tmpFile)

		header := &tar.Header{
			Name: fmt.Sprintf("offline/%d/%d", latIdx, lonIdx),
			Mode: 0o644,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(header); err != nil {
			f.Close()
			return errors.Wrap(err, "tar header")
		}
		if _, err := tw.Write(data); err != nil {
			f.Close()
			return errors.Wrap(err, "tar write")
		}

		tw.Close()
		gw.Close()
		f.Close()

		fmt.Printf("  Saved: %s (%d bytes)\n", tilePath, len(data))
	}
	return nil
}

