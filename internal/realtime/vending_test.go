package realtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type recordingVendingStore struct {
	calls       int
	purchase    store.VendingPurchase
	purchaseErr error
}

func (s *recordingVendingStore) PurchaseVendingDrink(
	_ context.Context,
	_ int64,
	requestID,
	machineID,
	drinkKey string,
	price int64,
	winningCan bool,
) (store.VendingPurchase, error) {
	s.calls++
	if s.purchaseErr != nil {
		return store.VendingPurchase{}, s.purchaseErr
	}
	s.purchase = store.VendingPurchase{
		RequestID: requestID, MachineID: machineID, DrinkKey: drinkKey,
		Price: price, WinningCan: winningCan, Yen: 400,
		Inventory: []store.InventoryItem{{
			ItemDefinition: store.ItemDefinition{
				Key: "winning_can", Name: "Winning Can",
				Category: "collectible", MaxStack: 99,
			},
			Quantity: 1,
		}},
	}
	return s.purchase, nil
}

func persistentVendingClient(
	hub *Hub,
	worldID string,
	x,
	z float64,
) *Client {
	client := &Client{
		hub: hub, id: "vending-client", name: "Ryo",
		accountID: 11, characterID: 22, persistent: true,
		send: make(chan []byte, 4), done: make(chan struct{}),
	}
	hub.clients[client.id] = client
	hub.presences[client.id] = storedPresence{
		state: protocol.PlayerState{
			ID: client.id, WorldID: worldID,
			X: x, Z: z, Movement: "idle", Sequence: 1,
		},
	}
	return client
}

func readVendingResult(t *testing.T, client *Client) protocol.VendingResult {
	t.Helper()
	payload := <-client.send
	var result protocol.VendingResult
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestVendingPurchaseRequiresExactMachineProximityAndReturnsState(
	t *testing.T,
) {
	rig := newTestRig(t, 10)
	machine, ok := rig.hub.vending.Machine("dobuita-vm-0-0")
	if !ok {
		t.Fatal("missing Dobuita vending machine")
	}
	database := &recordingVendingStore{}
	rig.hub.vendingStore = database
	rig.hub.vendingDraw = func(denominator int) (int, error) {
		if denominator != 10 {
			t.Fatalf("draw denominator = %d, want 10", denominator)
		}
		return 0, nil
	}
	client := persistentVendingClient(
		rig.hub,
		machine.WorldID,
		machine.Position[0],
		machine.Position[2],
	)
	request := protocol.VendingPurchaseRequest{
		Header:    protocol.NewHeader(protocol.TypeVendingPurchase),
		RequestID: "purchase-0001",
		MachineID: machine.ID,
		DrinkKey:  "jet_cola",
	}
	if !rig.hub.HandleVendingPurchase(client, request) {
		t.Fatal("purchase result was not sent")
	}
	result := readVendingResult(t, client)
	if result.Outcome != "purchased" || result.Price != 100 ||
		!result.WinningCan || result.Yen != 400 ||
		len(result.Inventory) != 1 {
		t.Fatalf("unexpected vending result: %#v", result)
	}
	if database.calls != 1 || !database.purchase.WinningCan {
		t.Fatalf("unexpected vending store call: %#v", database.purchase)
	}

	rig.hub.presences[client.id] = storedPresence{
		state: protocol.PlayerState{
			ID: client.id, WorldID: machine.WorldID,
			X: machine.Position[0] + machine.InteractionRadius + 1,
			Z: machine.Position[2], Movement: "idle", Sequence: 2,
		},
	}
	request.RequestID = "purchase-0002"
	rig.hub.HandleVendingPurchase(client, request)
	rejected := readVendingResult(t, client)
	if rejected.Outcome != "too_far" || database.calls != 1 {
		t.Fatalf("distant purchase was not rejected: %#v", rejected)
	}

	rig.hub.presences[client.id] = storedPresence{
		state: protocol.PlayerState{
			ID: client.id, WorldID: machine.WorldID,
			X: machine.Position[0], Z: machine.Position[2],
			Movement: "idle", Sequence: 3,
		},
	}
	request.RequestID = "purchase-0003"
	rig.hub.HandleVendingPurchase(client, request)
	rateLimited := readVendingResult(t, client)
	if rateLimited.Outcome != "rate_limited" || database.calls != 1 {
		t.Fatalf("rapid purchase was not throttled: %#v", rateLimited)
	}
}

func TestVendingAnimationsAreValidNetworkPresence(t *testing.T) {
	for _, id := range []string{"vendingDrink", "vendingCoffee"} {
		animation := id
		normalized, ok := validatePresence(protocol.Presence{
			WorldID: "dobuita", CharacterID: "ryo",
			Movement: "idle", Sequence: 1, AnimationID: &animation,
		})
		if !ok || normalized == nil || *normalized != id {
			t.Fatalf("vending animation %q was rejected", id)
		}
	}
}
