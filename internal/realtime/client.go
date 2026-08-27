package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
	"github.com/gorilla/websocket"
)

const (
	maxInboundMessageSize = 4096
	writeWait             = 10 * time.Second
	pongWait              = 60 * time.Second
	pingPeriod            = 20 * time.Second
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	id   string
	name string
	// This is connection-scoped and deliberately survives map/room changes.
	announcedInitialEntry  bool
	connectedAt            time.Time
	remoteIP               string
	userAgent              string
	send                   chan []byte
	replace                chan []byte
	done                   chan struct{}
	closeOnce              sync.Once
	rateMu                 sync.Mutex
	presenceTokens         float64
	presenceLast           time.Time
	acceptedChatTimes      []time.Time
	forkliftSoundTimes     []time.Time
	lastVendingAt          time.Time
	lastDiagnosticAt       time.Time
	accountID              int64
	accountType            string
	characterID            int64
	avatarID               string
	initialWorldID         string
	persistent             bool
	inventory              []store.InventoryItem
	scriptMu               sync.Mutex
	activeScriptRun        int64
	automaticScriptWorldID string
	locationMu             sync.Mutex
	currentWorldID         string
	flushMu                sync.Mutex
	dirtyLocation          *store.Location
	locationVersion        uint64
	lastPlaytimeSave       time.Time
}

func newClient(
	hub *Hub,
	conn *websocket.Conn,
	id string,
	name string,
	connection ConnectionMetadata,
) *Client {
	now := time.Now()
	client := &Client{
		hub:              hub,
		conn:             conn,
		id:               id,
		name:             name,
		connectedAt:      now,
		remoteIP:         connection.RemoteIP,
		userAgent:        connection.UserAgent,
		send:             make(chan []byte, 64),
		replace:          make(chan []byte, 1),
		done:             make(chan struct{}),
		presenceTokens:   5,
		presenceLast:     now,
		lastPlaytimeSave: now,
	}
	if connection.Character != nil {
		client.name = connection.Character.Name
		client.accountID = connection.AccountID
		client.accountType = connection.AccountType
		client.characterID = connection.Character.ID
		client.avatarID = connection.Character.AvatarKey
		client.initialWorldID = connection.Character.WorldID
		client.currentWorldID = connection.Character.WorldID
		client.persistent = true
		client.inventory = append([]store.InventoryItem(nil), connection.Inventory...)
	}
	return client
}

func (c *Client) markLocation(location store.Location) {
	if !c.persistent {
		return
	}
	c.locationMu.Lock()
	worldChanged := c.currentWorldID != location.WorldID
	c.currentWorldID = location.WorldID
	c.dirtyLocation = &location
	c.locationVersion++
	c.locationMu.Unlock()
	if worldChanged {
		c.scriptMu.Lock()
		c.automaticScriptWorldID = ""
		c.scriptMu.Unlock()
	}
}

func (c *Client) worldID() string {
	c.locationMu.Lock()
	defer c.locationMu.Unlock()
	return c.currentWorldID
}

func (c *Client) scriptRun() int64 {
	c.scriptMu.Lock()
	defer c.scriptMu.Unlock()
	return c.activeScriptRun
}

func (c *Client) allowDiagnostic(now time.Time) bool {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if !c.lastDiagnosticAt.IsZero() && now.Sub(c.lastDiagnosticAt) < time.Second {
		return false
	}
	c.lastDiagnosticAt = now
	return true
}

func (c *Client) setScriptRun(runID int64) {
	c.scriptMu.Lock()
	c.activeScriptRun = runID
	c.scriptMu.Unlock()
}

func (c *Client) claimAutomaticScriptDispatch(worldID string) bool {
	c.scriptMu.Lock()
	defer c.scriptMu.Unlock()
	if worldID == "" || c.automaticScriptWorldID == worldID {
		return false
	}
	c.automaticScriptWorldID = worldID
	return true
}

func (c *Client) flushLocation(ctx context.Context, saver LocationSaver) error {
	if !c.persistent || saver == nil {
		return nil
	}
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	c.locationMu.Lock()
	if c.dirtyLocation == nil {
		c.locationMu.Unlock()
		return nil
	}
	location := *c.dirtyLocation
	version := c.locationVersion
	playtime := time.Since(c.lastPlaytimeSave)
	c.locationMu.Unlock()
	if err := saver.SaveLocation(ctx, c.characterID, location, playtime); err != nil {
		return err
	}
	c.locationMu.Lock()
	if c.locationVersion == version {
		c.dirtyLocation = nil
	}
	c.lastPlaytimeSave = time.Now()
	c.locationMu.Unlock()
	return nil
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) Name() string {
	c.hub.mu.RLock()
	defer c.hub.mu.RUnlock()
	return c.name
}

func (c *Client) trySend(payload []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- payload:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

func (c *Client) notifyReplacement(payload []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.replace <- payload:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

func (c *Client) allowPresence(now time.Time) bool {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	elapsed := now.Sub(c.presenceLast).Seconds()
	c.presenceLast = now
	c.presenceTokens += elapsed * 20
	if c.presenceTokens > 5 {
		c.presenceTokens = 5
	}
	if c.presenceTokens < 1 {
		return false
	}
	c.presenceTokens--
	return true
}

func (c *Client) allowChat(now time.Time) (bool, string) {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	minuteCutoff := now.Add(-time.Minute)
	kept := c.acceptedChatTimes[:0]
	for _, sentAt := range c.acceptedChatTimes {
		if sentAt.After(minuteCutoff) {
			kept = append(kept, sentAt)
		}
	}
	c.acceptedChatTimes = kept
	if len(c.acceptedChatTimes) >= 15 {
		return false, "You may send at most 15 messages per minute."
	}
	burstCutoff := now.Add(-10 * time.Second)
	burstCount := 0
	for _, sentAt := range c.acceptedChatTimes {
		if sentAt.After(burstCutoff) {
			burstCount++
		}
	}
	if burstCount >= 3 {
		return false, "You may send at most 3 messages every 10 seconds."
	}
	// Rejected attempts return above and never enter the accounting window.
	c.acceptedChatTimes = append(c.acceptedChatTimes, now)
	return true, ""
}

func (c *Client) allowVending(now time.Time) bool {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if !c.lastVendingAt.IsZero() &&
		now.Sub(c.lastVendingAt) < time.Second {
		return false
	}
	c.lastVendingAt = now
	return true
}

func (c *Client) allowForkliftSound(now time.Time) bool {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	cutoff := now.Add(-time.Second)
	kept := c.forkliftSoundTimes[:0]
	for _, sentAt := range c.forkliftSoundTimes {
		if sentAt.After(cutoff) {
			kept = append(kept, sentAt)
		}
	}
	c.forkliftSoundTimes = kept
	if len(c.forkliftSoundTimes) >= 8 {
		return false
	}
	c.forkliftSoundTimes = append(c.forkliftSoundTimes, now)
	return true
}

func (c *Client) readPump() {
	defer c.hub.Unregister(c)
	c.conn.SetReadLimit(maxInboundMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		messageType, payload, err := c.conn.ReadMessage()
		if err != nil {
			var closeErr *websocket.CloseError
			if !errors.As(err, &closeErr) && !errors.Is(err, net.ErrClosed) {
				c.hub.logf("websocket read failed for %s: %v", c.id, err)
			}
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var header protocol.Header
		if err := json.Unmarshal(payload, &header); err != nil {
			continue
		}
		if header.Version != protocol.Version {
			_ = c.conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseProtocolError, "unsupported protocol version"),
				time.Now().Add(writeWait),
			)
			return
		}

		switch header.Type {
		case protocol.TypePresence:
			var message protocol.Presence
			if json.Unmarshal(payload, &message) == nil && c.allowPresence(time.Now()) {
				c.hub.HandlePresence(c, message)
			}
		case protocol.TypeLeaveWorld:
			c.hub.LeaveWorld(c)
		case protocol.TypeTransitionRequest:
			var message protocol.TransitionRequest
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleTransitionRequest(c, message)
			}
		case protocol.TypeTransitionCommit:
			var message protocol.TransitionCommit
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleTransitionCommit(c, message)
			}
		case protocol.TypePlayerDirectoryRequest:
			c.hub.SendPlayerDirectory(c)
		case protocol.TypeChat:
			var message protocol.ChatRequest
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleChat(c, message.Text)
			}
		case protocol.TypeForkliftUpdate:
			var message protocol.ForkliftUpdate
			if json.Unmarshal(payload, &message) == nil &&
				c.allowPresence(time.Now()) {
				c.hub.HandleForkliftUpdate(c, message)
			}
		case protocol.TypeForkliftSound:
			var message protocol.ForkliftSound
			if json.Unmarshal(payload, &message) == nil &&
				c.allowForkliftSound(time.Now()) {
				c.hub.HandleForkliftSound(c, message)
			}
		case protocol.TypeForkliftSpawn:
			var message protocol.ForkliftSpawn
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleForkliftSpawn(c, message)
			}
		case protocol.TypeCargoClaim:
			var message protocol.CargoClaim
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleCargoClaim(c, message)
			}
		case protocol.TypeCargoUpdate:
			var message protocol.CargoUpdate
			if json.Unmarshal(payload, &message) == nil &&
				c.allowPresence(time.Now()) {
				c.hub.HandleCargoUpdate(c, message)
			}
		case protocol.TypeVendingPurchase:
			var message protocol.VendingPurchaseRequest
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleVendingPurchase(c, message)
			}
		case protocol.TypeScriptEventStart:
			var message protocol.ScriptEventStartRequest
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleScriptEventStart(c, message)
			}
		case protocol.TypeScriptEventAdvance:
			var message protocol.ScriptEventAdvanceRequest
			if json.Unmarshal(payload, &message) == nil {
				c.hub.HandleScriptEventAdvance(c, message)
			}
		case protocol.TypeClientDiagnostic:
			var message protocol.ClientDiagnostic
			if json.Unmarshal(payload, &message) == nil &&
				c.allowDiagnostic(time.Now()) &&
				message.Scope == "native-script-activity" &&
				len(message.Payload) > 0 {
				c.hub.logf(
					"client diagnostic character %d run %d %s: %s",
					c.characterID,
					message.RunID,
					message.Scope,
					string(message.Payload),
				)
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.hub.Unregister(c)
	for {
		select {
		case payload := <-c.replace:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
			_ = c.conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(
					websocket.ClosePolicyViolation,
					"session replaced",
				),
				time.Now().Add(writeWait),
			)
			return
		case payload := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(writeWait),
			); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
