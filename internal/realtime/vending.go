package realtime

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"regexp"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/activitylog"
	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const vendingRequestTimeout = 5 * time.Second

var vendingRequestIDPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9_-]{7,79}$`,
)

type VendingStore interface {
	PurchaseVendingDrink(
		context.Context,
		int64,
		string,
		string,
		string,
		int64,
		bool,
	) (store.VendingPurchase, error)
}

func secureVendingDraw(denominator int) (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(denominator)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func protocolInventory(items []store.InventoryItem) []protocol.InventoryItem {
	result := make([]protocol.InventoryItem, 0, len(items))
	for _, item := range items {
		result = append(result, protocol.InventoryItem{
			Key: item.Key, Name: item.Name, Description: item.Description,
			Category: item.Category, MaxStack: item.MaxStack,
			Usable: item.Usable, EffectKind: item.EffectKind,
			EffectValue: item.EffectValue, Quantity: item.Quantity,
		})
	}
	return result
}

func (h *Hub) sendVendingResult(
	client *Client,
	result protocol.VendingResult,
) bool {
	result.Header = protocol.NewHeader(protocol.TypeVendingResult)
	return h.sendOne(client, result)
}

func (h *Hub) rejectVending(
	client *Client,
	request protocol.VendingPurchaseRequest,
	outcome,
	message string,
) bool {
	return h.sendVendingResult(client, protocol.VendingResult{
		RequestID: request.RequestID,
		MachineID: request.MachineID,
		DrinkKey:  request.DrinkKey,
		Outcome:   outcome,
		Message:   message,
	})
}

func (h *Hub) HandleVendingPurchase(
	client *Client,
	request protocol.VendingPurchaseRequest,
) bool {
	if !vendingRequestIDPattern.MatchString(request.RequestID) {
		return h.rejectVending(
			client,
			request,
			"invalid_request",
			"Invalid vending request.",
		)
	}
	product, productOK := h.vending.Product(request.DrinkKey)
	machine, machineOK := h.vending.Machine(request.MachineID)
	if !productOK || !machineOK {
		return h.rejectVending(
			client,
			request,
			"invalid_selection",
			"That drink or vending machine is unavailable.",
		)
	}

	h.mu.RLock()
	current := h.clients[client.id]
	presence, inWorld := h.presences[client.id]
	vendingStore := h.vendingStore
	draw := h.vendingDraw
	h.mu.RUnlock()
	if current != client || !client.persistent || vendingStore == nil {
		return h.rejectVending(
			client,
			request,
			"unavailable",
			"Vending requires a connected saved character.",
		)
	}
	if !inWorld || presence.state.VehicleID != nil ||
		!h.vending.IsNear(
			machine,
			presence.state.WorldID,
			presence.state.X,
			presence.state.Z,
		) {
		return h.rejectVending(
			client,
			request,
			"too_far",
			"Move closer to the vending machine.",
		)
	}
	if !client.allowVending(time.Now()) {
		return h.rejectVending(
			client,
			request,
			"rate_limited",
			"Please wait a moment before buying another drink.",
		)
	}

	if draw == nil {
		draw = secureVendingDraw
	}
	chance := h.vending.Manifest().WinningCanChance
	drawValue, err := draw(chance.Denominator)
	if err != nil {
		h.logf("vending random draw failed: %v", err)
		return h.rejectVending(
			client,
			request,
			"server_error",
			"The vending machine could not complete the purchase.",
		)
	}
	winningCan := h.vending.IsWinningDraw(drawValue)

	ctx, cancel := context.WithTimeout(context.Background(), vendingRequestTimeout)
	defer cancel()
	purchase, err := vendingStore.PurchaseVendingDrink(
		ctx,
		client.characterID,
		request.RequestID,
		request.MachineID,
		request.DrinkKey,
		h.vending.UnitPrice(),
		winningCan,
	)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInsufficient):
			return h.rejectVending(
				client,
				request,
				"insufficient_funds",
				"You need ¥100.",
			)
		case errors.Is(err, store.ErrDuplicateEvent):
			return h.rejectVending(
				client,
				request,
				"duplicate_request",
				"That vending request was already used.",
			)
		default:
			h.logf(
				"vending purchase failed for character %d: %v",
				client.characterID,
				err,
			)
			return h.rejectVending(
				client,
				request,
				"server_error",
				"The vending machine could not complete the purchase.",
			)
		}
	}
	client.inventory = append(
		[]store.InventoryItem(nil),
		purchase.Inventory...,
	)
	result := protocol.VendingResult{
		RequestID:  purchase.RequestID,
		MachineID:  purchase.MachineID,
		DrinkKey:   purchase.DrinkKey,
		DrinkName:  product.Name,
		Price:      purchase.Price,
		WinningCan: purchase.WinningCan,
		Outcome:    "purchased",
		Yen:        purchase.Yen,
		Inventory:  protocolInventory(purchase.Inventory),
	}
	if purchase.WinningCan {
		result.Message = "Yeah! A winning can!"
	} else {
		result.Message = product.Name
	}
	h.recordActivity(activitylog.Event{
		Type:      "vending_purchase",
		PlayerID:  client.id,
		Name:      client.name,
		WorldID:   machine.WorldID,
		Text:      product.Name,
		RemoteIP:  client.remoteIP,
		UserAgent: client.userAgent,
	})
	return h.sendVendingResult(client, result)
}
