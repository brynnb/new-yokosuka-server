// Command npc-audit samples a complete authoritative NPC day and reports
// visible same-world discontinuities for extraction review.
package main

import (
	"fmt"
	"log"
	"sort"

	"github.com/brynnb/new-yokosuka-server/internal/npc"
	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

type discontinuity struct {
	actorID        string
	second         int
	distance       float64
	fromOperation  int
	toOperation    int
	fromRoute      string
	toRoute        string
	classification string
}

func main() {
	manifest, err := npcdata.Load()
	if err != nil {
		log.Fatal(err)
	}
	interpreter := npc.NewInterpreter(manifest.AreaWorlds)
	if err := interpreter.Compile(manifest.Actors); err != nil {
		log.Fatal(err)
	}
	rows := make([]discontinuity, 0)
	for _, actor := range manifest.Actors {
		var previous npc.State
		for second := 0; second < 24*60*60; second++ {
			current, err := interpreter.Evaluate(actor, float64(second))
			if err != nil {
				log.Fatalf("%s at %d: %v", actor.InstanceID, second, err)
			}
			if previous.Visible() && current.Visible() {
				kind, distance := npc.ClassifyDiscontinuity(
					previous,
					current,
				)
				if kind != "" {
					rows = append(rows, discontinuity{
						actorID:        actor.InstanceID,
						second:         second,
						distance:       distance,
						fromOperation:  previous.Operation,
						toOperation:    current.Operation,
						fromRoute:      previous.RouteID,
						toRoute:        current.RouteID,
						classification: kind,
					})
				}
			}
			previous = current
		}
	}
	sort.Slice(rows, func(left, right int) bool {
		return rows[left].distance > rows[right].distance
	})
	fmt.Printf("visible same-world discontinuities: %d\n", len(rows))
	for index, row := range rows {
		if index == 50 {
			break
		}
		fmt.Printf(
			"%s second=%d distance=%.3f kind=%s "+
				"op=%#x->%#x route=%s->%s\n",
			row.actorID,
			row.second,
			row.distance,
			row.classification,
			row.fromOperation,
			row.toOperation,
			row.fromRoute,
			row.toRoute,
		)
	}
}
