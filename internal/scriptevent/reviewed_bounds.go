package scriptevent

import (
	"errors"
	"math"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

const fullNativeTurn = 0x10000

type worldActorTransform struct {
	X, Y, Z, Yaw float64
}

type reviewedNativeBounds struct {
	position                              [3]float32
	requiredFacingRaw, facingToleranceRaw int
	lateralHalfWidth, verticalHalfExtent  float32
	longitudinalHalfExtent                float32
}

var reviewedBounds = map[string]reviewedNativeBounds{
	"D000/AKIR/d000.hato.spatial.5": {
		position:               [3]float32{-119.13939666748047, 0, 79.6144027709961},
		requiredFacingRaw:      9375,
		facingToleranceRaw:     16384,
		lateralHalfWidth:       0.5,
		verticalHalfExtent:     0.3999999761581421,
		longitudinalHalfExtent: 0.5999999642372131,
	},
}

func validateReviewedBoundsCapabilities(registry scriptcontent.CommandRegistry) error {
	declared := make(map[string]bool)
	for _, capability := range registry.Capabilities {
		if capability.Kind == "bounds" {
			declared[capability.Identifier] = true
		}
	}
	implemented := make(map[string]bool)
	for key := range reviewedBounds {
		separator := strings.LastIndexByte(key, '/')
		if separator < 0 || separator == len(key)-1 {
			return errors.New("reviewed bounds catalog contains an invalid key")
		}
		identifier := key[separator+1:]
		if implemented[identifier] {
			return errors.New("reviewed bounds capability is ambiguous")
		}
		implemented[identifier] = true
	}
	if len(declared) != len(implemented) {
		return errors.New("reviewed bounds capabilities do not match command registry")
	}
	for identifier := range declared {
		if !implemented[identifier] {
			return errors.New("reviewed bounds capabilities do not match command registry")
		}
	}
	return nil
}

// ReviewedActorBounds resolves only explicitly catalogued native interaction
// records. Unknown areas, actors, and bounds remain absent so Yarn queries fail
// closed instead of silently substituting a generic distance check.
func ReviewedActorBounds(area, actor string, x, y, z, yaw float64) map[string]map[string]bool {
	result := map[string]map[string]bool{}
	if !finite(x) || !finite(y) || !finite(z) || !finite(yaw) {
		return result
	}
	transform := worldActorTransform{X: x, Y: y, Z: z, Yaw: yaw}
	prefix := area + "/" + actor + "/"
	for key, bounds := range reviewedBounds {
		if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		if result[actor] == nil {
			result[actor] = map[string]bool{}
		}
		result[actor][key[len(prefix):]] = matchesReviewedBounds(bounds, transform)
	}
	return result
}

func matchesReviewedBounds(bounds reviewedNativeBounds, actor worldActorTransform) bool {
	nativePosition := [3]float32{float32(-actor.X), float32(actor.Y), float32(actor.Z)}
	vertical := bounds.position[1] - nativePosition[1]
	if !(abs32(vertical) < bounds.verticalHalfExtent) {
		return false
	}
	facingRaw := worldYawToNativeRaw(actor.Yaw)
	radians := float64(facingRaw) * math.Pi * 2 / fullNativeTurn
	sine, cosine := float32(math.Sin(radians)), float32(math.Cos(radians))
	deltaX := bounds.position[0] - nativePosition[0]
	deltaZ := bounds.position[2] - nativePosition[2]
	lateral := float32(float32(deltaX*cosine) - float32(deltaZ*sine))
	longitudinal := float32(float32(deltaZ*cosine) + float32(deltaX*sine))
	if !(abs32(lateral) < bounds.lateralHalfWidth) ||
		!(abs32(longitudinal) < bounds.longitudinalHalfExtent) {
		return false
	}
	return circularNativeDifference(facingRaw, bounds.requiredFacingRaw) < bounds.facingToleranceRaw
}

func worldYawToNativeRaw(yaw float64) int {
	raw := int(-yaw / (math.Pi * 2) * fullNativeTurn)
	return ((raw % fullNativeTurn) + fullNativeTurn) % fullNativeTurn
}

func circularNativeDifference(left, right int) int {
	direct := left - right
	if direct < 0 {
		direct = -direct
	}
	if wrapped := fullNativeTurn - direct; wrapped < direct {
		return wrapped
	}
	return direct
}

func abs32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
