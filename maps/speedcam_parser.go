package maps

import (
	"bufio"
	stdmath "math"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// RawCamera represents a parsed camera from speedcamonline.ru
type RawCamera struct {
	Lat  float64
	Lon  float64
	Type string // normalized: stationary, mobile, tripod, avtodoria
}

var cameraTypeMap = []struct {
	keywords []string
	result   string
}{
	{[]string{"СТЦ", "Стационар", "Стационарная"}, "stationary"},
	{[]string{"МЛЖ", "Передвижная", "Мобильная"}, "mobile"},
	{[]string{"Тренога"}, "tripod"},
	{[]string{"Автодория", "Автодорья"}, "avtodoria"},
}

func normalizeCameraType(raw string) string {
	upper := strings.ToUpper(raw)
	for _, mapping := range cameraTypeMap {
		for _, kw := range mapping.keywords {
			if strings.Contains(upper, strings.ToUpper(kw)) {
				return mapping.result
			}
		}
	}
	return "stationary"
}

// ParseSpeedcam parses speedcamonline.ru Rus.radar.txt (Windows-1251, CityPlan format)
func ParseSpeedcam(data []byte) ([]RawCamera, error) {
	decoder := charmap.Windows1251.NewDecoder()
	utf8Data, _, err := transform.Bytes(decoder, data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode cp1251")
	}

	var cameras []RawCamera
	scanner := bufio.NewScanner(strings.NewReader(string(utf8Data)))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.Contains(line, "|>") {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		typePart := strings.TrimSpace(parts[0])
		latStr := strings.TrimSpace(parts[1])
		lonStr := strings.TrimSpace(parts[2])

		typeStr := typePart
		if idx := strings.Index(typePart, "."); idx != -1 {
			typeStr = strings.TrimSpace(typePart[idx+1:])
		}

		lat, err := strconv.ParseFloat(latStr, 64)
		if err != nil {
			continue
		}
		lon, err := strconv.ParseFloat(lonStr, 64)
		if err != nil {
			continue
		}

		if lat < 41.0 || lat > 82.0 || lon < 19.0 || lon > 169.0 {
			continue
		}

		camType := normalizeCameraType(typeStr)
		cameras = append(cameras, RawCamera{
			Lat:  lat,
			Lon:  lon,
			Type: camType,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "scanner error")
	}

	cameras = deduplicateCameras(cameras)
	return cameras, nil
}

const dedupCellSize = 0.0005

func deduplicateCameras(cams []RawCamera) []RawCamera {
	type gridKey struct {
		latIdx int
		lonIdx int
	}

	grid := make(map[gridKey][]RawCamera)
	var result []RawCamera

	for _, cam := range cams {
		latIdx := int(stdmath.Floor(cam.Lat / dedupCellSize))
		lonIdx := int(stdmath.Floor(cam.Lon / dedupCellSize))

		duplicate := false
		for dLat := -1; dLat <= 1 && !duplicate; dLat++ {
			for dLon := -1; dLon <= 1 && !duplicate; dLon++ {
				key := gridKey{latIdx + dLat, lonIdx + dLon}
				for _, existing := range grid[key] {
					if existing.Type != cam.Type {
						continue
					}
					if haversine(cam.Lat, cam.Lon, existing.Lat, existing.Lon) < 5.0 {
						duplicate = true
						break
					}
				}
			}
		}

		if !duplicate {
			key := gridKey{latIdx, lonIdx}
			grid[key] = append(grid[key], cam)
			result = append(result, cam)
		}
	}

	return result
}

// ParseSpeedcamFromFile reads and parses a Rus.radar.txt file
func ParseSpeedcamFromFile(path string) ([]RawCamera, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read speedcam file")
	}
	return ParseSpeedcam(data)
}

// haversine distance in meters
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000.0 // Earth radius in meters
	phi1 := lat1 * stdmath.Pi / 180
	phi2 := lat2 * stdmath.Pi / 180
	deltaPhi := (lat2 - lat1) * stdmath.Pi / 180
	deltaLambda := (lon2 - lon1) * stdmath.Pi / 180

	a := stdmath.Sin(deltaPhi/2)*stdmath.Sin(deltaPhi/2) +
		stdmath.Cos(phi1)*stdmath.Cos(phi2)*
			stdmath.Sin(deltaLambda/2)*stdmath.Sin(deltaLambda/2)
	c := 2 * stdmath.Atan2(stdmath.Sqrt(a), stdmath.Sqrt(1-a))

	return R * c
}
