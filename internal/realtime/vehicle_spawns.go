package realtime

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// vehicle_spawns.json is the single authored source for persistent forklift
// and cargo spawns. Vite imports the same file for the browser client.
//
//go:embed vehicle_spawns.json
var vehicleSpawnManifestJSON []byte

type vehicleSpawnManifestFile struct {
	Schema               string                       `json:"schema"`
	DefaultForkliftModel string                       `json:"defaultForkliftModel"`
	Worlds               map[string]vehicleSpawnWorld `json:"worlds"`
}

type vehicleSpawnWorld struct {
	Forklifts    []vehicleForkliftSpawn `json:"forklifts"`
	Cargo        []vehicleCargoSpawn    `json:"cargo"`
	CargoEnabled bool                   `json:"cargoEnabled"`
}

type vehicleSpawnPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type vehicleForkliftSpawn struct {
	ID       string               `json:"id"`
	Model    string               `json:"model"`
	Position vehicleSpawnPosition `json:"position"`
	Yaw      float64              `json:"yaw"`
}

type vehicleCargoSpawn struct {
	ID       string               `json:"id"`
	Position vehicleSpawnPosition `json:"position"`
}

var vehicleSpawnManifest = mustLoadVehicleSpawnManifest()
var validForklifts = configuredForkliftIDs(vehicleSpawnManifest)

var cargoSpawnX, cargoSpawnY, cargoSpawnZ = configuredCargoPosition(
	vehicleSpawnManifest,
	"ma00",
	"cargo-job-1",
)
var cargoSpawn2X, _, cargoSpawn2Z = configuredCargoPosition(
	vehicleSpawnManifest,
	"ma00",
	"cargo-job-2",
)
var cargoSpawn3X, _, cargoSpawn3Z = configuredCargoPosition(
	vehicleSpawnManifest,
	"ma00",
	"cargo-job-3",
)

func mustLoadVehicleSpawnManifest() vehicleSpawnManifestFile {
	var manifest vehicleSpawnManifestFile
	if err := json.Unmarshal(vehicleSpawnManifestJSON, &manifest); err != nil {
		panic(fmt.Sprintf("parse vehicle spawn manifest: %v", err))
	}
	if manifest.Schema != "new-yokosuka-vehicle-spawns-v1" {
		panic(fmt.Sprintf("unsupported vehicle spawn schema %q", manifest.Schema))
	}
	if strings.TrimSpace(manifest.DefaultForkliftModel) == "" {
		panic("vehicle spawn manifest has no default forklift model")
	}
	ids := make(map[string]string)
	for worldID, world := range manifest.Worlds {
		if _, ok := validWorlds[worldID]; !ok {
			panic(fmt.Sprintf("vehicle spawn manifest has unknown world %q", worldID))
		}
		for _, spawn := range world.Forklifts {
			validateSpawnPosition(worldID, spawn.ID, spawn.Position)
			validateSpawnNumber(worldID, spawn.ID, "yaw", spawn.Yaw)
			if !strings.HasPrefix(spawn.ID, "forklift-") {
				panic(fmt.Sprintf("invalid forklift spawn ID %q", spawn.ID))
			}
			if previous, exists := ids[spawn.ID]; exists {
				panic(fmt.Sprintf(
					"duplicate vehicle spawn ID %q in %s and %s",
					spawn.ID,
					previous,
					worldID,
				))
			}
			ids[spawn.ID] = worldID
		}
		for _, spawn := range world.Cargo {
			validateSpawnPosition(worldID, spawn.ID, spawn.Position)
			if !cargoIDPattern.MatchString(spawn.ID) {
				panic(fmt.Sprintf("invalid cargo spawn ID %q", spawn.ID))
			}
			if previous, exists := ids[spawn.ID]; exists {
				panic(fmt.Sprintf(
					"duplicate vehicle spawn ID %q in %s and %s",
					spawn.ID,
					previous,
					worldID,
				))
			}
			ids[spawn.ID] = worldID
		}
	}
	return manifest
}

func validateSpawnPosition(
	worldID string,
	id string,
	position vehicleSpawnPosition,
) {
	validateSpawnNumber(worldID, id, "x", position.X)
	validateSpawnNumber(worldID, id, "y", position.Y)
	validateSpawnNumber(worldID, id, "z", position.Z)
}

func validateSpawnNumber(worldID string, id string, field string, value float64) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic(fmt.Sprintf(
			"vehicle spawn %q in %s has invalid %s",
			id,
			worldID,
			field,
		))
	}
}

func configuredForkliftIDs(
	manifest vehicleSpawnManifestFile,
) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, world := range manifest.Worlds {
		for _, spawn := range world.Forklifts {
			ids[spawn.ID] = struct{}{}
		}
	}
	return ids
}

func configuredCargoPosition(
	manifest vehicleSpawnManifestFile,
	worldID string,
	id string,
) (float64, float64, float64) {
	for _, spawn := range manifest.Worlds[worldID].Cargo {
		if spawn.ID == id {
			return spawn.Position.X, spawn.Position.Y, spawn.Position.Z
		}
	}
	panic(fmt.Sprintf("missing configured cargo spawn %q in %s", id, worldID))
}
