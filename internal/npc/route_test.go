package npc

import (
	"math"
	"testing"
)

func TestSampleRouteReportsSegmentAndProgress(t *testing.T) {
	points := []Vector3{
		{X: 0, Z: 0},
		{X: 3, Z: 0},
		{X: 3, Z: 4},
	}
	sample, ok := sampleRoute(points, 5)
	if !ok {
		t.Fatal("route did not sample")
	}
	if sample.SegmentIndex != 1 || math.Abs(sample.Progress-0.5) > 1e-9 {
		t.Fatalf("sample = segment %d progress %f", sample.SegmentIndex, sample.Progress)
	}
	if sample.Position != (Vector3{X: 3, Z: 2}) {
		t.Fatalf("position = %+v", sample.Position)
	}
}
