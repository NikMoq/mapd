package main

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/NikMoq/mapd/maps"
	m "github.com/NikMoq/mapd/math"
	ms "github.com/NikMoq/mapd/settings"
	"github.com/NikMoq/mapd/utils"
)

// SpeedCameraType - типы камер
type SpeedCameraType string

const (
	CameraTypeSpeed    SpeedCameraType = "speed"     // Контроль скорости
	CameraTypeTraffic  SpeedCameraType = "traffic"   // Контроль светофора
	CameraTypeZone     SpeedCameraType = "zone"      // Контроль зоны (город/трасса)
	CameraTypeAverage  SpeedCameraType = "average"   // Средняя скорость
)

// SpeedCamera - структура камеры
type SpeedCamera struct {
	Lat         float64         `json:"lat"`
	Lon         float64         `json:"lon"`
	Type        SpeedCameraType `json:"type"`
	SpeedLimit  float64         `json:"speed_limit"` // 0 если нет ограничения
	Description string          `json:"description"`
	Valid       bool            `json:"-"`
}

// SpeedCameraState - состояние камер
type SpeedCameraState struct {
	Cameras            []SpeedCamera         // Все загруженные камеры
	CameraIndex        map[string][]SpeedCamera // Индекс по координатам (для быстрого lookup)
	NextCamera         utils.Float64Tracker  // Расстояние до следующей камеры
	NextCameraSpeed    utils.Float32Tracker  // Ограничение на камере
	NextCameraType     string                // Тип камеры
	NextCameraValid    utils.BoolTracker     // Валидность данных
	LastCameraDistance float64               // Расстояние до последней известной камеры
}

// SpeedCameraAlert - предупреждение о камере
type SpeedCameraAlert struct {
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Type        string  `json:"type"`
	SpeedLimit  float64 `json:"speed_limit"`
	Distance    float64 `json:"distance"`
	Description string  `json:"description"`
	Valid       bool    `json:"valid"`
}

// Init - инициализация состояния камер
func (cs *SpeedCameraState) Init() {
	cs.CameraIndex = make(map[string][]SpeedCamera)
	cs.NextCamera.AllowNullLastValue = false
	cs.NextCameraSpeed.AllowNullLastValue = true
	cs.NextCameraValid.AllowNullLastValue = false
}

// LoadCameras - загрузка камер из файла Rus.radar.txt
func (cs *SpeedCameraState) LoadCameras(camerasFile string) error {
	file, err := os.Open(camerasFile)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("Cameras file not found, speed camera alerts disabled", "file", camerasFile)
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	cameras := []SpeedCamera{}
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Пропускаем пустые строки и комментарии
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Парсинг формата: LAT;LON;TYPE;SPEED;DESCRIPTION
		parts := strings.Split(line, ";")
		if len(parts) < 3 {
			slog.Warn("Invalid camera line format", "line", lineNum, "parts", len(parts))
			continue
		}

		// Парсинг координат
		lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			slog.Warn("Invalid latitude", "line", lineNum, "value", parts[0])
			continue
		}

		lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			slog.Warn("Invalid longitude", "line", lineNum, "value", parts[1])
			continue
		}

		// Парсинг типа камеры
		cameraType := SpeedCameraType(strings.TrimSpace(parts[2]))
		if cameraType != CameraTypeSpeed && cameraType != CameraTypeTraffic &&
			cameraType != CameraTypeZone && cameraType != CameraTypeAverage {
			// По умолчанию считаем speed camera
			cameraType = CameraTypeSpeed
		}

		// Парсинг ограничения скорости (опционально)
		speedLimit := 0.0
		if len(parts) >= 4 && parts[3] != "" {
			speedLimit, _ = strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		}

		// Парсинг описания (опционально)
		description := ""
		if len(parts) >= 5 {
			description = strings.TrimSpace(parts[4])
		}

		camera := SpeedCamera{
			Lat:         lat,
			Lon:         lon,
			Type:        cameraType,
			SpeedLimit:  speedLimit,
			Description: description,
			Valid:       true,
		}

		cameras = append(cameras, camera)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	cs.Cameras = cameras
	cs.buildCameraIndex()

	slog.Info("Loaded speed cameras", "count", len(cameras), "file", camerasFile)
	return nil
}

// buildCameraIndex - построение spatial index для быстрого lookup
func (cs *SpeedCameraState) buildCameraIndex() {
	cs.CameraIndex = make(map[string][]SpeedCamera)

	for _, camera := range cs.Cameras {
		// Группируем камеры по сетке 0.01 градус (~1km)
		gridLat := int(camera.Lat * 100)
		gridLon := int(camera.Lon * 100)
		gridKey := strconv.Itoa(gridLat) + "," + strconv.Itoa(gridLon)

		cs.CameraIndex[gridKey] = append(cs.CameraIndex[gridKey], camera)
	}

	slog.Debug("Built camera index", "gridCells", len(cs.CameraIndex))
}

// Update - обновление состояния камер на основе позиции
func (cs *SpeedCameraState) Update(pos m.Pos, car CarState) {
	nearestCamera, distance := cs.findNearestCamera(pos, car)

	if nearestCamera.Valid {
		cs.NextCamera.Update(distance)
		cs.NextCameraSpeed.Update(float32(nearestCamera.SpeedLimit))
		cs.NextCameraType = string(nearestCamera.Type)
		cs.NextCameraValid.Update(true)
		cs.LastCameraDistance = distance
	} else {
		cs.NextCameraValid.Update(false)
	}
}

// findNearestCamera - поиск ближайшей камеры в направлении движения
func (cs *SpeedCameraState) findNearestCamera(pos m.Pos, car CarState) (SpeedCamera, float64) {
	bestCamera := SpeedCamera{Valid: false}
	bestDistance := float64(10000) // 10km max range

	// Получаем координаты сетки вокруг текущей позиции
	gridLat := int(pos.Lat() * 100)
	gridLon := int(pos.Lon() * 100)

	// Ищем в соседних ячейках сетки (3x3 область)
	for dl := -1; dl <= 1; dl++ {
		for do := -1; do <= 1; do++ {
			gridKey := strconv.Itoa(gridLat+dl) + "," + strconv.Itoa(gridLon+do)
			cameras := cs.CameraIndex[gridKey]

			for _, camera := range cameras {
				dist := cs.calculateDistanceAndDirection(pos, car, camera)
				if dist > 0 && dist < bestDistance {
					bestCamera = camera
					bestCamera.Valid = true
					bestDistance = dist
				}
			}
		}
	}

	return bestCamera, bestDistance
}

// calculateDistanceAndDirection - расчет расстояния с учетом направления движения
func (cs *SpeedCameraState) calculateDistanceAndDirection(pos m.Pos, car CarState, camera SpeedCamera) float64 {
	// Расстояние до камеры
	dist := pos.DistanceTo(m.NewPosition(camera.Lat, camera.Lon))

	// Проверяем угол между направлением движения и камерой
	course := car.Course()
	toCamera := pos.BearingTo(m.NewPosition(camera.Lat, camera.Lon))
	angleDiff := m.NormalizeAngle(toCamera - course)

	// Игнорируем камеры сзади (угол > 90 градусов)
	if angleDiff > m.DEG_TO_RAD*90 {
		return -1
	}

	// Игнорируем камеры если скорость очень низкая (< 5 км/ч)
	if car.VEgo < 1.4 { // 5 км/ч в м/с
		return -1
	}

	return dist
}

// GetAlert - получение предупреждения о камере
func (cs *SpeedCameraState) GetAlert() SpeedCameraAlert {
	if !cs.NextCameraValid.Value {
		return SpeedCameraAlert{Valid: false}
	}

	return SpeedCameraAlert{
		Lat:         0,
		Lon:         0,
		Type:        cs.NextCameraType,
		SpeedLimit:  float64(cs.NextCameraSpeed.Value),
		Distance:    cs.NextCameraDistance(),
		Description: "",
		Valid:       true,
	}
}

// NextCameraDistance - расстояние до следующей камеры
func (cs *SpeedCameraState) NextCameraDistance() float64 {
	return cs.NextCamera.Value
}

// GetCamerasInBoundingBox - получение камер в заданной области (для генерации тайлов)
func (cs *SpeedCameraState) GetCamerasInBoundingBox(minLat, minLon, maxLat, maxLon float64) []SpeedCamera {
	result := []SpeedCamera{}

	for _, camera := range cs.Cameras {
		if camera.Lat >= minLat && camera.Lat <= maxLat &&
			camera.Lon >= minLon && camera.Lon <= maxLon {
			result = append(result, camera)
		}
	}

	// Сортируем по расстоянию от начала области
	sort.Slice(result, func(i, j int) bool {
		return result[i].Lat < result[j].Lat ||
			(result[i].Lat == result[j].Lat && result[i].Lon < result[j].Lon)
	})

	return result
}

// ToJSON - сериализация в JSON
func (cs *SpeedCameraState) ToJSON() string {
	alert := cs.GetAlert()
	data, _ := json.Marshal(alert)
	return string(data)
}

// GetCamerasCount - получить количество загруженных камер
func (cs *SpeedCameraState) GetCamerasCount() int {
	return len(cs.Cameras)
}

// GetCamerasForRegion - получить камеры для региона (при генерации тайлов)
func GetCamerasForRegion(camerasFile string, minLat, minLon, maxLat, maxLon float64) ([]SpeedCamera, error) {
	if camerasFile == "" {
		return []SpeedCamera{}, nil
	}

	file, err := os.Open(camerasFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []SpeedCamera{}, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	cameras := []SpeedCamera{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ";")
		if len(parts) < 3 {
			continue
		}

		lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			continue
		}

		lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			continue
		}

		// Фильтруем по bounding box
		if lat >= minLat && lat <= maxLat && lon >= minLon && lon <= maxLon {
			cameraType := SpeedCameraType(strings.TrimSpace(parts[2]))
			speedLimit := 0.0
			if len(parts) >= 4 && parts[3] != "" {
				speedLimit, _ = strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
			}
			description := ""
			if len(parts) >= 5 {
				description = strings.TrimSpace(parts[4])
			}

			cameras = append(cameras, SpeedCamera{
				Lat:         lat,
				Lon:         lon,
				Type:        cameraType,
				SpeedLimit:  speedLimit,
				Description: description,
				Valid:       true,
			})
		}
	}

	return cameras, scanner.Err()
}

// WriteCamerasToOffline - запись камер в офлайн тайлы
func WriteCamerasToOffline(cameras []SpeedCamera, outputDir string) error {
	if len(cameras) == 0 {
		return nil
	}

	data, err := json.Marshal(cameras)
	if err != nil {
		return err
	}

	filename := outputDir + "/cameras.json"
	return os.WriteFile(filename, data, 0o644)
}

// LoadCamerasFromOffline - загрузка камер из офлайн тайлов
func LoadCamerasFromOffline(offlineDir string) ([]SpeedCamera, error) {
	filename := offlineDir + "/cameras.json"

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []SpeedCamera{}, nil
		}
		return nil, err
	}

	var cameras []SpeedCamera
	err = json.Unmarshal(data, &cameras)
	if err != nil {
		return nil, err
	}

	slog.Info("Loaded offline cameras", "count", len(cameras), "file", filename)
	return cameras, nil
}

// GetCamerasFileFromSettings - получение пути к файлу камер из настроек
func GetCamerasFileFromSettings() string {
	camerasFile := ms.Settings.CamerasFile
	if camerasFile != "" {
		return camerasFile
	}

	// Region-aware paths для разных окружений
	// Приоритет:
	// 1. /data/media/0/maps/russia/{region}/Rus.radar.txt
	// 2. /data/media/0/osm/radars/Rus.radar.txt
	// 3. /data/media/0/osm/Rus.radar.txt
	// 4. /tmp/Rus.radar.txt

	// Проверяем region-aware paths
	region := ms.Settings.RegionId
	if region == "" {
		region = "krasnodar" // default
	}

	regionAwarePaths := []string{
		fmt.Sprintf("/data/media/0/maps/russia/%s/Rus.radar.txt", region),
		fmt.Sprintf("/data/media/0/osm/maps/russia/%s/Rus.radar.txt", region),
	}

	// Стандартные paths
	defaultPaths := []string{
		"/data/media/0/osm/radars/Rus.radar.txt",
		"/data/media/0/osm/Rus.radar.txt",
		"/tmp/Rus.radar.txt",
		"/tmp/camera-cache/Rus.radar.txt",
	}

	// Проверяем region-aware paths сначала
	for _, path := range regionAwarePaths {
		if _, err := os.Stat(path); err == nil {
			slog.Debug("Using region-aware radar DB", "path", path, "region", region)
			return path
		}
	}

	// Затем проверяем стандартные paths
	for _, path := range defaultPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

