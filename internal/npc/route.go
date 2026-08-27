package npc

import "math"

type RouteSample struct {
	Position     Vector3
	Direction    Vector3
	Yaw          float64
	SegmentIndex int
	Progress     float64
}

func routeLength(points []Vector3) float64 {
	var length float64
	for index := 1; index < len(points); index++ {
		length += points[index-1].distance(points[index])
	}
	return length
}

func sampleRoute(points []Vector3, distance float64) (RouteSample, bool) {
	if len(points) == 0 {
		return RouteSample{}, false
	}
	if len(points) == 1 {
		return RouteSample{Position: points[0]}, true
	}
	if distance < 0 {
		distance = 0
	}
	remaining := distance
	for index := 1; index < len(points); index++ {
		previous := points[index-1]
		current := points[index]
		segmentLength := previous.distance(current)
		if remaining <= segmentLength || index == len(points)-1 {
			progress := 1.0
			if segmentLength > 0 {
				progress = math.Min(1, remaining/segmentLength)
			}
			position := Vector3{
				X: previous.X + (current.X-previous.X)*progress,
				Y: previous.Y + (current.Y-previous.Y)*progress,
				Z: previous.Z + (current.Z-previous.Z)*progress,
			}
			direction := previous.horizontalDirectionTo(current)
			return RouteSample{
				Position:     position,
				Direction:    direction,
				Yaw:          math.Atan2(current.X-previous.X, current.Z-previous.Z),
				SegmentIndex: index - 1,
				Progress:     progress,
			}, true
		}
		remaining -= segmentLength
	}
	return RouteSample{}, false
}

func decodePoints(raw [][]float64) ([]Vector3, bool) {
	points := make([]Vector3, 0, len(raw))
	for _, values := range raw {
		point, ok := vectorFromArray(values)
		if !ok {
			return nil, false
		}
		points = append(points, point)
	}
	return points, len(points) > 0
}
