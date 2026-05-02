package maps

import (
	stdmath "math"
	"sort"

	m "pfeifer.dev/mapd/math"
)

type WayIndex struct {
	grid     map[gridKey][]Way
	cellSize float64
}

type gridKey struct {
	latIdx int64
	lonIdx int64
}

func NewWayIndex(ways []Way, cellSize float64) *WayIndex {
	wi := &WayIndex{
		grid:     make(map[gridKey][]Way),
		cellSize: cellSize,
	}
	for _, w := range ways {
		box := w.Box()
		minLatIdx := int64(stdmath.Floor(box.MinPos.Lat() / cellSize))
		minLonIdx := int64(stdmath.Floor(box.MinPos.Lon() / cellSize))
		maxLatIdx := int64(stdmath.Floor(box.MaxPos.Lat() / cellSize))
		maxLonIdx := int64(stdmath.Floor(box.MaxPos.Lon() / cellSize))

		for latIdx := minLatIdx; latIdx <= maxLatIdx; latIdx++ {
			for lonIdx := minLonIdx; lonIdx <= maxLonIdx; lonIdx++ {
				key := gridKey{latIdx, lonIdx}
				wi.grid[key] = append(wi.grid[key], w)
			}
		}
	}
	return wi
}

type MatchedWay struct {
	Way      Way
	Distance float64
	Bearing  float64
}

func (wi *WayIndex) FindNearestWays(lat, lon float64, maxDist float64) []MatchedWay {
	pos := m.NewPosition(lat, lon)
	cellLat := int64(stdmath.Floor(lat / wi.cellSize))
	cellLon := int64(stdmath.Floor(lon / wi.cellSize))

	maxDistDeg := maxDist / 111000.0
	cellRange := int64(stdmath.Ceil(maxDistDeg / wi.cellSize))
	if cellRange < 1 {
		cellRange = 1
	}

	seen := make(map[uint64]bool)
	var matches []MatchedWay

	for dLat := -cellRange; dLat <= cellRange; dLat++ {
		for dLon := -cellRange; dLon <= cellRange; dLon++ {
			key := gridKey{cellLat + dLat, cellLon + dLon}
			for _, w := range wi.grid[key] {
				nodes := w.Nodes()
				var wayHash uint64
				if len(nodes) > 0 {
					wayHash = stdmath.Float64bits(nodes[0].Lat()) ^ stdmath.Float64bits(nodes[0].Lon())
				}
				if seen[wayHash] {
					continue
				}
				seen[wayHash] = true

				dist, bearing, ok := wi.distanceAndBearingToWay(pos, w)
				if !ok || dist > maxDist {
					continue
				}
				matches = append(matches, MatchedWay{
					Way:      w,
					Distance: dist,
					Bearing:  bearing,
				})
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Distance < matches[j].Distance
	})

	return matches
}

func (wi *WayIndex) distanceAndBearingToWay(pos m.Position, w Way) (float64, float64, bool) {
	nodes := w.Nodes()
	if len(nodes) < 2 {
		return 0, 0, false
	}

	minDist := stdmath.MaxFloat64
	var bestBearing float64

	for i := 0; i < len(nodes)-1; i++ {
		line := m.Line{Start: nodes[i], End: nodes[i+1]}
		lp := line.NearestPosition(pos)
		dist := float64(pos.DistanceTo(lp.Pos))
		if dist < minDist {
			minDist = dist
			vec := nodes[i].VectorTo(nodes[i+1])
			bestBearing = vec.Bearing() * 180.0 / stdmath.Pi
			if bestBearing < 0 {
				bestBearing += 360.0
			}
		}
	}

	return minDist, bestBearing, true
}
