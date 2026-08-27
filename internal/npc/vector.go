package npc

import "math"

type Vector3 struct {
	X float64
	Y float64
	Z float64
}

func vectorFromArray(values []float64) (Vector3, bool) {
	if len(values) != 3 {
		return Vector3{}, false
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return Vector3{}, false
		}
	}
	return Vector3{X: values[0], Y: values[1], Z: values[2]}, true
}

func (v Vector3) distance(other Vector3) float64 {
	return math.Sqrt(v.distanceSquared(other))
}

func (v Vector3) distanceSquared(other Vector3) float64 {
	dx := v.X - other.X
	dy := v.Y - other.Y
	dz := v.Z - other.Z
	return dx*dx + dy*dy + dz*dz
}

func (v Vector3) horizontalDistance(other Vector3) float64 {
	return math.Hypot(v.X-other.X, v.Z-other.Z)
}

func (v Vector3) horizontalDirectionTo(other Vector3) Vector3 {
	dx := other.X - v.X
	dz := other.Z - v.Z
	length := math.Hypot(dx, dz)
	if length == 0 {
		return Vector3{}
	}
	return Vector3{X: dx / length, Z: dz / length}
}

func (v Vector3) addScaled(direction Vector3, amount float64) Vector3 {
	return Vector3{
		X: v.X + direction.X*amount,
		Y: v.Y + direction.Y*amount,
		Z: v.Z + direction.Z*amount,
	}
}

func horizontalDot(left, right Vector3) float64 {
	return left.X*right.X + left.Z*right.Z
}
