package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/activitylog"
	"github.com/brynnb/new-yokosuka-server/internal/dialoguestate"
	"github.com/brynnb/new-yokosuka-server/internal/game"
	"github.com/brynnb/new-yokosuka-server/internal/npc"
	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
	"github.com/brynnb/new-yokosuka-server/internal/travelaccess"
	"github.com/brynnb/new-yokosuka-server/internal/vending"
	"github.com/brynnb/new-yokosuka-server/internal/worldstate"
	"github.com/gorilla/websocket"
)

var (
	ErrCapacity = errors.New("server is at capacity")
)

var (
	tokenPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,31}$`)
)

var validWorlds = map[string]struct{}{
	"exterior":    {},
	"interior":    {},
	"yamanose":    {},
	"sakuragaoka": {},
	"dobuita":     {},
	"cinema":      {},
	"mfsy":        {},
	"mksg":        {},
	"mfbt":        {},
	"ms08":        {},
	"ma00":        {},
	"ma00race":    {},
	"arcade":      {},
	"op00":        {},
	"dkty":        {},
	"dski":        {},
	"dtky":        {},
	"dcha":        {},
	"tatq":        {},
	"dbyo":        {},
	"djaz":        {},
	"dpiz":        {},
	"drme":        {},
	"dsli":        {},
	"durn":        {},
	"dykz":        {},
	"dcbn":        {},
	"daza":        {},
	"dbhb":        {},
	"dgct":        {},
	"dkpa":        {},
	"drht":        {},
	"drsa":        {},
	"dsba":        {},
	"dslt":        {},
	"dsus":        {},
	"s2ak00":      {},
	"s2ar02":      {},
	"s2ar03":      {},
	"s2wb00":      {},
	"s2we00":      {},
	"s2wk00":      {},
	"s2wn00":      {},
	"s2wr00":      {},
	"s2ws00":      {},
	"s2wt00":      {},
	"s2aka3":      {},
	"s2aks0":      {},
	"s2aks1":      {},
	"s2akt0":      {},
	"s2akt1":      {},
	"s2akt2":      {},
	"s2akt3":      {},
	"s2aky0":      {},
	"s2ar01":      {},
	"s2ara0":      {},
	"s2arc0":      {},
	"s2arm0":      {},
	"s2arsf":      {},
	"s2arz0":      {},
	"s2wb01":      {},
	"s2wecf":      {},
	"s2weg0":      {},
	"s2wem1":      {},
	"s2wes1":      {},
	"s2wesm":      {},
	"s2wet0":      {},
	"s2wka0":      {},
	"s2wrs2":      {},
	"s2wsg1":      {},
	"s2wsy0":      {},
	"s2wta0":      {},
}

var validMovements = map[string]struct{}{
	"idle":      {},
	"walk":      {},
	"backpedal": {},
	"run":       {},
}

var dynamicForkliftPattern = regexp.MustCompile(`^forklift-[a-f0-9]{12}$`)
var cargoIDPattern = regexp.MustCompile(`^cargo-[a-z0-9-]{1,40}$`)

func validForkliftID(id string) bool {
	if _, ok := validForklifts[id]; ok {
		return true
	}
	return dynamicForkliftPattern.MatchString(id)
}

const abandonedForkliftLifetime = 5 * time.Minute
const forkliftMaximumLift = 3.2
const cargoClaimReleaseDelay = 500 * time.Millisecond
const cargoReplenishDelay = 3 * time.Second
const cargoAutoRightDelay = 2 * time.Second
const cargoCleanupAge = 10 * time.Minute
const cargoCleanupThreshold = 50

func forkliftReleaseExpiry(worldID string, now time.Time) int64 {
	if worldID != "ma00race" && worldID != "ma00" {
		return 0
	}
	return now.Add(abandonedForkliftLifetime).UnixMilli()
}

const cargoUprightDotThreshold = 0.65
const cargoSpawnRadius = 3.0

type cargoSpawnPosition struct {
	x float64
	y float64
	z float64
}

func initialCargoSpawnPositions() map[string]cargoSpawnPosition {
	positions := make(map[string]cargoSpawnPosition)
	for _, world := range vehicleSpawnManifest.Worlds {
		for _, spawn := range world.Cargo {
			positions[spawn.ID] = cargoSpawnPosition{
				x: spawn.Position.X,
				y: spawn.Position.Y,
				z: spawn.Position.Z,
			}
		}
	}
	return positions
}

func cargoSpawnForWorld(worldID string) (float64, float64, float64) {
	if world, exists := vehicleSpawnManifest.Worlds[worldID]; exists &&
		len(world.Cargo) > 0 {
		position := world.Cargo[0].Position
		return position.X, position.Y, position.Z
	}
	return configuredCargoPosition(
		vehicleSpawnManifest,
		"ma00",
		"cargo-job-1",
	)
}

func forkliftOrientationForYaw(yaw float64) (float64, float64, float64, float64) {
	return 0, math.Sin(yaw / 2), 0, math.Cos(yaw / 2)
}

func initialForkliftState(
	id string,
	worldID string,
	x float64,
	z float64,
	yaw float64,
	now int64,
) protocol.ForkliftState {
	qx, qy, qz, qw := forkliftOrientationForYaw(yaw)
	return protocol.ForkliftState{
		ID: id, WorldID: worldID, X: x, Z: z, Yaw: yaw,
		QX: qx, QY: qy, QZ: qz, QW: qw, UpdatedAtMs: now,
	}
}

func initialForkliftStates() map[string]protocol.ForkliftState {
	now := time.Now().UnixMilli()
	states := make(map[string]protocol.ForkliftState)
	for worldID, world := range vehicleSpawnManifest.Worlds {
		for _, spawn := range world.Forklifts {
			states[spawn.ID] = initialForkliftState(
				spawn.ID,
				worldID,
				spawn.Position.X,
				spawn.Position.Z,
				spawn.Yaw,
				now,
			)
			state := states[spawn.ID]
			state.Y = spawn.Position.Y
			states[spawn.ID] = state
		}
	}
	return states
}

func initialCargoStates() map[string]protocol.CargoState {
	states := make(map[string]protocol.CargoState)
	now := time.Now().UnixMilli()
	for worldID, world := range vehicleSpawnManifest.Worlds {
		for _, spawn := range world.Cargo {
			states[spawn.ID] = protocol.CargoState{
				ID: spawn.ID, WorldID: worldID,
				X:  spawn.Position.X,
				Y:  spawn.Position.Y,
				Z:  spawn.Position.Z,
				QW: 1, Sleeping: true, UpdatedAtMs: now,
			}
		}
	}
	return states
}

type storedPresence struct {
	state            protocol.PlayerState
	animationStarted time.Time
}

func (p storedPresence) stateAt(now time.Time) protocol.PlayerState {
	state := p.state
	if state.AnimationID != nil && !p.animationStarted.IsZero() {
		state.AnimationElapsedMs = max(0, now.Sub(p.animationStarted).Milliseconds())
	}
	return state
}

type Hub struct {
	mu                 sync.RWMutex
	clients            map[string]*Client
	names              map[string]string
	rooms              map[string]map[string]*Client
	presences          map[string]storedPresence
	departedWorlds     map[string]string
	transitionGrants   map[string]transitionGrant
	authorizedArrivals map[string]authorizedArrival
	forkliftOwners     map[string]string
	forklifts          map[string]protocol.ForkliftState
	cargo              map[string]protocol.CargoState
	cargoSpawns        map[string]cargoSpawnPosition
	cargoAwaySince     map[string]time.Time
	cargoUntouched     map[string]time.Time
	cargoLastTouched   map[string]time.Time
	cargoReplaced      map[string]bool
	nextCargoID        uint64
	maxClients         int
	world              *worldstate.Manager
	logger             *log.Logger
	activity           *activitylog.Recorder
	guestCounter       uint64
	activeCharacters   map[int64]string
	pendingLocations   map[string]*Client
	locationSaver      LocationSaver
	chatMessageSaver   ChatMessageSaver
	chatHistoryLoader  ChatHistoryLoader
	publicChatSink     func(string, string)
	npcs               *npc.Engine
	vending            *vending.Catalog
	vendingStore       VendingStore
	vendingDraw        func(int) (int, error)
	travelAccess       *travelaccess.Catalog
	scriptEvents       ScriptEventEngine
	nativeAreaForWorld map[string]string
}

type ConnectionMetadata struct {
	RemoteIP      string
	UserAgent     string
	AccountID     int64
	AccountType   string
	Character     *store.Character
	Inventory     []store.InventoryItem
	DialogueState *dialoguestate.Snapshot
}

func NewHub(
	maxClients int,
	world *worldstate.Manager,
	logger *log.Logger,
	activity *activitylog.Recorder,
) *Hub {
	if maxClients <= 0 {
		maxClients = 100
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Hub{
		clients:            make(map[string]*Client),
		names:              make(map[string]string),
		rooms:              make(map[string]map[string]*Client),
		presences:          make(map[string]storedPresence),
		departedWorlds:     make(map[string]string),
		transitionGrants:   make(map[string]transitionGrant),
		authorizedArrivals: make(map[string]authorizedArrival),
		forkliftOwners:     make(map[string]string),
		forklifts:          initialForkliftStates(),
		cargo:              initialCargoStates(),
		cargoSpawns:        initialCargoSpawnPositions(),
		cargoAwaySince:     make(map[string]time.Time),
		cargoUntouched:     make(map[string]time.Time),
		cargoLastTouched:   make(map[string]time.Time),
		cargoReplaced:      make(map[string]bool),
		nextCargoID:        4,
		activeCharacters:   make(map[int64]string),
		pendingLocations:   make(map[string]*Client),
		maxClients:         maxClients,
		world:              world,
		vending:            vending.MustLoad(),
		travelAccess:       travelaccess.MustLoad(),
		logger:             logger,
		activity:           activity,
		nativeAreaForWorld: make(map[string]string),
	}
}

func (h *Hub) logf(format string, args ...any) {
	h.logger.Printf(format, args...)
}

func (h *Hub) recordActivity(event activitylog.Event) {
	if h.activity == nil {
		return
	}
	if err := h.activity.Record(event); err != nil {
		h.logf("activity log write failed: %v", err)
	}
}

func (h *Hub) Register(
	conn *websocket.Conn,
	connection ConnectionMetadata,
) (*Client, error) {
	h.mu.Lock()
	var replacedClient *Client
	if connection.Character != nil {
		activeID := h.activeCharacters[connection.Character.ID]
		replacedClient = h.clients[activeID]
		if activeID != "" && replacedClient == nil {
			delete(h.activeCharacters, connection.Character.ID)
		}
	}
	if len(h.clients) >= h.maxClients && replacedClient == nil {
		h.mu.Unlock()
		return nil, ErrCapacity
	}
	id, err := randomHex(16)
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	name := h.allocateGuestNameLocked()
	if connection.Character != nil {
		name = connection.Character.Name
	}
	client := newClient(h, conn, id, name, connection)
	h.clients[id] = client
	if client.persistent {
		h.activeCharacters[client.characterID] = id
	}
	h.names[strings.ToLower(name)] = id
	connectedClients := len(h.clients)
	recipients := h.allClientsLocked(id)
	chatHistoryLoader := h.chatHistoryLoader
	h.mu.Unlock()
	if replacedClient != nil {
		h.notifySessionReplaced(replacedClient)
	}
	h.recordActivity(activitylog.Event{
		Type:             "connect",
		PlayerID:         id,
		Name:             name,
		RemoteIP:         connection.RemoteIP,
		UserAgent:        connection.UserAgent,
		ConnectedClients: connectedClients,
	})

	welcome := protocol.Welcome{
		Header: protocol.NewHeader(protocol.TypeWelcome),
		Self: protocol.Self{
			ID:          id,
			Name:        name,
			AccountID:   client.accountID,
			CharacterID: client.characterID,
			AccountType: client.accountType,
			AvatarID:    client.avatarID,
		},
		WorldState:       h.world.Snapshot(),
		ConnectedClients: connectedClients,
	}
	if chatHistoryLoader != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		history, historyErr := chatHistoryLoader.RecentChatMessages(
			ctx,
			store.MaxRecentChatMessages,
		)
		cancel()
		if historyErr != nil {
			h.logf("recent chat history load failed for %s: %v", id, historyErr)
		} else {
			welcome.ChatHistory = make([]protocol.ChatMessage, 0, len(history))
			for _, message := range history {
				welcome.ChatHistory = append(welcome.ChatHistory, protocol.ChatMessage{
					PlayerID: message.PlayerID,
					Name:     message.PlayerName,
					WorldID:  message.WorldID,
					Text:     message.Text,
					SentAt:   message.SentAt.UnixMilli(),
				})
			}
		}
	}
	if connection.Character != nil {
		level := game.LevelForExperience(connection.Character.Experience)
		welcome.Character = &protocol.CharacterProfile{
			ID:                connection.Character.ID,
			Name:              connection.Character.Name,
			AvatarID:          connection.Character.AvatarKey,
			WorldID:           connection.Character.WorldID,
			X:                 connection.Character.X,
			Y:                 connection.Character.Y,
			Z:                 connection.Character.Z,
			Yaw:               connection.Character.Yaw,
			Experience:        connection.Character.Experience,
			Level:             level,
			CurrentHP:         connection.Character.CurrentHP,
			MaxHP:             connection.Character.MaxHP,
			Yen:               connection.Character.Yen,
			TimePlayedSeconds: connection.Character.TimePlayedSeconds,
		}
		welcome.Inventory = make([]protocol.InventoryItem, 0, len(connection.Inventory))
		for _, item := range connection.Inventory {
			welcome.Inventory = append(welcome.Inventory, protocol.InventoryItem{
				Key: item.Key, Name: item.Name, Description: item.Description,
				Category: item.Category, MaxStack: item.MaxStack, Usable: item.Usable,
				EffectKind: item.EffectKind, EffectValue: item.EffectValue,
				Quantity: item.Quantity,
			})
		}
		welcome.DialogueState = connection.DialogueState
	}
	if !h.sendOne(client, welcome) {
		h.Unregister(client)
		return nil, errors.New("failed to enqueue welcome")
	}
	h.sendMany(recipients, protocol.ClientCount{
		Header:           protocol.NewHeader(protocol.TypeClientCount),
		ConnectedClients: connectedClients,
	})
	return client, nil
}

func (h *Hub) notifySessionReplaced(client *Client) {
	payload, err := json.Marshal(protocol.SessionReplaced{
		Header:  protocol.NewHeader(protocol.TypeSessionReplaced),
		Message: "This character was opened in another browser or tab.",
	})
	if err != nil || !client.notifyReplacement(payload) {
		go h.Unregister(client)
	}
}

func (h *Hub) SendPlayerDirectory(client *Client) bool {
	h.mu.RLock()
	players := make([]protocol.OnlinePlayer, 0, len(h.clients))
	for id, onlineClient := range h.clients {
		worldID := ""
		if presence, exists := h.presences[id]; exists {
			worldID = presence.state.WorldID
		}
		players = append(players, protocol.OnlinePlayer{
			ID:      id,
			Name:    onlineClient.name,
			WorldID: worldID,
		})
	}
	h.mu.RUnlock()
	sort.Slice(players, func(i, j int) bool {
		left := strings.ToLower(players[i].Name)
		right := strings.ToLower(players[j].Name)
		if left == right {
			return players[i].ID < players[j].ID
		}
		return left < right
	})
	return h.sendOne(client, protocol.PlayerDirectory{
		Header:  protocol.NewHeader(protocol.TypePlayerDirectory),
		Players: players,
	})
}

func (h *Hub) allocateGuestNameLocked() string {
	for attempt := 0; attempt < 128; attempt++ {
		var raw [2]byte
		if _, err := rand.Read(raw[:]); err == nil {
			number := 1000 + (int(raw[0])<<8|int(raw[1]))%9000
			name := fmt.Sprintf("Guest%04d", number)
			if _, exists := h.names[strings.ToLower(name)]; !exists {
				return name
			}
		}
	}
	for {
		h.guestCounter++
		name := fmt.Sprintf("Guest%d", 10000+h.guestCounter)
		if _, exists := h.names[strings.ToLower(name)]; !exists {
			return name
		}
	}
}

func randomHex(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (h *Hub) Unregister(client *Client) {
	if client == nil {
		return
	}
	h.mu.Lock()
	current, exists := h.clients[client.id]
	if !exists || current != client {
		h.mu.Unlock()
		client.close()
		return
	}
	delete(h.clients, client.id)
	delete(h.departedWorlds, client.id)
	delete(h.transitionGrants, client.id)
	delete(h.authorizedArrivals, client.id)
	nameKey := strings.ToLower(client.name)
	if h.names[nameKey] == client.id {
		delete(h.names, nameKey)
	}
	if client.persistent &&
		h.activeCharacters[client.characterID] == client.id {
		delete(h.activeCharacters, client.characterID)
	}
	if client.persistent {
		if h.pendingLocations == nil {
			h.pendingLocations = make(map[string]*Client)
		}
		h.pendingLocations[client.id] = client
	}
	sessionReplaced := client.persistent &&
		h.activeCharacters[client.characterID] != "" &&
		h.activeCharacters[client.characterID] != client.id
	oldPresence, hadPresence := h.presences[client.id]
	var recipients []*Client
	var abandonedForklift *protocol.ForkliftState
	var releasedCargo []protocol.CargoState
	if hadPresence {
		delete(h.presences, client.id)
		releasedCargo = h.markClientCargoForReleaseLocked(
			client.id,
			time.Now(),
		)
		if oldPresence.state.VehicleID != nil {
			forklift := h.forklifts[*oldPresence.state.VehicleID]
			forklift.OwnerID = ""
			// There is no server-side vehicle simulation after the owner
			// disappears. Retaining the final velocities would reapply stale
			// momentum when another client enters the abandoned forklift,
			// sometimes launching it violently after a page refresh.
			forklift.VelocityX = 0
			forklift.VelocityY = 0
			forklift.VelocityZ = 0
			forklift.AngularVelocityX = 0
			forklift.AngularVelocityY = 0
			forklift.AngularVelocityZ = 0
			forklift.ExpiresAtMs = time.Now().Add(
				abandonedForkliftLifetime,
			).UnixMilli()
			forklift.UpdatedAtMs = time.Now().UnixMilli()
			h.forklifts[forklift.ID] = forklift
			abandonedForklift = &forklift
		}
		h.releaseForkliftLocked(client.id, oldPresence.state.VehicleID)
		recipients = h.removeFromRoomLocked(client, oldPresence.state.WorldID)
	}
	connectedClients := len(h.clients)
	connectedRecipients := h.allClientsLocked("")
	disconnectedName := client.name
	h.mu.Unlock()
	disconnectedAt := time.Now()
	h.cancelClientScript(client)
	h.flushClientLocation(client)
	if client.persistent {
		h.mu.Lock()
		if h.pendingLocations[client.id] == client {
			delete(h.pendingLocations, client.id)
		}
		h.mu.Unlock()
	}
	worldID := ""
	if hadPresence {
		worldID = oldPresence.state.WorldID
	}
	h.recordActivity(activitylog.Event{
		Type:             "disconnect",
		PlayerID:         client.id,
		Name:             disconnectedName,
		WorldID:          worldID,
		RemoteIP:         client.remoteIP,
		UserAgent:        client.userAgent,
		SessionSeconds:   time.Since(client.connectedAt).Seconds(),
		ConnectedClients: connectedClients,
	})
	client.close()
	if hadPresence {
		h.sendMany(recipients, protocol.PlayerLeft{
			Header:    protocol.NewHeader(protocol.TypePlayerLeft),
			PlayerID:  client.id,
			UpdatedAt: disconnectedAt.UnixMilli(),
		})
	}
	if abandonedForklift != nil {
		h.sendMany(recipients, protocol.ForkliftStateEvent{
			Header:   protocol.NewHeader(protocol.TypeForkliftState),
			Forklift: *abandonedForklift,
		})
		h.scheduleForkliftExpiry(
			abandonedForklift.ID,
			abandonedForklift.ExpiresAtMs,
		)
		h.sendMany(recipients, protocol.ForkliftSound{
			Header:     protocol.NewHeader(protocol.TypeForkliftSound),
			ForkliftID: abandonedForklift.ID,
			Cue:        "shutdown",
		})
	}
	for _, cargo := range releasedCargo {
		h.sendMany(recipients, protocol.CargoStateEvent{
			Header: protocol.NewHeader(protocol.TypeCargoState),
			Cargo:  cargo,
		})
		h.scheduleCargoClaimRelease(
			cargo.ID,
			cargo.OwnerID,
			cargo.ClaimExpiresAtMs,
		)
		h.scheduleCargoAutoRight(
			cargo.ID,
			time.UnixMilli(cargo.UpdatedAtMs),
		)
		h.scheduleCargoCleanup(
			cargo.ID,
			time.UnixMilli(cargo.UpdatedAtMs),
		)
	}
	h.sendMany(connectedRecipients, protocol.ClientCount{
		Header:           protocol.NewHeader(protocol.TypeClientCount),
		ConnectedClients: connectedClients,
	})
	if hadPresence && !sessionReplaced {
		h.sendMany(connectedRecipients, protocol.SystemMessage{
			Header: protocol.NewHeader(protocol.TypeSystemMessage),
			Text: fmt.Sprintf(
				"%s has left the game.",
				disconnectedName,
			),
			SentAt: disconnectedAt.UnixMilli(),
		})
	}
}

func (h *Hub) HandlePresence(client *Client, message protocol.Presence) bool {
	animationID, ok := validatePresence(message)
	if !ok {
		return false
	}
	now := time.Now()

	h.mu.Lock()
	if h.clients[client.id] != client {
		h.mu.Unlock()
		return false
	}
	previous, hadPrevious := h.presences[client.id]
	if hadPrevious && message.Sequence <= previous.state.Sequence {
		h.mu.Unlock()
		return false
	}
	if hadPrevious {
		if message.AnimationRevision < previous.state.AnimationRevision {
			h.mu.Unlock()
			return false
		}
		if message.AnimationRevision == previous.state.AnimationRevision &&
			animationID != nil && !equalStringPointers(animationID, previous.state.AnimationID) {
			h.mu.Unlock()
			return false
		}
	}
	vehicleID, vehicleOrientation, vehicleOK := normalizeVehiclePresence(message)
	if !vehicleOK {
		h.mu.Unlock()
		return false
	}
	if vehicleID != nil {
		forklift, exists := h.forklifts[*vehicleID]
		if !exists || forklift.WorldID != message.WorldID {
			vehicleID = nil
		}
	}
	if vehicleID != nil {
		if owner, occupied := h.forkliftOwners[*vehicleID]; occupied && owner != client.id {
			// Keep the player connected and visible, but reject the contested
			// vehicle claim. The room snapshot contains its real owner, which
			// lets the requesting client choose another available forklift.
			vehicleID = nil
		}
	}
	if vehicleID == nil {
		vehicleOrientation = [4]float64{0, 0, 0, 1}
	}
	if !h.allowPresenceDestinationLocked(
		client,
		message,
		previous,
		hadPrevious,
	) {
		h.mu.Unlock()
		return false
	}
	var releasedForklift *protocol.ForkliftState
	enteredForkliftID := ""
	if hadPrevious && !equalStringPointers(previous.state.VehicleID, vehicleID) {
		if previous.state.VehicleID != nil {
			released := h.forklifts[*previous.state.VehicleID]
			released.OwnerID = ""
			released.ExpiresAtMs = forkliftReleaseExpiry(
				released.WorldID,
				now,
			)
			released.UpdatedAtMs = now.UnixMilli()
			h.forklifts[released.ID] = released
			releasedForklift = &released
		}
		h.releaseForkliftLocked(client.id, previous.state.VehicleID)
	}
	if vehicleID != nil {
		if !hadPrevious ||
			previous.state.VehicleID == nil ||
			*previous.state.VehicleID != *vehicleID {
			enteredForkliftID = *vehicleID
		}
		h.forkliftOwners[*vehicleID] = client.id
		forklift := h.forklifts[*vehicleID]
		forklift.ID = *vehicleID
		forklift.X = message.X
		forklift.Y = message.Y
		forklift.Z = message.Z
		forklift.Yaw = message.Yaw
		forklift.QX = vehicleOrientation[0]
		forklift.QY = vehicleOrientation[1]
		forklift.QZ = vehicleOrientation[2]
		forklift.QW = vehicleOrientation[3]
		forklift.Lift = message.VehicleLift
		forklift.Steering = message.VehicleSteering
		forklift.WheelRoll = message.VehicleWheelRoll
		forklift.OwnerID = client.id
		forklift.ExpiresAtMs = 0
		forklift.UpdatedAtMs = now.UnixMilli()
		h.forklifts[*vehicleID] = forklift
	}
	firstConnectionPresence := !client.announcedInitialEntry
	client.announcedInitialEntry = true

	animationStarted := previous.animationStarted
	if animationID == nil {
		animationStarted = time.Time{}
	} else if !hadPrevious ||
		message.AnimationRevision != previous.state.AnimationRevision ||
		!equalStringPointers(animationID, previous.state.AnimationID) {
		animationStarted = now
	}

	avatarID := message.AvatarID
	if avatarID == "" {
		avatarID = message.CharacterID
	}
	characterID := avatarID
	if client.persistent {
		client.avatarID = avatarID
		characterID = strconv.FormatInt(client.characterID, 10)
	}
	state := protocol.PlayerState{
		ID:                client.id,
		Name:              client.name,
		WorldID:           message.WorldID,
		CharacterID:       characterID,
		AvatarID:          avatarID,
		X:                 message.X,
		Y:                 message.Y,
		Z:                 message.Z,
		Yaw:               message.Yaw,
		Movement:          message.Movement,
		AnimationID:       animationID,
		AnimationRevision: message.AnimationRevision,
		Sequence:          message.Sequence,
		UpdatedAt:         now.UnixMilli(),
		VehicleID:         vehicleID,
		VehicleLift:       message.VehicleLift,
		VehicleSteering:   message.VehicleSteering,
		VehicleWheelRoll:  message.VehicleWheelRoll,
		VehicleQX:         vehicleOrientation[0],
		VehicleQY:         vehicleOrientation[1],
		VehicleQZ:         vehicleOrientation[2],
		VehicleQW:         vehicleOrientation[3],
	}
	next := storedPresence{state: state, animationStarted: animationStarted}
	roomChanged := hadPrevious && previous.state.WorldID != message.WorldID
	var oldRecipients []*Client
	var releasedCargo []protocol.CargoState
	if roomChanged {
		releasedCargo = h.markClientCargoForReleaseLocked(client.id, now)
		oldRecipients = h.removeFromRoomLocked(client, previous.state.WorldID)
	}
	if !hadPrevious || roomChanged {
		if h.rooms[message.WorldID] == nil {
			h.rooms[message.WorldID] = make(map[string]*Client)
		}
		h.rooms[message.WorldID][client.id] = client
	}
	h.presences[client.id] = next

	var snapshotPlayers []protocol.PlayerState
	var snapshotForklifts []protocol.ForkliftState
	var snapshotCargo []protocol.CargoState
	var snapshotNPCs []protocol.NPCState
	if !hadPrevious || roomChanged {
		for id := range h.rooms[message.WorldID] {
			if id == client.id {
				continue
			}
			if presence, exists := h.presences[id]; exists {
				snapshotPlayers = append(snapshotPlayers, presence.stateAt(now))
			}
		}
		for _, forklift := range h.forklifts {
			if forklift.WorldID == message.WorldID {
				snapshotForklifts = append(snapshotForklifts, forklift)
			}
		}
		for _, cargo := range h.cargo {
			if cargo.WorldID == message.WorldID {
				snapshotCargo = append(snapshotCargo, cargo)
			}
		}
		if h.npcs != nil {
			for _, state := range h.npcs.Snapshot(message.WorldID) {
				snapshotNPCs = append(snapshotNPCs, npcProtocolState(state))
			}
		}
	}
	recipients := h.roomRecipientsLocked(message.WorldID, client.id)
	var entryRecipients []*Client
	if firstConnectionPresence {
		entryRecipients = h.allClientsLocked(client.id)
	}
	outboundState := next.stateAt(now)
	h.mu.Unlock()
	if vehicleID == nil {
		client.markLocation(store.Location{
			WorldID: message.WorldID,
			X:       message.X,
			Y:       message.Y,
			Z:       message.Z,
			Yaw:     message.Yaw,
		})
	}
	if roomChanged {
		go h.flushClientLocation(client)
	}

	if releasedForklift != nil {
		forkliftRecipients := recipients
		if roomChanged {
			forkliftRecipients = oldRecipients
		}
		h.sendMany(forkliftRecipients, protocol.ForkliftStateEvent{
			Header:   protocol.NewHeader(protocol.TypeForkliftState),
			Forklift: *releasedForklift,
		})
		h.sendMany(forkliftRecipients, protocol.ForkliftSound{
			Header:     protocol.NewHeader(protocol.TypeForkliftSound),
			ForkliftID: releasedForklift.ID,
			Cue:        "shutdown",
		})
		if releasedForklift.ExpiresAtMs > 0 {
			h.scheduleForkliftExpiry(
				releasedForklift.ID,
				releasedForklift.ExpiresAtMs,
			)
		}
	}
	if roomChanged {
		h.sendMany(oldRecipients, protocol.PlayerLeft{
			Header:    protocol.NewHeader(protocol.TypePlayerLeft),
			PlayerID:  client.id,
			UpdatedAt: now.UnixMilli(),
		})
		for _, cargo := range releasedCargo {
			h.sendMany(oldRecipients, protocol.CargoStateEvent{
				Header: protocol.NewHeader(protocol.TypeCargoState),
				Cargo:  cargo,
			})
			h.scheduleCargoClaimRelease(
				cargo.ID,
				cargo.OwnerID,
				cargo.ClaimExpiresAtMs,
			)
			h.scheduleCargoAutoRight(
				cargo.ID,
				time.UnixMilli(cargo.UpdatedAtMs),
			)
			h.scheduleCargoCleanup(
				cargo.ID,
				time.UnixMilli(cargo.UpdatedAtMs),
			)
		}
	}
	if !hadPrevious || roomChanged {
		if snapshotPlayers == nil {
			snapshotPlayers = []protocol.PlayerState{}
		}
		h.sendOne(client, protocol.Snapshot{
			Header:    protocol.NewHeader(protocol.TypeSnapshot),
			Players:   snapshotPlayers,
			Forklifts: snapshotForklifts,
			Cargo:     snapshotCargo,
			NPCs:      snapshotNPCs,
		})
	}
	h.sendMany(recipients, protocol.PlayerStateEvent{
		Header: protocol.NewHeader(protocol.TypePlayerState),
		Player: outboundState,
	})
	if enteredForkliftID != "" {
		h.sendMany(recipients, protocol.ForkliftSound{
			Header:     protocol.NewHeader(protocol.TypeForkliftSound),
			ForkliftID: enteredForkliftID,
			Cue:        "startup",
		})
	}
	if firstConnectionPresence {
		h.sendMany(entryRecipients, protocol.PlayerEntered{
			Header:   protocol.NewHeader(protocol.TypePlayerEnter),
			PlayerID: client.id,
			Name:     client.name,
			WorldID:  message.WorldID,
		})
	}
	return true
}

func validatePresence(message protocol.Presence) (*string, bool) {
	if _, ok := validWorlds[message.WorldID]; !ok {
		return nil, false
	}
	if _, ok := validMovements[message.Movement]; !ok {
		return nil, false
	}
	avatarID := message.AvatarID
	if avatarID == "" {
		avatarID = message.CharacterID
	}
	// Avatar IDs come from the server-owned allowlist, which is generated from
	// the same catalog as the client. Do not also apply tokenPattern here: the
	// canonical NPC IDs intentionally contain hyphens (for example s1-hos-l).
	// Exact allowlist membership is the stronger validation boundary.
	if !game.ValidAvatar(avatarID) || message.Sequence == 0 {
		return nil, false
	}
	numbers := []float64{
		message.X,
		message.Y,
		message.Z,
		message.Yaw,
		message.VehicleLift,
		message.VehicleSteering,
		message.VehicleWheelRoll,
		message.VehicleQX,
		message.VehicleQY,
		message.VehicleQZ,
		message.VehicleQW,
	}
	for _, value := range numbers {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1_000_000 {
			return nil, false
		}
	}
	if message.AnimationID == nil {
		return nil, true
	}
	normalized := strings.TrimSpace(*message.AnimationID)
	if normalized == "" {
		return nil, true
	}
	if !tokenPattern.MatchString(normalized) {
		return nil, false
	}
	return &normalized, true
}

func (h *Hub) LeaveWorld(client *Client) {
	h.leaveWorld(client, false)
}

func (h *Hub) leaveWorld(client *Client, preserveArrival bool) bool {
	now := time.Now()
	h.mu.Lock()
	if !preserveArrival {
		delete(h.transitionGrants, client.id)
		delete(h.authorizedArrivals, client.id)
	}
	previous, exists := h.presences[client.id]
	if !exists {
		h.mu.Unlock()
		return false
	}
	delete(h.presences, client.id)
	h.departedWorlds[client.id] = previous.state.WorldID
	releasedCargo := h.markClientCargoForReleaseLocked(client.id, now)
	var replacement *protocol.ForkliftState
	var removedForkliftID string
	if previous.state.VehicleID != nil {
		if state, base := initialForkliftStates()[*previous.state.VehicleID]; base {
			h.forklifts[state.ID] = state
			replacement = &state
		} else {
			removedForkliftID = *previous.state.VehicleID
			delete(h.forklifts, removedForkliftID)
		}
	}
	h.releaseForkliftLocked(client.id, previous.state.VehicleID)
	recipients := h.removeFromRoomLocked(client, previous.state.WorldID)
	h.mu.Unlock()
	h.sendMany(recipients, protocol.PlayerLeft{
		Header:    protocol.NewHeader(protocol.TypePlayerLeft),
		PlayerID:  client.id,
		UpdatedAt: now.UnixMilli(),
	})
	if previous.state.VehicleID != nil {
		h.sendMany(recipients, protocol.ForkliftSound{
			Header:     protocol.NewHeader(protocol.TypeForkliftSound),
			ForkliftID: *previous.state.VehicleID,
			Cue:        "shutdown",
		})
	}
	if replacement != nil {
		h.sendMany(recipients, protocol.ForkliftStateEvent{
			Header:   protocol.NewHeader(protocol.TypeForkliftState),
			Forklift: *replacement,
		})
	}
	if removedForkliftID != "" {
		h.sendMany(recipients, protocol.ForkliftRemoved{
			Header:     protocol.NewHeader(protocol.TypeForkliftRemoved),
			ForkliftID: removedForkliftID,
		})
	}
	for _, cargo := range releasedCargo {
		h.sendMany(recipients, protocol.CargoStateEvent{
			Header: protocol.NewHeader(protocol.TypeCargoState),
			Cargo:  cargo,
		})
		h.scheduleCargoClaimRelease(
			cargo.ID,
			cargo.OwnerID,
			cargo.ClaimExpiresAtMs,
		)
		h.scheduleCargoAutoRight(
			cargo.ID,
			time.UnixMilli(cargo.UpdatedAtMs),
		)
		h.scheduleCargoCleanup(
			cargo.ID,
			time.UnixMilli(cargo.UpdatedAtMs),
		)
	}
	return true
}

func (h *Hub) BroadcastWorldState(state protocol.WorldState) {
	h.mu.RLock()
	recipients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		recipients = append(recipients, client)
	}
	h.mu.RUnlock()
	h.sendMany(recipients, protocol.WorldStateEvent{
		Header:     protocol.NewHeader(protocol.TypeWorldState),
		WorldState: state,
	})
}

func (h *Hub) removeFromRoomLocked(client *Client, worldID string) []*Client {
	room := h.rooms[worldID]
	if room == nil {
		return nil
	}
	delete(room, client.id)
	recipients := make([]*Client, 0, len(room))
	for _, target := range room {
		recipients = append(recipients, target)
	}
	if len(room) == 0 {
		delete(h.rooms, worldID)
	}
	return recipients
}

func (h *Hub) roomRecipientsLocked(worldID, excludeID string) []*Client {
	room := h.rooms[worldID]
	recipients := make([]*Client, 0, len(room))
	for id, client := range room {
		if id != excludeID {
			recipients = append(recipients, client)
		}
	}
	return recipients
}

func (h *Hub) allClientsLocked(excludeID string) []*Client {
	recipients := make([]*Client, 0, len(h.clients))
	for id, client := range h.clients {
		if id != excludeID {
			recipients = append(recipients, client)
		}
	}
	return recipients
}

func (h *Hub) sendOne(client *Client, message any) bool {
	payload, err := json.Marshal(message)
	if err != nil {
		h.logf("failed to marshal outbound message: %v", err)
		return false
	}
	if client.trySend(payload) {
		return true
	}
	go h.Unregister(client)
	return false
}

func (h *Hub) sendMany(clients []*Client, message any) {
	payload, err := json.Marshal(message)
	if err != nil {
		h.logf("failed to marshal outbound message: %v", err)
		return
	}
	for _, client := range clients {
		if !client.trySend(payload) {
			go h.Unregister(client)
		}
	}
}

func (h *Hub) ActiveClients() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// AdminSession is the privacy-conscious live player snapshot exposed to the
// authenticated operations dashboard.
type AdminSession struct {
	ID        string `json:"id"`
	Name      string `json:"char_name"`
	WorldID   string `json:"zone_name,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// AdminSessions returns all connected players without remote IPs or account
// details. The result is sorted for stable dashboard rendering.
func (h *Hub) AdminSessions() []AdminSession {
	h.mu.RLock()
	sessions := make([]AdminSession, 0, len(h.clients))
	for id, client := range h.clients {
		worldID := ""
		if presence, exists := h.presences[id]; exists {
			worldID = presence.state.WorldID
		}
		sessions = append(sessions, AdminSession{
			ID:        id,
			Name:      client.name,
			WorldID:   worldID,
			CreatedAt: client.connectedAt.Unix(),
		})
	}
	h.mu.RUnlock()

	sort.Slice(sessions, func(i, j int) bool {
		left := strings.ToLower(sessions[i].Name)
		right := strings.ToLower(sessions[j].Name)
		if left == right {
			return sessions[i].ID < sessions[j].ID
		}
		return left < right
	})
	return sessions
}
