package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npc"
	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
	"github.com/brynnb/new-yokosuka-server/internal/worldstate"
	"github.com/gorilla/websocket"
)

type recordingChatSaver struct {
	messages     []store.ChatMessageLog
	history      []store.ChatMessageLog
	historyLimit int
	err          error
}

func (s *recordingChatSaver) SaveChatMessage(
	_ context.Context,
	message store.ChatMessageLog,
) error {
	s.messages = append(s.messages, message)
	return s.err
}

func (s *recordingChatSaver) RecentChatMessages(
	_ context.Context,
	limit int,
) ([]store.ChatMessageLog, error) {
	s.historyLimit = limit
	return s.history, s.err
}

type testRig struct {
	hub    *Hub
	server *httptest.Server
	wsURL  string
}

func newTestRig(t *testing.T, maxClients int) *testRig {
	t.Helper()
	epoch := time.UnixMilli(1_700_000_000_000)
	clock, err := worldstate.NewClock(epoch, "summer")
	if err != nil {
		t.Fatal(err)
	}
	clock.SetNowForTest(func() time.Time {
		return epoch.Add(7*time.Minute + 30*time.Second)
	})
	world := worldstate.NewManager(clock)
	hub := NewHub(maxClients, world, log.New(io.Discard, "", 0), nil)
	mux := http.NewServeMux()
	mux.Handle("/ws", NewWebSocketHandler(hub, nil))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &testRig{
		hub:    hub,
		server: server,
		wsURL:  "ws" + strings.TrimPrefix(server.URL, "http") + "/ws",
	}
}

func (r *testRig) connect(t *testing.T) (*websocket.Conn, protocol.Welcome) {
	t.Helper()
	headers := http.Header{"Origin": []string{r.server.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(r.wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var welcome protocol.Welcome
	readJSON(t, conn, &welcome)
	if welcome.Type != protocol.TypeWelcome || welcome.Version != protocol.Version {
		t.Fatalf("unexpected welcome: %#v", welcome)
	}
	if welcome.Self.ID == "" || welcome.Self.Name == "" {
		t.Fatalf("incomplete welcome: %#v", welcome)
	}
	if welcome.WorldState.TimeOfDay != "day" ||
		welcome.WorldState.DayLengthMs != worldstate.ShenmueDayLength.Milliseconds() {
		t.Fatalf("unexpected world state: %#v", welcome.WorldState)
	}
	return conn, welcome
}

func TestWelcomeIncludesRecentChatHistoryInDisplayOrder(t *testing.T) {
	rig := newTestRig(t, 10)
	saver := &recordingChatSaver{history: []store.ChatMessageLog{
		{
			PlayerID: "older-player", PlayerName: "Nozomi",
			WorldID: "dobuita", Text: "Older message",
			SentAt: time.UnixMilli(100),
		},
		{
			PlayerID: "newer-player", PlayerName: "Tom",
			WorldID: "yamanose", Text: "Newer message",
			SentAt: time.UnixMilli(200),
		},
	}}
	rig.hub.SetChatMessageSaver(saver)

	_, welcome := rig.connect(t)
	if saver.historyLimit != store.MaxRecentChatMessages {
		t.Fatalf("history limit = %d, want %d", saver.historyLimit, store.MaxRecentChatMessages)
	}
	if len(welcome.ChatHistory) != 2 ||
		welcome.ChatHistory[0].Text != "Older message" ||
		welcome.ChatHistory[1].Text != "Newer message" ||
		welcome.ChatHistory[1].SentAt != 200 {
		t.Fatalf("unexpected welcome chat history: %#v", welcome.ChatHistory)
	}
}

func readJSON(t *testing.T, conn *websocket.Conn, target any) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(target); err != nil {
		t.Fatal(err)
	}
}

func readSnapshot(t *testing.T, conn *websocket.Conn) protocol.Snapshot {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, body, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var header protocol.Header
		if err := json.Unmarshal(body, &header); err != nil {
			t.Fatal(err)
		}
		if header.Type != protocol.TypeSnapshot {
			continue
		}
		var snapshot protocol.Snapshot
		if err := json.Unmarshal(body, &snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
}

func readSystemMessage(t *testing.T, conn *websocket.Conn) protocol.SystemMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, body, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var header protocol.Header
		if err := json.Unmarshal(body, &header); err != nil {
			t.Fatal(err)
		}
		if header.Type != protocol.TypeSystemMessage {
			continue
		}
		var message protocol.SystemMessage
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatal(err)
		}
		return message
	}
}

func readClientCount(t *testing.T, conn *websocket.Conn, want int) {
	t.Helper()
	var event protocol.ClientCount
	readJSON(t, conn, &event)
	if event.Type != protocol.TypeClientCount ||
		event.ConnectedClients != want {
		t.Fatalf("unexpected connected-client count: %#v", event)
	}
}

func readPlayerEntered(
	t *testing.T,
	conn *websocket.Conn,
	wantID string,
	wantWorldID string,
) {
	t.Helper()
	var event protocol.PlayerEntered
	readJSON(t, conn, &event)
	if event.Type != protocol.TypePlayerEnter ||
		event.PlayerID != wantID ||
		event.WorldID != wantWorldID ||
		event.Name == "" {
		t.Fatalf("unexpected player-entry event: %#v", event)
	}
}

func sendPresence(t *testing.T, conn *websocket.Conn, worldID string, sequence uint64, animation *string, revision uint64) {
	t.Helper()
	if err := conn.WriteJSON(protocol.Presence{
		Header:            protocol.NewHeader(protocol.TypePresence),
		WorldID:           worldID,
		CharacterID:       "ryo",
		X:                 float64(sequence),
		Y:                 2,
		Z:                 3,
		Yaw:               1.25,
		Movement:          "walk",
		AnimationID:       animation,
		AnimationRevision: revision,
		Sequence:          sequence,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNewestConnectionReplacesTheSamePersistentCharacter(t *testing.T) {
	rig := newTestRig(t, 1)
	character := &store.Character{
		ID: 42, AccountID: 7, Name: "Test Player",
		AvatarKey: "ryo", WorldID: "dobuita", CurrentHP: 100,
	}
	connection := ConnectionMetadata{
		AccountID: 7, AccountType: "account", Character: character,
	}
	first, err := rig.hub.Register(nil, connection)
	if err != nil {
		t.Fatal(err)
	}
	<-first.send // welcome

	second, err := rig.hub.Register(nil, connection)
	if err != nil {
		t.Fatalf("replacement connection was rejected: %v", err)
	}
	<-second.send // welcome
	var replacement protocol.SessionReplaced
	if err := json.Unmarshal(<-first.replace, &replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.Type != protocol.TypeSessionReplaced ||
		replacement.Message == "" {
		t.Fatalf("unexpected replacement event: %#v", replacement)
	}
	if rig.hub.activeCharacters[character.ID] != second.id {
		t.Fatal("new connection did not take character ownership")
	}

	rig.hub.Unregister(first)
	if rig.hub.names[strings.ToLower(character.Name)] != second.id {
		t.Fatal("old disconnect removed the replacement's name reservation")
	}
	if rig.hub.activeCharacters[character.ID] != second.id {
		t.Fatal("old disconnect removed the replacement's character ownership")
	}
	var count protocol.ClientCount
	if err := json.Unmarshal(<-second.send, &count); err != nil {
		t.Fatal(err)
	}
	if count.Type != protocol.TypeClientCount {
		t.Fatalf("replacement received unexpected event: %#v", count)
	}
	select {
	case payload := <-second.send:
		t.Fatalf("session handoff announced a false departure: %s", payload)
	default:
	}
	rig.hub.Unregister(second)
}

func TestRoomSnapshotIncludesAuthoritativeNPCs(t *testing.T) {
	rig := newTestRig(t, 10)
	manifest, err := npcdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := npc.NewEngine(manifest)
	if err != nil {
		t.Fatal(err)
	}
	world := rig.hub.world.Snapshot()
	if _, err := engine.Tick(npc.TickTime{
		ServerTime: time.UnixMilli(world.ServerTimeMs),
		GameTime:   time.UnixMilli(world.GameTimeMs),
		DayNumber:  world.DayNumber,
		DayLength:  time.Duration(world.DayLengthMs) * time.Millisecond,
	}, nil); err != nil {
		t.Fatal(err)
	}
	rig.hub.SetNPCEngine(engine)

	conn, _ := rig.connect(t)
	sendPresence(t, conn, "dobuita", 1, nil, 0)
	var snapshot protocol.Snapshot
	readJSON(t, conn, &snapshot)
	if snapshot.Type != protocol.TypeSnapshot || len(snapshot.NPCs) == 0 {
		t.Fatalf("snapshot did not contain NPCs: %#v", snapshot)
	}
	for _, state := range snapshot.NPCs {
		if state.WorldID != "dobuita" || state.ID == "" || state.Revision == 0 {
			t.Fatalf("invalid NPC snapshot state: %#v", state)
		}
	}
}

func TestMultipleClientsAndReconnectReceiveTheSameNPCRevision(t *testing.T) {
	rig := newTestRig(t, 10)
	manifest, err := npcdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := npc.NewEngine(manifest)
	if err != nil {
		t.Fatal(err)
	}
	world := rig.hub.world.Snapshot()
	if _, err := engine.Tick(npc.TickTime{
		ServerTime: time.UnixMilli(world.ServerTimeMs),
		GameTime:   time.UnixMilli(world.GameTimeMs),
		DayNumber:  world.DayNumber,
		DayLength:  time.Duration(world.DayLengthMs) * time.Millisecond,
	}, nil); err != nil {
		t.Fatal(err)
	}
	rig.hub.SetNPCEngine(engine)

	first, _ := rig.connect(t)
	sendPresence(t, first, "dobuita", 1, nil, 0)
	firstSnapshot := readSnapshot(t, first)
	_ = first.Close()

	reconnected, _ := rig.connect(t)
	sendPresence(t, reconnected, "dobuita", 1, nil, 0)
	secondSnapshot := readSnapshot(t, reconnected)
	if len(firstSnapshot.NPCs) == 0 ||
		len(firstSnapshot.NPCs) != len(secondSnapshot.NPCs) {
		t.Fatalf(
			"NPC snapshot sizes differ: %d and %d",
			len(firstSnapshot.NPCs),
			len(secondSnapshot.NPCs),
		)
	}
	firstRevisions := make(map[string]uint64, len(firstSnapshot.NPCs))
	for _, state := range firstSnapshot.NPCs {
		firstRevisions[state.ID] = state.Revision
	}
	for _, state := range secondSnapshot.NPCs {
		if firstRevisions[state.ID] != state.Revision {
			t.Fatalf(
				"NPC %s revision changed across reconnect: %d -> %d",
				state.ID,
				firstRevisions[state.ID],
				state.Revision,
			)
		}
	}
}

func TestForkliftClaimsAreExclusiveAndReleasedWithTheWorld(t *testing.T) {
	rig := newTestRig(t, 10)
	first, firstWelcome := rig.connect(t)
	second, secondWelcome := rig.connect(t)
	readClientCount(t, first, 2)

	forkliftID := "forklift-1"
	firstPresence := protocol.Presence{
		Header:           protocol.NewHeader(protocol.TypePresence),
		WorldID:          "ma00",
		CharacterID:      "ryo",
		X:                1.6,
		Z:                29,
		Yaw:              3.14,
		Movement:         "idle",
		Sequence:         1,
		VehicleID:        &forkliftID,
		VehicleLift:      3.1776,
		VehicleSteering:  0.2,
		VehicleWheelRoll: 3,
		VehicleQX:        0.1,
		VehicleQY:        0.2,
		VehicleQZ:        0.3,
		VehicleQW:        0.9,
	}
	if err := first.WriteJSON(firstPresence); err != nil {
		t.Fatal(err)
	}
	var snapshot protocol.Snapshot
	readJSON(t, first, &snapshot)
	if len(snapshot.Forklifts) != 5 {
		t.Fatalf("expected five server forklifts, got %#v", snapshot.Forklifts)
	}
	readPlayerEntered(t, second, firstWelcome.Self.ID, "ma00")

	secondPresence := firstPresence
	secondPresence.Sequence = 1
	if err := second.WriteJSON(secondPresence); err != nil {
		t.Fatal(err)
	}
	readJSON(t, second, &snapshot)
	if len(snapshot.Players) != 1 ||
		snapshot.Players[0].VehicleID == nil ||
		*snapshot.Players[0].VehicleID != forkliftID ||
		snapshot.Players[0].VehicleLift != 3.1776 {
		t.Fatalf("snapshot did not retain authoritative forklift owner: %#v", snapshot)
	}

	rig.hub.mu.RLock()
	owner := rig.hub.forkliftOwners[forkliftID]
	secondState := rig.hub.presences[secondWelcome.Self.ID]
	rig.hub.mu.RUnlock()
	if owner != firstWelcome.Self.ID {
		t.Fatalf("forklift owner changed after a contested claim: %q", owner)
	}
	if secondState.state.VehicleID != nil {
		t.Fatalf("contested player retained forklift claim: %#v", secondState.state)
	}
	rig.hub.mu.RLock()
	firstState := rig.hub.presences[firstWelcome.Self.ID].state
	forklift := rig.hub.forklifts[forkliftID]
	rig.hub.mu.RUnlock()
	playerOrientationLength := math.Sqrt(
		firstState.VehicleQX*firstState.VehicleQX +
			firstState.VehicleQY*firstState.VehicleQY +
			firstState.VehicleQZ*firstState.VehicleQZ +
			firstState.VehicleQW*firstState.VehicleQW,
	)
	forkliftOrientationLength := math.Sqrt(
		forklift.QX*forklift.QX +
			forklift.QY*forklift.QY +
			forklift.QZ*forklift.QZ +
			forklift.QW*forklift.QW,
	)
	if math.Abs(playerOrientationLength-1) > 1e-12 ||
		math.Abs(forkliftOrientationLength-1) > 1e-12 ||
		forklift.QZ == 0 {
		t.Fatalf(
			"full forklift orientation was not normalized and retained: player=%#v forklift=%#v",
			firstState,
			forklift,
		)
	}

	if err := first.WriteJSON(protocol.LeaveWorld{
		Header: protocol.NewHeader(protocol.TypeLeaveWorld),
	}); err != nil {
		t.Fatal(err)
	}
	var left protocol.PlayerLeft
	readJSON(t, second, &left)
	rig.hub.mu.RLock()
	_, stillOwned := rig.hub.forkliftOwners[forkliftID]
	rig.hub.mu.RUnlock()
	if stillOwned {
		t.Fatal("forklift was not returned after its driver left the world")
	}
}

func TestForkliftSoundEventsRequireOwnershipAndStayInWorld(t *testing.T) {
	rig := newTestRig(t, 10)
	owner := &Client{id: "sound-owner"}
	listener := &Client{
		id:   "sound-listener",
		send: make(chan []byte, 4),
		done: make(chan struct{}),
	}
	otherWorld := &Client{
		id:   "other-world",
		send: make(chan []byte, 4),
		done: make(chan struct{}),
	}
	forkliftID := "forklift-1"
	rig.hub.mu.Lock()
	rig.hub.presences[owner.id] = storedPresence{
		state: protocol.PlayerState{
			WorldID:   "ma00",
			VehicleID: &forkliftID,
		},
	}
	rig.hub.forkliftOwners[forkliftID] = owner.id
	state := rig.hub.forklifts[forkliftID]
	state.OwnerID = owner.id
	rig.hub.forklifts[forkliftID] = state
	rig.hub.rooms["ma00"] = map[string]*Client{
		owner.id:    owner,
		listener.id: listener,
	}
	rig.hub.rooms["dobuita"] = map[string]*Client{
		otherWorld.id: otherWorld,
	}
	rig.hub.mu.Unlock()

	if !rig.hub.HandleForkliftSound(owner, protocol.ForkliftSound{
		ForkliftID: forkliftID,
		Cue:        "horn",
	}) {
		t.Fatal("owned forklift horn was rejected")
	}
	var event protocol.ForkliftSound
	if err := json.Unmarshal(<-listener.send, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.TypeForkliftSound ||
		event.ForkliftID != forkliftID ||
		event.Cue != "horn" {
		t.Fatalf("unexpected forklift sound event: %#v", event)
	}
	select {
	case event := <-otherWorld.send:
		t.Fatalf("forklift sound escaped its world: %s", event)
	default:
	}
	if rig.hub.HandleForkliftSound(owner, protocol.ForkliftSound{
		ForkliftID: forkliftID,
		Cue:        "startup",
	}) {
		t.Fatal("client-originated startup cue was accepted")
	}
	if rig.hub.HandleForkliftSound(
		&Client{id: "impostor"},
		protocol.ForkliftSound{ForkliftID: forkliftID, Cue: "horn"},
	) {
		t.Fatal("unowned forklift horn was accepted")
	}
}

func TestForkliftSoundRateLimitAllowsNormalUseAndStopsFloods(t *testing.T) {
	client := &Client{}
	now := time.Unix(1_700_000_000, 0)
	for index := 0; index < 8; index++ {
		if !client.allowForkliftSound(now) {
			t.Fatalf("normal cue %d was rejected", index+1)
		}
	}
	if client.allowForkliftSound(now) {
		t.Fatal("ninth forklift sound inside one second was accepted")
	}
	if !client.allowForkliftSound(now.Add(time.Second + time.Millisecond)) {
		t.Fatal("forklift sound limit did not recover")
	}
}

func TestCargoClaimTransfersAfterHalfSecondWithoutContact(t *testing.T) {
	rig := newTestRig(t, 10)
	first := newClient(
		rig.hub, nil, "cargo-first", "Guest1111",
		ConnectionMetadata{},
	)
	second := newClient(
		rig.hub, nil, "cargo-second", "Guest2222",
		ConnectionMetadata{},
	)
	firstForklift := "forklift-1"
	secondForklift := "forklift-2"
	now := time.Now().UnixMilli()
	rig.hub.mu.Lock()
	rig.hub.clients[first.id] = first
	rig.hub.clients[second.id] = second
	rig.hub.rooms["ma00"] = map[string]*Client{
		first.id:  first,
		second.id: second,
	}
	rig.hub.presences[first.id] = storedPresence{state: protocol.PlayerState{
		ID: first.id, WorldID: "ma00",
		X: cargoSpawnX, Y: cargoSpawnY, Z: cargoSpawnZ,
		VehicleID: &firstForklift, UpdatedAt: now,
	}}
	rig.hub.presences[second.id] = storedPresence{state: protocol.PlayerState{
		ID: second.id, WorldID: "ma00",
		X: cargoSpawnX, Y: cargoSpawnY, Z: cargoSpawnZ,
		VehicleID: &secondForklift, UpdatedAt: now,
	}}
	rig.hub.mu.Unlock()

	if !rig.hub.HandleCargoClaim(first, protocol.CargoClaim{
		CargoID: "cargo-job-1",
	}) {
		t.Fatal("first forklift could not claim unowned cargo")
	}
	if rig.hub.HandleCargoClaim(second, protocol.CargoClaim{
		CargoID: "cargo-job-1",
	}) {
		t.Fatal("second forklift stole an active cargo claim")
	}
	if !rig.hub.HandleCargoUpdate(first, protocol.CargoUpdate{
		CargoState: protocol.CargoState{
			ID: "cargo-job-1",
			X:  cargoSpawnX + 0.1, Y: cargoSpawnY + 0.1, Z: cargoSpawnZ,
			QW: 1, Sleeping: false,
		},
		Touching: false,
	}) {
		t.Fatal("cargo owner could not publish its release pose")
	}
	rig.hub.mu.RLock()
	releasing := rig.hub.cargo["cargo-job-1"]
	rig.hub.mu.RUnlock()
	remaining := time.Until(time.UnixMilli(releasing.ClaimExpiresAtMs))
	if remaining <= 0 || remaining > cargoClaimReleaseDelay {
		t.Fatalf("unexpected cargo release grace: %v", remaining)
	}
	if rig.hub.HandleCargoClaim(second, protocol.CargoClaim{
		CargoID: "cargo-job-1",
	}) {
		t.Fatal("cargo transferred before its release grace elapsed")
	}

	time.Sleep(cargoClaimReleaseDelay + 50*time.Millisecond)
	if !rig.hub.HandleCargoClaim(second, protocol.CargoClaim{
		CargoID: "cargo-job-1",
	}) {
		t.Fatal("second forklift could not claim cargo after release")
	}
	rig.hub.mu.RLock()
	owner := rig.hub.cargo["cargo-job-1"].OwnerID
	rig.hub.mu.RUnlock()
	if owner != second.id {
		t.Fatalf("cargo owner = %q, want %q", owner, second.id)
	}
}

func TestCargoReleaseGraceStartsWhenDriverLeavesWorld(t *testing.T) {
	rig := newTestRig(t, 10)
	driver := newClient(
		rig.hub,
		nil,
		"cargo-warp-driver",
		"Guest3333",
		ConnectionMetadata{},
	)
	forkliftID := "forklift-1"
	now := time.Now().UnixMilli()
	rig.hub.mu.Lock()
	rig.hub.clients[driver.id] = driver
	rig.hub.rooms["ma00"] = map[string]*Client{driver.id: driver}
	rig.hub.presences[driver.id] = storedPresence{
		state: protocol.PlayerState{
			ID: driver.id, WorldID: "ma00",
			X: cargoSpawnX, Y: cargoSpawnY, Z: cargoSpawnZ,
			VehicleID: &forkliftID, UpdatedAt: now,
		},
	}
	cargo := rig.hub.cargo["cargo-job-1"]
	cargo.OwnerID = driver.id
	rig.hub.cargo[cargo.ID] = cargo
	rig.hub.mu.Unlock()

	rig.hub.LeaveWorld(driver)

	rig.hub.mu.RLock()
	releasing := rig.hub.cargo["cargo-job-1"]
	rig.hub.mu.RUnlock()
	remaining := time.Until(time.UnixMilli(releasing.ClaimExpiresAtMs))
	if releasing.OwnerID != driver.id ||
		remaining <= 0 ||
		remaining > cargoClaimReleaseDelay {
		t.Fatalf("leaving world did not start cargo grace: %#v", releasing)
	}
}

func TestCargoReplenishesAfterThreeContinuousSecondsAway(t *testing.T) {
	rig := newTestRig(t, 10)
	start := time.Unix(1_800_000_000, 0)
	rig.hub.mu.Lock()
	source := rig.hub.cargo["cargo-job-1"]
	source.X = cargoSpawnX + cargoSpawnRadius + 0.1
	rig.hub.cargo[source.ID] = source
	firstSince, started := rig.hub.updateCargoReplenishmentLocked(
		source,
		start,
	)
	if !started {
		rig.hub.mu.Unlock()
		t.Fatal("moving cargo outside the spawn radius did not start timer")
	}
	_, _, spawnedEarly := rig.hub.replenishCargoLocked(
		source.ID,
		firstSince,
		start.Add(cargoReplenishDelay-time.Millisecond),
	)
	if spawnedEarly {
		rig.hub.mu.Unlock()
		t.Fatal("cargo replenished before three seconds elapsed")
	}

	source.X = cargoSpawnX
	rig.hub.cargo[source.ID] = source
	rig.hub.updateCargoReplenishmentLocked(source, start.Add(2*time.Second))
	source.X = cargoSpawnX + cargoSpawnRadius + 0.1
	rig.hub.cargo[source.ID] = source
	secondSince, restarted := rig.hub.updateCargoReplenishmentLocked(
		source,
		start.Add(2500*time.Millisecond),
	)
	if !restarted {
		rig.hub.mu.Unlock()
		t.Fatal("cargo timer did not restart after returning to spawn")
	}
	_, _, staleSpawned := rig.hub.replenishCargoLocked(
		source.ID,
		firstSince,
		start.Add(4*time.Second),
	)
	if staleSpawned {
		rig.hub.mu.Unlock()
		t.Fatal("stale timer replenished cargo after continuity was broken")
	}
	replacement, _, spawned := rig.hub.replenishCargoLocked(
		source.ID,
		secondSince,
		secondSince.Add(cargoReplenishDelay),
	)
	rig.hub.mu.Unlock()
	if !spawned {
		t.Fatal("cargo did not replenish after three continuous seconds")
	}
	if replacement.ID != "cargo-job-4" ||
		replacement.X != cargoSpawnX ||
		replacement.Y != cargoSpawnY ||
		replacement.Z != cargoSpawnZ {
		t.Fatalf("unexpected replacement cargo: %#v", replacement)
	}
}

func TestCargoReplacementUsesItsOwnPlaygroundSpawn(t *testing.T) {
	rig := newTestRig(t, 10)
	now := time.Unix(1_800_000_000, 0)
	rig.hub.mu.Lock()
	source := rig.hub.cargo["cargo-job-2"]
	source.X = cargoSpawn2X + cargoSpawnRadius + 0.1
	rig.hub.cargo[source.ID] = source
	awaySince, started := rig.hub.updateCargoReplenishmentLocked(source, now)
	if !started {
		rig.hub.mu.Unlock()
		t.Fatal("moving second cargo outside its spawn radius did not start timer")
	}
	replacement, _, spawned := rig.hub.replenishCargoLocked(
		source.ID,
		awaySince,
		now.Add(cargoReplenishDelay),
	)
	rig.hub.mu.Unlock()
	if !spawned {
		t.Fatal("second cargo did not replenish")
	}
	if replacement.X != cargoSpawn2X ||
		replacement.Y != cargoSpawnY ||
		replacement.Z != cargoSpawn2Z {
		t.Fatalf("replacement did not use second cargo spawn: %#v", replacement)
	}
}

func TestCargoAutoRightsOnlyWhenItsBottomNoLongerFacesDown(t *testing.T) {
	rig := newTestRig(t, 10)
	start := time.Unix(1_800_000_000, 0)
	rig.hub.mu.Lock()
	state := rig.hub.cargo["cargo-job-1"]
	state.QX = math.Sin(math.Pi / 4)
	state.QW = math.Cos(math.Pi / 4)
	state.AngularVelocityZ = 3
	rig.hub.cargo[state.ID] = state
	untouchedSince, started := rig.hub.updateCargoAutoRightLocked(
		state,
		false,
		start,
	)
	if !started {
		rig.hub.mu.Unlock()
		t.Fatal("losing forklift contact did not start auto-right timer")
	}
	_, _, changedEarly := rig.hub.autoRightCargoLocked(
		state.ID,
		untouchedSince,
		start.Add(cargoAutoRightDelay-time.Millisecond),
	)
	if changedEarly {
		rig.hub.mu.Unlock()
		t.Fatal("cargo auto-righted before two seconds elapsed")
	}
	righted, _, changed := rig.hub.autoRightCargoLocked(
		state.ID,
		untouchedSince,
		start.Add(cargoAutoRightDelay),
	)
	rig.hub.mu.Unlock()
	if !changed {
		t.Fatal("sideways cargo did not auto-right after two seconds")
	}
	if math.Abs(righted.QX) > 1e-12 ||
		math.Abs(righted.QZ) > 1e-12 ||
		math.Abs(righted.QW-1) > 1e-12 ||
		righted.AngularVelocityZ != 0 ||
		!righted.AutoRight ||
		!righted.Sleeping {
		t.Fatalf("unexpected auto-right state: %#v", righted)
	}

	mostlyUpright := protocol.CargoState{
		QX: math.Sin(math.Pi / 12),
		QW: math.Cos(math.Pi / 12),
	}
	if !cargoBottomFacesDown(mostlyUpright) {
		t.Fatal("a crate tilted only 30 degrees should remain untouched")
	}
}

func TestDisconnectRetainsForkliftForFiveMinutes(t *testing.T) {
	rig := newTestRig(t, 10)
	conn, welcome := rig.connect(t)
	forkliftID := "forklift-2"
	if err := conn.WriteJSON(protocol.Presence{
		Header:      protocol.NewHeader(protocol.TypePresence),
		WorldID:     "ma00",
		CharacterID: "ryo",
		X:           12,
		Y:           1,
		Z:           34,
		Yaw:         2,
		Movement:    "idle",
		Sequence:    1,
		VehicleID:   &forkliftID,
	}); err != nil {
		t.Fatal(err)
	}
	var snapshot protocol.Snapshot
	readJSON(t, conn, &snapshot)
	if err := conn.WriteJSON(protocol.ForkliftUpdate{
		Header:   protocol.NewHeader(protocol.TypeForkliftUpdate),
		Righting: true,
		ForkliftState: protocol.ForkliftState{
			ID:               forkliftID,
			X:                12,
			Y:                1,
			Z:                34,
			Yaw:              2,
			VelocityX:        8,
			VelocityY:        3,
			VelocityZ:        -4,
			AngularVelocityX: 1,
			AngularVelocityY: 2,
			AngularVelocityZ: 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var moving protocol.ForkliftStateEvent
	readJSON(t, conn, &moving)
	rightingRemaining := time.Until(
		time.UnixMilli(moving.Forklift.RightingUntilMs),
	)
	if rightingRemaining < 500*time.Millisecond ||
		rightingRemaining > time.Second {
		t.Fatalf(
			"unexpected forklift righting collision window: %v",
			rightingRemaining,
		)
	}
	_ = conn.Close()

	deadline := time.Now().Add(time.Second)
	for {
		rig.hub.mu.RLock()
		_, connected := rig.hub.clients[welcome.Self.ID]
		forklift := rig.hub.forklifts[forkliftID]
		_, owned := rig.hub.forkliftOwners[forkliftID]
		rig.hub.mu.RUnlock()
		if !connected {
			if owned {
				t.Fatal("disconnected forklift retained an owner")
			}
			remaining := time.Until(time.UnixMilli(forklift.ExpiresAtMs))
			if remaining < 4*time.Minute+59*time.Second ||
				remaining > abandonedForkliftLifetime {
				t.Fatalf("unexpected forklift retention: %v", remaining)
			}
			if forklift.X != 12 || forklift.Y != 1 || forklift.Z != 34 {
				t.Fatalf("forklift pose was not retained: %#v", forklift)
			}
			if forklift.VelocityX != 0 ||
				forklift.VelocityY != 0 ||
				forklift.VelocityZ != 0 ||
				forklift.AngularVelocityX != 0 ||
				forklift.AngularVelocityY != 0 ||
				forklift.AngularVelocityZ != 0 {
				t.Fatalf(
					"disconnected forklift retained stale momentum: %#v",
					forklift,
				)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnect was not processed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSecretForkliftSpawnCreatesAWorldScopedServerEntity(t *testing.T) {
	rig := newTestRig(t, 10)
	conn, _ := rig.connect(t)
	sendPresence(t, conn, "yamanose", 1, nil, 0)
	var snapshot protocol.Snapshot
	readJSON(t, conn, &snapshot)
	if len(snapshot.Forklifts) != 0 {
		t.Fatalf("unexpected forklifts before spawn: %#v", snapshot.Forklifts)
	}
	if err := conn.WriteJSON(protocol.ForkliftSpawn{
		Header: protocol.NewHeader(protocol.TypeForkliftSpawn),
		X:      12,
		Y:      3,
		Z:      45,
		Yaw:    1.5,
	}); err != nil {
		t.Fatal(err)
	}
	var event protocol.ForkliftStateEvent
	readJSON(t, conn, &event)
	if event.Type != protocol.TypeForkliftState ||
		!dynamicForkliftPattern.MatchString(event.Forklift.ID) ||
		event.Forklift.WorldID != "yamanose" ||
		event.Forklift.X != 12 ||
		event.Forklift.Y != 3 ||
		event.Forklift.Z != 45 {
		t.Fatalf("unexpected spawned forklift: %#v", event)
	}
}

func TestSecretForkliftSpawnLimitsDynamicForkliftsByWorld(t *testing.T) {
	tests := []struct {
		name    string
		worldID string
		limit   int
	}{
		{name: "harbor", worldID: "mfsy", limit: 20},
		{name: "forklift race", worldID: "ma00race", limit: 20},
		{name: "other world", worldID: "dobuita", limit: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rig := newTestRig(t, 10)
			client := &Client{id: "spawn-limit-client"}
			rig.hub.mu.Lock()
			rig.hub.presences[client.id] = storedPresence{
				state: protocol.PlayerState{WorldID: test.worldID},
			}
			rig.hub.mu.Unlock()

			for index := 0; index < test.limit; index++ {
				if !rig.hub.HandleForkliftSpawn(client, protocol.ForkliftSpawn{
					X: float64(index),
				}) {
					t.Fatalf(
						"spawn %d of %d was unexpectedly rejected",
						index+1,
						test.limit,
					)
				}
			}
			if rig.hub.HandleForkliftSpawn(client, protocol.ForkliftSpawn{
				X: float64(test.limit),
			}) {
				t.Fatalf("spawn beyond the limit of %d was accepted", test.limit)
			}

			rig.hub.mu.RLock()
			dynamicCount := rig.hub.dynamicForkliftCountLocked(test.worldID)
			rig.hub.mu.RUnlock()
			if dynamicCount != test.limit {
				t.Fatalf(
					"expected %d dynamic forklifts, got %d",
					test.limit,
					dynamicCount,
				)
			}
		})
	}
}

func TestSecretForkliftSpawnDoesNotCountAuthoredForklifts(t *testing.T) {
	rig := newTestRig(t, 10)
	client := &Client{id: "authored-forklift-client"}
	rig.hub.mu.Lock()
	rig.hub.presences[client.id] = storedPresence{
		state: protocol.PlayerState{WorldID: "ma00race"},
	}
	rig.hub.mu.Unlock()

	for index := 0; index < raceDynamicForkliftSpawnLimit; index++ {
		if !rig.hub.HandleForkliftSpawn(client, protocol.ForkliftSpawn{
			X: float64(index),
		}) {
			t.Fatalf(
				"authored race forklifts incorrectly consumed spawn slot %d",
				index+1,
			)
		}
	}
}

func TestRoomSnapshotFanoutTransitionAndDisconnect(t *testing.T) {
	rig := newTestRig(t, 10)
	first, firstWelcome := rig.connect(t)
	second, secondWelcome := rig.connect(t)
	readClientCount(t, first, 2)
	if firstWelcome.ConnectedClients != 1 ||
		secondWelcome.ConnectedClients != 2 {
		t.Fatalf("unexpected welcome counts: %d, %d",
			firstWelcome.ConnectedClients,
			secondWelcome.ConnectedClients,
		)
	}
	if firstWelcome.Self.Name == secondWelcome.Self.Name {
		t.Fatal("guest names must be unique")
	}

	sendPresence(t, first, "dobuita", 1, nil, 0)
	var firstSnapshot protocol.Snapshot
	readJSON(t, first, &firstSnapshot)
	if len(firstSnapshot.Players) != 0 {
		t.Fatalf("expected empty initial snapshot, got %#v", firstSnapshot)
	}
	readPlayerEntered(t, second, firstWelcome.Self.ID, "dobuita")

	sendPresence(t, second, "dobuita", 1, nil, 0)
	var secondSnapshot protocol.Snapshot
	readJSON(t, second, &secondSnapshot)
	if len(secondSnapshot.Players) != 1 ||
		secondSnapshot.Players[0].ID != firstWelcome.Self.ID {
		t.Fatalf("expected first player in snapshot, got %#v", secondSnapshot)
	}
	var joined protocol.PlayerStateEvent
	readJSON(t, first, &joined)
	if joined.Player.ID != secondWelcome.Self.ID || joined.Player.CharacterID != "ryo" {
		t.Fatalf("unexpected joined state: %#v", joined)
	}
	readPlayerEntered(t, first, secondWelcome.Self.ID, "dobuita")

	sendPresence(t, second, "yamanose", 2, nil, 0)
	var left protocol.PlayerLeft
	readJSON(t, first, &left)
	if left.PlayerID != secondWelcome.Self.ID {
		t.Fatalf("unexpected leave: %#v", left)
	}
	readJSON(t, second, &secondSnapshot)
	if len(secondSnapshot.Players) != 0 {
		t.Fatalf("new room should be empty: %#v", secondSnapshot)
	}

	sendPresence(t, second, "dobuita", 3, nil, 0)
	readJSON(t, second, &secondSnapshot)
	joined = protocol.PlayerStateEvent{}
	readJSON(t, first, &joined)
	if err := second.WriteJSON(protocol.LeaveWorld{
		Header: protocol.NewHeader(protocol.TypeLeaveWorld),
	}); err != nil {
		t.Fatal(err)
	}
	readJSON(t, first, &left)
	if left.PlayerID != secondWelcome.Self.ID {
		t.Fatalf("explicit leave did not remove second player: %#v", left)
	}

	sendPresence(t, second, "dobuita", 4, nil, 0)
	readJSON(t, second, &secondSnapshot)
	joined = protocol.PlayerStateEvent{}
	readJSON(t, first, &joined)
	_ = second.Close()
	readJSON(t, first, &left)
	if left.PlayerID != secondWelcome.Self.ID {
		t.Fatalf("disconnect did not remove second player: %#v", left)
	}
	departure := readSystemMessage(t, first)
	if departure.Text != secondWelcome.Self.Name+" has left the game." ||
		departure.SentAt <= 0 {
		t.Fatalf("unexpected departure announcement: %#v", departure)
	}
}

func TestMapChangesDoNotRepeatTheInitialEntryAnnouncement(t *testing.T) {
	rig := newTestRig(t, 10)
	observer, _ := rig.hub.Register(nil, ConnectionMetadata{})
	mover, _ := rig.hub.Register(nil, ConnectionMetadata{})
	<-observer.send // welcome
	<-observer.send // client count after mover connects
	<-mover.send    // welcome

	firstPresence := protocol.Presence{
		Header:      protocol.NewHeader(protocol.TypePresence),
		WorldID:     "dobuita",
		CharacterID: "ryo",
		Movement:    "idle",
		Sequence:    1,
		VehicleQW:   1,
	}
	if !rig.hub.HandlePresence(observer, firstPresence) {
		t.Fatal("observer presence was rejected")
	}
	<-observer.send // snapshot
	<-mover.send    // observer's initial player_entered

	if !rig.hub.HandlePresence(mover, firstPresence) {
		t.Fatal("mover presence was rejected")
	}
	<-mover.send    // snapshot
	<-observer.send // player state
	var initialEntry protocol.PlayerEntered
	if err := json.Unmarshal(<-observer.send, &initialEntry); err != nil {
		t.Fatal(err)
	}
	if initialEntry.Type != protocol.TypePlayerEnter ||
		initialEntry.PlayerID != mover.id {
		t.Fatalf("unexpected initial entry: %#v", initialEntry)
	}

	rig.hub.LeaveWorld(mover)
	<-observer.send // player_left presence cleanup, not a chat announcement
	reentered := firstPresence
	reentered.WorldID = "yamanose"
	reentered.Sequence = 2
	if !rig.hub.HandlePresence(mover, reentered) {
		t.Fatal("map re-entry presence was rejected")
	}
	<-mover.send // new-room snapshot
	select {
	case payload := <-observer.send:
		t.Fatalf("map change repeated initial entry announcement: %s", payload)
	default:
	}

	rig.hub.Unregister(observer)
	rig.hub.Unregister(mover)
}

func TestAnimationElapsedInLateSnapshot(t *testing.T) {
	rig := newTestRig(t, 10)
	first, firstWelcome := rig.connect(t)
	bow := "bow"
	sendPresence(t, first, "dobuita", 1, &bow, 7)
	var empty protocol.Snapshot
	readJSON(t, first, &empty)
	time.Sleep(30 * time.Millisecond)

	second, _ := rig.connect(t)
	sendPresence(t, second, "dobuita", 1, nil, 0)
	var snapshot protocol.Snapshot
	readJSON(t, second, &snapshot)
	if len(snapshot.Players) != 1 || snapshot.Players[0].ID != firstWelcome.Self.ID {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	state := snapshot.Players[0]
	if state.AnimationID == nil || *state.AnimationID != "bow" ||
		state.AnimationRevision != 7 || state.AnimationElapsedMs < 20 {
		t.Fatalf("animation timing was not preserved: %#v", state)
	}
}

func TestRoomChatAndCapabilityRename(t *testing.T) {
	rig := newTestRig(t, 10)
	chatSaver := &recordingChatSaver{}
	rig.hub.SetChatMessageSaver(chatSaver)
	first, firstWelcome := rig.connect(t)
	second, secondWelcome := rig.connect(t)
	readClientCount(t, first, 2)
	sendPresence(t, first, "dobuita", 1, nil, 0)
	var snapshot protocol.Snapshot
	readJSON(t, first, &snapshot)
	readPlayerEntered(t, second, firstWelcome.Self.ID, "dobuita")
	sendPresence(t, second, "yamanose", 1, nil, 0)
	readJSON(t, second, &snapshot)
	readPlayerEntered(t, first, secondWelcome.Self.ID, "yamanose")

	if err := first.WriteJSON(protocol.ChatRequest{
		Header: protocol.NewHeader(protocol.TypeChat),
		Text:   "  Hello   Yokosuka!  ",
	}); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{first, second} {
		var event protocol.ChatEvent
		readJSON(t, conn, &event)
		if event.Message.Text != "Hello Yokosuka!" ||
			event.Message.PlayerID != firstWelcome.Self.ID ||
			event.Message.Name != firstWelcome.Self.Name ||
			event.Message.WorldID != "dobuita" {
			t.Fatalf("unexpected chat event: %#v", event)
		}
	}
	if len(chatSaver.messages) != 1 {
		t.Fatalf("saved chat count = %d, want 1", len(chatSaver.messages))
	}
	savedChat := chatSaver.messages[0]
	if savedChat.PlayerID != firstWelcome.Self.ID ||
		savedChat.PlayerName != firstWelcome.Self.Name ||
		savedChat.WorldID != "dobuita" ||
		savedChat.Text != "Hello Yokosuka!" ||
		savedChat.SentAt.IsZero() {
		t.Fatalf("unexpected saved chat: %#v", savedChat)
	}
}

func TestChatRateLimitAndWorldRequirement(t *testing.T) {
	rig := newTestRig(t, 10)
	conn, _ := rig.connect(t)
	if err := conn.WriteJSON(protocol.ChatRequest{
		Header: protocol.NewHeader(protocol.TypeChat),
		Text:   "hello",
	}); err != nil {
		t.Fatal(err)
	}
	var rejected protocol.ChatRejected
	readJSON(t, conn, &rejected)
	if rejected.Type != protocol.TypeChatRejected {
		t.Fatalf("expected rejection outside world: %#v", rejected)
	}

	sendPresence(t, conn, "dobuita", 1, nil, 0)
	var snapshot protocol.Snapshot
	readJSON(t, conn, &snapshot)
	if err := conn.WriteJSON(protocol.ChatRequest{
		Header: protocol.NewHeader(protocol.TypeChat),
		Text:   "first",
	}); err != nil {
		t.Fatal(err)
	}
	var chat protocol.ChatEvent
	readJSON(t, conn, &chat)
	for _, text := range []string{"second", "third"} {
		if err := conn.WriteJSON(protocol.ChatRequest{
			Header: protocol.NewHeader(protocol.TypeChat),
			Text:   text,
		}); err != nil {
			t.Fatal(err)
		}
		readJSON(t, conn, &chat)
	}
	if err := conn.WriteJSON(protocol.ChatRequest{
		Header: protocol.NewHeader(protocol.TypeChat),
		Text:   "fourth",
	}); err != nil {
		t.Fatal(err)
	}
	readJSON(t, conn, &rejected)
	if !strings.Contains(rejected.Reason, "3 messages") {
		t.Fatalf("unexpected rate-limit reason: %#v", rejected)
	}
}

func TestChatSlidingWindowLimits(t *testing.T) {
	client := &Client{}
	now := time.Unix(1_700_000_000, 0)
	for offset := 0; offset < 3; offset++ {
		if allowed, reason := client.allowChat(
			now.Add(time.Duration(offset) * time.Second),
		); !allowed {
			t.Fatalf("message %d should be allowed: %s", offset+1, reason)
		}
	}
	if allowed, reason := client.allowChat(now.Add(3 * time.Second)); allowed ||
		!strings.Contains(reason, "3 messages") {
		t.Fatalf("expected 10-second limit, got allowed=%v reason=%q", allowed, reason)
	}
	for _, offset := range []time.Duration{
		4 * time.Second,
		5 * time.Second,
		9 * time.Second,
	} {
		if allowed, reason := client.allowChat(now.Add(offset)); allowed ||
			!strings.Contains(reason, "3 messages") {
			t.Fatalf("rejected retry at %s was unexpectedly allowed: %q",
				offset, reason)
		}
	}
	if len(client.acceptedChatTimes) != 3 {
		t.Fatalf("rejected attempts entered rate-limit accounting: %v",
			client.acceptedChatTimes)
	}
	if allowed, reason := client.allowChat(now.Add(11 * time.Second)); !allowed {
		t.Fatalf("short window should recover: %s", reason)
	}

	client.acceptedChatTimes = client.acceptedChatTimes[:0]
	for index := 0; index < 15; index++ {
		client.acceptedChatTimes = append(
			client.acceptedChatTimes,
			now.Add(-59*time.Second+time.Duration(index)*4*time.Second),
		)
	}
	if allowed, reason := client.allowChat(now); allowed ||
		!strings.Contains(reason, "15 messages") {
		t.Fatalf("expected minute limit, got allowed=%v reason=%q", allowed, reason)
	}
}

func TestContentFiltersAreAppliedToChat(t *testing.T) {
	rig := newTestRig(t, 10)
	conn, _ := rig.connect(t)
	sendPresence(t, conn, "dobuita", 1, nil, 0)
	var snapshot protocol.Snapshot
	readJSON(t, conn, &snapshot)

	if err := conn.WriteJSON(protocol.ChatRequest{
		Header: protocol.NewHeader(protocol.TypeChat),
		Text:   "hello f4g world",
	}); err != nil {
		t.Fatal(err)
	}
	var chat protocol.ChatEvent
	readJSON(t, conn, &chat)
	if chat.Message.Text != "hello *** world" {
		t.Fatalf("chat was not censored before broadcast: %#v", chat)
	}
}

func TestValidationAndSlowClientIsolation(t *testing.T) {
	for _, worldID := range []string{
		"exterior",
		"interior",
		"yamanose",
		"sakuragaoka",
		"dobuita",
		"mfsy",
		"mksg",
		"mfbt",
		"ms08",
		"ma00",
		"ma00race",
		"arcade",
		"dkty",
		"dski",
		"dtky",
		"dcha",
		"tatq",
		"dbyo",
		"djaz",
		"dpiz",
		"drme",
		"dsli",
		"durn",
		"dykz",
		"dcbn",
		"daza",
		"dbhb",
		"dgct",
		"dkpa",
		"drht",
		"drsa",
		"dsba",
		"dslt",
		"dsus",
		"s2ak00",
		"s2ar02",
		"s2ar03",
		"s2wb00",
		"s2we00",
		"s2wk00",
		"s2wn00",
		"s2wr00",
		"s2ws00",
		"s2wt00",
		"s2aka3",
		"s2aks0",
		"s2aks1",
		"s2akt0",
		"s2akt1",
		"s2akt2",
		"s2akt3",
		"s2aky0",
		"s2ar01",
		"s2ara0",
		"s2arc0",
		"s2arm0",
		"s2arsf",
		"s2arz0",
		"s2wb01",
		"s2wecf",
		"s2weg0",
		"s2wem1",
		"s2wes1",
		"s2wesm",
		"s2wet0",
		"s2wka0",
		"s2wrs2",
		"s2wsg1",
		"s2wsy0",
		"s2wta0",
		"op00",
	} {
		if _, ok := validatePresence(protocol.Presence{
			WorldID: worldID, CharacterID: "ryo", Movement: "idle", Sequence: 1,
		}); !ok {
			t.Errorf("browser world %q should accept multiplayer presence", worldID)
		}
	}
	if _, ok := validatePresence(protocol.Presence{
		WorldID: "unknown", CharacterID: "ryo", Movement: "idle", Sequence: 1,
	}); ok {
		t.Fatal("unknown world should fail")
	}
	forkliftID := "forklift-1"
	if _, _, ok := normalizeVehiclePresence(protocol.Presence{
		VehicleID: &forkliftID,
		VehicleQW: 10,
	}); ok {
		t.Fatal("unnormalizable forklift orientation should fail")
	}
	if text, ok := normalizeChat(" hi \n there "); !ok || text != "hi there" {
		t.Fatalf("chat normalization failed: %q %v", text, ok)
	}
	if _, ok := normalizeChat(strings.Repeat("a", 241)); ok {
		t.Fatal("oversized chat should fail")
	}
	rig := newTestRig(t, 10)
	client := newClient(
		rig.hub, nil, "slow", "Guest5555",
		ConnectionMetadata{},
	)
	rig.hub.mu.Lock()
	rig.hub.clients[client.id] = client
	rig.hub.names[strings.ToLower(client.name)] = client.id
	rig.hub.mu.Unlock()
	for i := 0; i < cap(client.send); i++ {
		client.send <- json.RawMessage(`{"v":1}`)
	}
	start := time.Now()
	if rig.hub.sendOne(client, map[string]int{"v": 1}) {
		t.Fatal("full client queue should reject send")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("slow recipient blocked sender")
	}
	deadline := time.Now().Add(time.Second)
	for rig.hub.ActiveClients() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rig.hub.ActiveClients() != 0 {
		t.Fatal("slow client was not disconnected")
	}
}

func TestRaceAndPlaygroundForkliftsExpireAfterNormalExit(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	want := now.Add(abandonedForkliftLifetime).UnixMilli()
	for _, worldID := range []string{"ma00race", "ma00"} {
		got := forkliftReleaseExpiry(worldID, now)
		if got != want {
			t.Fatalf("%s forklift expiry = %d, want %d", worldID, got, want)
		}
	}
	if got := forkliftReleaseExpiry("mfsy", now); got != 0 {
		t.Fatalf("harbor forklift unexpectedly expires at %d", got)
	}
}

func TestHarborStartsWithoutForkliftsOrCargo(t *testing.T) {
	for id, state := range initialForkliftStates() {
		if state.WorldID == "mfsy" {
			t.Fatalf("harbor retained initial forklift %s: %#v", id, state)
		}
	}
	for id, state := range initialCargoStates() {
		if state.WorldID == "mfsy" {
			t.Fatalf("harbor retained initial cargo %s: %#v", id, state)
		}
	}
}

func TestStaleCargoCleanupOnlyWhenWorldExceedsThreshold(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old := now.Add(-cargoCleanupAge)
	makeCargo := func(count int) (*Hub, string) {
		rig := newTestRig(t, 10)
		rig.hub.cargo = make(map[string]protocol.CargoState)
		rig.hub.cargoLastTouched = make(map[string]time.Time)
		targetID := ""
		for index := 0; index < count; index++ {
			id := fmt.Sprintf("cargo-test-%d", index)
			if index == 0 {
				targetID = id
			}
			rig.hub.cargo[id] = protocol.CargoState{
				ID:      id,
				WorldID: "ma00",
				QW:      1,
			}
			rig.hub.cargoLastTouched[id] = old
		}
		return rig.hub, targetID
	}

	atLimit, atLimitID := makeCargo(cargoCleanupThreshold)
	if _, _, removed := atLimit.cleanupStaleCargoLocked(
		atLimitID,
		old,
		now,
	); removed {
		t.Fatal("cargo was removed while world was at cleanup threshold")
	}

	overLimit, overLimitID := makeCargo(cargoCleanupThreshold + 1)
	if _, _, removed := overLimit.cleanupStaleCargoLocked(
		overLimitID,
		old,
		now,
	); !removed {
		t.Fatal("stale cargo was not removed while world exceeded threshold")
	}
	if _, exists := overLimit.cargo[overLimitID]; exists {
		t.Fatal("removed cargo remains in server state")
	}
}

func TestUnsupportedProtocolVersionClosesConnection(t *testing.T) {
	rig := newTestRig(t, 10)
	conn, _ := rig.connect(t)
	if err := conn.WriteJSON(protocol.Header{
		Version: protocol.Version + 1,
		Type:    protocol.TypeLeaveWorld,
	}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := conn.ReadMessage()
	var closeError *websocket.CloseError
	if err == nil || !errors.As(err, &closeError) ||
		closeError.Code != websocket.CloseProtocolError {
		t.Fatalf("expected protocol close, got %v", err)
	}
}

func TestCapacityClosesExtraConnection(t *testing.T) {
	rig := newTestRig(t, 1)
	_, _ = rig.connect(t)
	headers := http.Header{"Origin": []string{rig.server.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(rig.wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected capacity connection to close")
	}
}
