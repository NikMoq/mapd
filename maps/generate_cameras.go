package maps

import (
	"fmt"
	"os"
	"path/filepath"
	stdmath "math"
	"sort"

	"capnproto.org/go/capnp/v3"
	"github.com/pkg/errors"
	"pfeifer.dev/mapd/cereal/offline"
)

type CameraGen struct {
	Lat        float64
	Lon        float64
	Type       string
	SpeedLimit float32
	Bearing    float64
	Confidence float32
	GroupID    string
}

func GenerateCameraTiles(rawCams []RawCamera, ways []Way, generation uint32) ([]offline.CameraTile, error) {
	if len(rawCams) == 0 {
		return nil, nil
	}

	wayIndex := NewWayIndex(ways, 0.01)

	var allCameras []CameraGen
	for _, rc := range rawCams {
		matched := wayIndex.FindNearestWays(rc.Lat, rc.Lon, 25.0)
		if len(matched) == 0 {
			continue
		}

		closeWays := filterWithin(matched, 15.0)
		if len(closeWays) >= 2 {
			for _, mw := range closeWays {
				allCameras = append(allCameras, buildCamera(rc, mw, 0.5))
			}
		} else {
			best := matched[0]
			if best.Way.OneWay() {
				allCameras = append(allCameras, buildCamera(rc, best, 0.95))
			} else {
				allCameras = append(allCameras, buildCamera(rc, best, 0.7))
				cam2 := buildCamera(rc, best, 0.7)
				cam2.Bearing = normalizeBearing(best.Bearing + 180.0)
				allCameras = append(allCameras, cam2)
			}
		}
	}

	allCameras = groupAvtodoria(allCameras)
	tiles := buildAdaptiveTiles(allCameras, generation)
	return tiles, nil
}

func filterWithin(matches []MatchedWay, maxDist float64) []MatchedWay {
	var res []MatchedWay
	for _, m := range matches {
		if m.Distance <= maxDist {
			res = append(res, m)
		}
	}
	return res
}

func buildCamera(rc RawCamera, mw MatchedWay, confidence float32) CameraGen {
	sl := float32(mw.Way.MaxSpeed() * 3.6)
	if sl < 0 {
		sl = 0
	}
	return CameraGen{
		Lat:        rc.Lat,
		Lon:        rc.Lon,
		Type:       rc.Type,
		SpeedLimit: sl,
		Bearing:    normalizeBearing(mw.Bearing),
		Confidence: confidence,
		GroupID:    "",
	}
}

func normalizeBearing(b float64) float64 {
	for b < 0 {
		b += 360.0
	}
	for b >= 360.0 {
		b -= 360.0
	}
	return b
}

func groupAvtodoria(cams []CameraGen) []CameraGen {
	var avtoIdx []int
	for i := range cams {
		if cams[i].Type == "avtodoria" {
			avtoIdx = append(avtoIdx, i)
		}
	}
	if len(avtoIdx) < 2 {
		return cams
	}

	groupNum := 1
	visited := make(map[int]bool)

	for _, startIdx := range avtoIdx {
		if visited[startIdx] {
			continue
		}
		groupID := fmt.Sprintf("avto_%d", groupNum)
		groupNum++

		queue := []int{startIdx}
		visited[startIdx] = true
		var groupMembers []int

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			groupMembers = append(groupMembers, curr)

			for _, otherIdx := range avtoIdx {
				if visited[otherIdx] {
					continue
				}
				d := haversine(cams[curr].Lat, cams[curr].Lon, cams[otherIdx].Lat, cams[otherIdx].Lon)
				if d <= 500.0 {
					visited[otherIdx] = true
					queue = append(queue, otherIdx)
				}
			}
		}

		for _, idx := range groupMembers {
			cams[idx].GroupID = groupID
		}
	}

	return cams
}

func buildAdaptiveTiles(cams []CameraGen, generation uint32) []offline.CameraTile {
	if len(cams) == 0 {
		return nil
	}

	minLat, minLon := cams[0].Lat, cams[0].Lon
	maxLat, maxLon := cams[0].Lat, cams[0].Lon
	for _, c := range cams[1:] {
		if c.Lat < minLat { minLat = c.Lat }
		if c.Lat > maxLat { maxLat = c.Lat }
		if c.Lon < minLon { minLon = c.Lon }
		if c.Lon > maxLon { maxLon = c.Lon }
	}

	var result []offline.CameraTile
	tileSize := 1.0

	for lat := stdmath.Floor(minLat/tileSize) * tileSize; lat < maxLat; lat += tileSize {
		for lon := stdmath.Floor(minLon/tileSize) * tileSize; lon < maxLon; lon += tileSize {
			tileCams := selectInBox(cams, lat, lon, lat+tileSize, lon+tileSize)
			if len(tileCams) == 0 {
				continue
			}
			subTiles := subdivideTile(tileCams, lat, lon, tileSize, generation)
			for _, t := range subTiles {
				if t.IsValid() {
					result = append(result, t)
				}
			}
		}
	}
	return result
}

func subdivideTile(cams []CameraGen, minLat, minLon, size float64, generation uint32) []offline.CameraTile {
	const maxCameras = 1000
	const minSize = 0.125

	if len(cams) <= maxCameras || size <= minSize {
		if len(cams) > maxCameras && size <= minSize {
			var filtered []CameraGen
			for _, c := range cams {
				if c.Confidence >= 0.6 {
					filtered = append(filtered, c)
				}
			}
			if len(filtered) > maxCameras {
				sort.Slice(filtered, func(i, j int) bool {
					return filtered[i].Confidence > filtered[j].Confidence
				})
				filtered = filtered[:maxCameras]
			}
			cams = filtered
		}
		if len(cams) == 0 {
			return nil
		}
		tile, err := createCameraTile(cams, minLat, minLon, minLat+size, minLon+size, size, generation)
		if err != nil {
			return nil
		}
		return []offline.CameraTile{tile}
	}

	half := size / 2.0
	var result []offline.CameraTile
	for qLat := 0; qLat < 2; qLat++ {
		for qLon := 0; qLon < 2; qLon++ {
			qMinLat := minLat + float64(qLat)*half
			qMinLon := minLon + float64(qLon)*half
			qCams := selectInBox(cams, qMinLat, qMinLon, qMinLat+half, qMinLon+half)
			if len(qCams) == 0 {
				continue
			}
			sub := subdivideTile(qCams, qMinLat, qMinLon, half, generation)
			result = append(result, sub...)
		}
	}
	return result
}

func selectInBox(cams []CameraGen, minLat, minLon, maxLat, maxLon float64) []CameraGen {
	var res []CameraGen
	for _, c := range cams {
		if c.Lat >= minLat && c.Lat < maxLat && c.Lon >= minLon && c.Lon < maxLon {
			res = append(res, c)
		}
	}
	return res
}

func createCameraTile(cams []CameraGen, minLat, minLon, maxLat, maxLon, tileSize float64, generation uint32) (offline.CameraTile, error) {
	arena := capnp.MultiSegment([][]byte{})
	msg, seg, err := capnp.NewMessage(arena)
	if err != nil {
		return offline.CameraTile{}, errors.Wrap(err, "capnp new message")
	}

	root, err := offline.NewRootOffline(seg)
	if err != nil {
		return offline.CameraTile{}, errors.Wrap(err, "capnp new root offline")
	}

	tiles, err := root.NewCameraTiles(1)
	if err != nil {
		return offline.CameraTile{}, errors.Wrap(err, "capnp new camera tiles")
	}

	tile := tiles.At(0)
	tile.SetMinLat(minLat)
	tile.SetMinLon(minLon)
	tile.SetMaxLat(maxLat)
	tile.SetMaxLon(maxLon)
	tile.SetHash(MortonHash(minLat, minLon, tileSize))

	capnpCams, err := tile.NewCameras(int32(len(cams)))
	if err != nil {
		return offline.CameraTile{}, errors.Wrap(err, "capnp new cameras list")
	}

	for i, c := range cams {
		cam := capnpCams.At(i)
		cam.SetLatitude(c.Lat)
		cam.SetLongitude(c.Lon)
		if err := cam.SetType(c.Type); err != nil {
			return offline.CameraTile{}, errors.Wrap(err, "capnp set type")
		}
		cam.SetSpeedLimit(c.SpeedLimit)
		cam.SetBearing(float32(c.Bearing))
		cam.SetConfidence(c.Confidence)
		if err := cam.SetGroupId(c.GroupID); err != nil {
			return offline.CameraTile{}, errors.Wrap(err, "capnp set groupId")
		}
		cam.SetTimestamp(generation)
	}

	root.SetGeneration(generation)
	root.SetVersion("rus-p0")

	_ = msg
	return tile, nil
}

func WriteCameraTile(tile offline.CameraTile, path string) error {
	arena := capnp.MultiSegment([][]byte{})
	msg, seg, err := capnp.NewMessage(arena)
	if err != nil {
		return errors.Wrap(err, "new message")
	}

	root, err := offline.NewRootOffline(seg)
	if err != nil {
		return errors.Wrap(err, "new root")
	}

	tiles, err := root.NewCameraTiles(1)
	if err != nil {
		return errors.Wrap(err, "new camera tiles")
	}

	dest := tiles.At(0)
	dest.SetMinLat(tile.MinLat())
	dest.SetMinLon(tile.MinLon())
	dest.SetMaxLat(tile.MaxLat())
	dest.SetMaxLon(tile.MaxLon())
	dest.SetHash(tile.Hash())

	srcCams, err := tile.Cameras()
	if err != nil {
		return errors.Wrap(err, "read cameras")
	}

	dstCams, err := dest.NewCameras(int32(srcCams.Len()))
	if err != nil {
		return errors.Wrap(err, "new cameras")
	}

	for i := range srcCams.Len() {
		src := srcCams.At(i)
		dst := dstCams.At(i)
		dst.SetLatitude(src.Latitude())
		dst.SetLongitude(src.Longitude())
		typeStr, _ := src.Type()
		dst.SetType(typeStr)
		dst.SetSpeedLimit(src.SpeedLimit())
		dst.SetBearing(src.Bearing())
		dst.SetConfidence(src.Confidence())
		gid, _ := src.GroupId()
		dst.SetGroupId(gid)
		dst.SetTimestamp(src.Timestamp())
	}

	data, err := msg.MarshalPacked()
	if err != nil {
		return errors.Wrap(err, "marshal")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o775); err != nil {
		return errors.Wrap(err, "mkdir")
	}
	return os.WriteFile(path, data, 0o644)
}

func ReadCameraTile(data []byte) (offline.CameraTile, error) {
	msg, err := capnp.UnmarshalPacked(data)
	if err != nil {
		return offline.CameraTile{}, errors.Wrap(err, "unmarshal")
	}

	root, err := offline.ReadRootOffline(msg)
	if err != nil {
		return offline.CameraTile{}, errors.Wrap(err, "read root")
	}

	tiles, err := root.CameraTiles()
	if err != nil {
		return offline.CameraTile{}, errors.Wrap(err, "camera tiles")
	}

	if tiles.Len() == 0 {
		return offline.CameraTile{}, errors.New("no camera tiles in file")
	}

	return tiles.At(0), nil
}
