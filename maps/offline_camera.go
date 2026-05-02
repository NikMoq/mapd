package maps

import (
	"log/slog"
	stdmath "math"

	"capnproto.org/go/capnp/v3"
	"pfeifer.dev/mapd/cereal/offline"
)

// OfflineCameraExt extends Offline with camera tile access.
type OfflineCameraExt struct {
	Offline   offline.Offline
	Tiles     []offline.CameraTile
	Loaded    bool
	Gen       uint32
	Version   string
}

// ReadOfflineWithCameras reads an Offline message that may contain camera tiles.
func ReadOfflineWithCameras(data []uint8) OfflineCameraExt {
	msg, err := capnp.UnmarshalPacked(data)
	if err != nil {
		slog.Warn("could not unmarshal offline data", "error", err)
		return OfflineCameraExt{Loaded: false}
	}

	o, err := offline.ReadRootOffline(msg)
	if err != nil {
		slog.Warn("could not read offline message", "error", err)
		return OfflineCameraExt{Loaded: false}
	}

	o.Message().ResetReadLimit(stdmath.MaxUint64)

	ext := OfflineCameraExt{
		Offline: o,
		Loaded:  true,
		Gen:     o.Generation(),
	}

	ver, err := o.Version()
	if err == nil {
		ext.Version = ver
	}

	tiles, err := o.CameraTiles()
	if err == nil && tiles.Len() > 0 {
		ext.Tiles = make([]offline.CameraTile, tiles.Len())
		for i := range tiles.Len() {
			ext.Tiles[i] = tiles.At(i)
		}
	}

	return ext
}
