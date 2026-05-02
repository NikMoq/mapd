package maps

import (
	stdmath "math"
	"sort"
	"time"

	"capnproto.org/go/capnp/v3"
	"pfeifer.dev/mapd/cereal/custom"
	"pfeifer.dev/mapd/cereal/offline"
)

type CameraIndex struct {
	tiles    []offline.CameraTile
	tileMap  map[uint64]int
	baseTime uint32
}

func NewCameraIndex(tiles []offline.CameraTile, generation uint32) *CameraIndex {
	ci := &CameraIndex{
		tiles:    tiles,
		tileMap:  make(map[uint64]int),
		baseTime: generation,
	}
	for i := range tiles {
		ci.tileMap[tiles[i].Hash()] = i
	}
	return ci
}

// FindCameraAhead returns nearest camera ahead. heading in degrees 0-360, speed in m/s.
func (ci *CameraIndex) FindCameraAhead(lat, lon, heading, speed float64) (custom.CameraInfo, bool) {
	if len(ci.tiles) == 0 {
		return custom.CameraInfo{}, false
	}

	now := uint32(time.Now().Unix())
	candidates := ci.collectCandidates(lat, lon)
	if len(candidates) == 0 {
		return custom.CameraInfo{}, false
	}

	var scored []scoredCamera
	for _, cam := range candidates {
		info, ok := ci.evaluateCamera(cam, lat, lon, heading, speed, now)
		if ok {
			scored = append(scored, scoredCamera{
				camera: cam,
				info:   info,
				dist:   float64(info.Distance()),
			})
		}
	}

	if len(scored) == 0 {
		return custom.CameraInfo{}, false
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].dist < scored[j].dist
	})

	best := scored[0]
	camType, _ := best.camera.Type()
	groupID, _ := best.camera.GroupId()
	if camType == "avtodoria" && groupID != "" {
		return ci.buildGroupInfo(best, scored)
	}

	return best.info, true
}

type scoredCamera struct {
	camera offline.Camera
	info   custom.CameraInfo
	dist   float64
}

func (ci *CameraIndex) collectCandidates(lat, lon float64) []offline.Camera {
	var result []offline.Camera
	seen := make(map[uint64]bool)

	var tileSize float64 = 1.0
	if len(ci.tiles) > 0 {
		tileSize = ci.tiles[0].MaxLat() - ci.tiles[0].MinLat()
		if tileSize <= 0 {
			tileSize = 1.0
		}
	}

	baseLat := stdmath.Floor(lat/tileSize) * tileSize
	baseLon := stdmath.Floor(lon/tileSize) * tileSize

	for dLat := -tileSize; dLat <= tileSize; dLat += tileSize {
		for dLon := -tileSize; dLon <= tileSize; dLon += tileSize {
			hash := MortonHash(baseLat+dLat, baseLon+dLon, tileSize)
			if idx, ok := ci.tileMap[hash]; ok {
				tile := ci.tiles[idx]
				cams, err := tile.Cameras()
				if err != nil {
					continue
				}
				for i := range cams.Len() {
					cam := cams.At(i)
					camHash := stdmath.Float64bits(cam.Latitude()) ^ stdmath.Float64bits(cam.Longitude())
					if seen[camHash] {
						continue
					}
					seen[camHash] = true
					result = append(result, cam)
				}
			}
		}
	}
	return result
}

func (ci *CameraIndex) evaluateCamera(cam offline.Camera, lat, lon, heading, speed float64, now uint32) (custom.CameraInfo, bool) {
	dist := haversine(lat, lon, cam.Latitude(), cam.Longitude())
	if dist > 2000.0 {
		return custom.CameraInfo{}, false
	}

	delta := stdmath.Abs(heading - float64(cam.Bearing()))
	if delta > 180.0 {
		delta = 360.0 - delta
	}
	if delta > 45.0 {
		return custom.CameraInfo{}, false
	}

	if speed < 1.39 && dist < 50.0 {
		return custom.CameraInfo{}, false
	}

	confidence := float64(cam.Confidence())
	camType, _ := cam.Type()
	if camType == "mobile" {
		age := now - ci.baseTime
		if age > 24*3600 {
			return custom.CameraInfo{}, false
		}
		if age > 3*3600 {
			confidence *= 0.3
		}
	}

	arena := capnp.MultiSegment([][]byte{})
	msg, seg, err := capnp.NewMessage(arena)
	if err != nil {
		return custom.CameraInfo{}, false
	}
	info, err := custom.NewRootCameraInfo(seg)
	if err != nil {
		return custom.CameraInfo{}, false
	}

	info.SetType(camType)
	info.SetDistance(float32(dist))
	info.SetSpeedLimit(cam.SpeedLimit())
	info.SetConfidence(float32(confidence))
	info.SetIsGroup(false)
	info.SetGroupPos(0)
	info.SetGroupTotal(0)

	_ = msg
	return info, true
}

func (ci *CameraIndex) buildGroupInfo(best scoredCamera, all []scoredCamera) (custom.CameraInfo, bool) {
	bestGroupID, _ := best.camera.GroupId()
	var groupMembers []scoredCamera
	for _, sc := range all {
		gid, _ := sc.camera.GroupId()
		if gid == bestGroupID {
			groupMembers = append(groupMembers, sc)
		}
	}

	sort.Slice(groupMembers, func(i, j int) bool {
		return groupMembers[i].dist < groupMembers[j].dist
	})

	groupPos := uint8(1)
	for i, m := range groupMembers {
		if m.dist == best.dist {
			groupPos = uint8(i + 1)
			break
		}
	}

	info := best.info
	info.SetIsGroup(true)
	info.SetGroupPos(groupPos)
	info.SetGroupTotal(uint8(len(groupMembers)))
	return info, true
}
