package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresGuestUpgradeAndPersistentCharacterState(t *testing.T) {
	databaseURL := os.Getenv("NEW_YOKOSUKA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEW_YOKOSUKA_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	guestHash := HashToken("guest-token-" + suffix)
	account, err := database.GetOrCreateGuest(ctx, guestHash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.db.Exec(`DELETE FROM accounts WHERE id = $1`, account.ID)
	})
	sameAccount, err := database.GetOrCreateGuest(ctx, guestHash)
	if err != nil || sameAccount.ID != account.ID {
		t.Fatalf("guest account was not stable: %#v, %v", sameAccount, err)
	}

	character, err := database.CreateCharacter(
		ctx,
		account.ID,
		"Guest"+suffix,
		"ryo",
		"exterior",
		-6.48,
		0,
		-19.32,
		0,
		100,
	)
	if err != nil {
		t.Fatal(err)
	}
	otherAccount, err := database.GetOrCreateGuest(
		ctx,
		HashToken("other-guest-token-"+suffix),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.db.Exec(`DELETE FROM accounts WHERE id = $1`, otherAccount.ID)
	})
	if _, err := database.CreateCharacter(
		ctx,
		otherAccount.ID,
		strings.ToLower(character.Name),
		"ryo",
		"exterior",
		-6.48,
		0,
		-19.32,
		0,
		100,
	); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("case-insensitive cross-account duplicate err = %v, want ErrNameTaken", err)
	}
	chatPlayerID := "integration-chat-" + suffix
	t.Cleanup(func() {
		_, _ = database.db.Exec(
			`DELETE FROM chat_messages WHERE player_id = $1`,
			chatPlayerID,
		)
	})
	chatSentAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := database.SaveChatMessage(ctx, ChatMessageLog{
		AccountID:   account.ID,
		CharacterID: character.ID,
		PlayerID:    chatPlayerID,
		PlayerName:  character.Name,
		WorldID:     "dobuita",
		Text:        "Hello Yokosuka!",
		RemoteIP:    "127.0.0.1",
		UserAgent:   "integration-test",
		SentAt:      chatSentAt,
	}); err != nil {
		t.Fatal(err)
	}
	var savedChat ChatMessageLog
	if err := database.db.QueryRowContext(ctx, `
		SELECT
			account_id,
			character_id,
			player_id,
			player_name,
			world_id,
			message_text,
			remote_ip,
			user_agent,
			sent_at
		FROM chat_messages
		WHERE player_id = $1`,
		chatPlayerID,
	).Scan(
		&savedChat.AccountID,
		&savedChat.CharacterID,
		&savedChat.PlayerID,
		&savedChat.PlayerName,
		&savedChat.WorldID,
		&savedChat.Text,
		&savedChat.RemoteIP,
		&savedChat.UserAgent,
		&savedChat.SentAt,
	); err != nil {
		t.Fatal(err)
	}
	if savedChat.AccountID != account.ID ||
		savedChat.CharacterID != character.ID ||
		savedChat.PlayerName != character.Name ||
		savedChat.WorldID != "dobuita" ||
		savedChat.Text != "Hello Yokosuka!" ||
		savedChat.RemoteIP != "127.0.0.1" ||
		savedChat.UserAgent != "integration-test" ||
		!savedChat.SentAt.Equal(chatSentAt) {
		t.Fatalf("chat message did not round-trip: %#v", savedChat)
	}
	recentChat, err := database.RecentChatMessages(ctx, MaxRecentChatMessages)
	if err != nil {
		t.Fatal(err)
	}
	foundRecentChat := false
	for _, message := range recentChat {
		if message.PlayerID == chatPlayerID {
			foundRecentChat = message.Text == "Hello Yokosuka!" &&
				message.SentAt.Equal(chatSentAt)
			break
		}
	}
	if !foundRecentChat {
		t.Fatalf("saved chat was absent from recent history: %#v", recentChat)
	}
	if character.Yen != 2000 {
		t.Fatalf("starting yen = %d, want 2000", character.Yen)
	}
	dialogue, err := database.DialogueState(ctx, account.ID, character.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dialogue.Revision != 0 || len(dialogue.Progress) != 325*12 {
		t.Fatalf("unexpected default dialogue state: %#v", dialogue)
	}
	dialogue.State.Banks.Bank2[13] = 1
	savedDialogue, err := database.SaveDialogueState(
		ctx,
		account.ID,
		character.ID,
		dialogue,
	)
	if err != nil {
		t.Fatal(err)
	}
	if savedDialogue.Revision != 1 {
		t.Fatalf("saved dialogue revision = %d, want 1", savedDialogue.Revision)
	}
	savedDialogue.State.Banks.Bank2[14] = 1
	resavedDialogue, err := database.SaveDialogueState(
		ctx,
		account.ID,
		character.ID,
		savedDialogue,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resavedDialogue.Revision != 2 {
		t.Fatalf(
			"resaved dialogue revision = %d, want 2",
			resavedDialogue.Revision,
		)
	}
	if _, err := database.SaveDialogueState(
		ctx,
		account.ID,
		character.ID,
		dialogue,
	); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale dialogue save err = %v, want ErrRevisionConflict", err)
	}
	reloadedDialogue, err := database.DialogueState(
		ctx,
		account.ID,
		character.ID,
	)
	if err != nil ||
		reloadedDialogue.Revision != 2 ||
		reloadedDialogue.State.Banks.Bank2[13] != 1 ||
		reloadedDialogue.State.Banks.Bank2[14] != 1 {
		t.Fatalf(
			"dialogue state did not round-trip: %#v, %v",
			reloadedDialogue,
			err,
		)
	}
	inventory, err := database.Inventory(ctx, character.ID)
	if err != nil || len(inventory) != 0 {
		t.Fatalf("unexpected starting inventory: %#v, %v", inventory, err)
	}

	location := Location{WorldID: "dobuita", X: 1, Y: 2, Z: 3, Yaw: 1.25}
	if err := database.SaveLocation(ctx, character.ID, location, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	reloaded, err := database.CharacterForAccount(ctx, account.ID, character.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.WorldID != "dobuita" || reloaded.X != 1 || reloaded.TimePlayedSeconds != 3 {
		t.Fatalf("location did not persist: %#v", reloaded)
	}

	upgraded, err := database.UpgradeGuestAccount(
		ctx,
		account.ID,
		"player-"+suffix+"@example.test",
		"$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
	)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.ID != account.ID || upgraded.AccountType != "registered" {
		t.Fatalf("guest upgrade changed identity: %#v", upgraded)
	}
	if _, err := database.CharacterForAccount(ctx, upgraded.ID, character.ID); err != nil {
		t.Fatalf("guest character was not preserved: %v", err)
	}

	quantity, err := database.GrantItem(
		ctx, character.ID, "toy_capsule", 2, "grant-"+suffix, "integration test",
	)
	if err != nil || quantity != 2 {
		t.Fatalf("grant item: quantity=%d err=%v", quantity, err)
	}
	if _, err := database.GrantItem(
		ctx, character.ID, "toy_capsule", 2, "grant-"+suffix, "integration test",
	); !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("duplicate grant err = %v, want ErrDuplicateEvent", err)
	}
	if _, err := database.SpendYen(
		ctx, character.ID, 10_000, "", "integration test",
	); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("overspend err = %v, want ErrInsufficient", err)
	}

	vendingRequest := "vending-" + suffix
	purchase, err := database.PurchaseVendingDrink(
		ctx,
		character.ID,
		vendingRequest,
		"dobuita-vm-0-0",
		"jet_cola",
		100,
		true,
	)
	if err != nil || !purchase.WinningCan {
		t.Fatalf("winning vending purchase: %#v, %v", purchase, err)
	}
	state, err := database.CharacterState(ctx, account.ID, character.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Character.Yen != 1900 ||
		len(state.Inventory) != 2 ||
		state.Inventory[1].Key != "winning_can" ||
		state.Inventory[1].Quantity != 1 {
		t.Fatalf("unexpected vending state: %#v", state)
	}
	repeated, err := database.PurchaseVendingDrink(
		ctx,
		character.ID,
		vendingRequest,
		"dobuita-vm-0-0",
		"jet_cola",
		100,
		false,
	)
	if err != nil ||
		repeated.RequestID != purchase.RequestID ||
		repeated.MachineID != purchase.MachineID ||
		repeated.DrinkKey != purchase.DrinkKey ||
		repeated.Price != purchase.Price ||
		repeated.WinningCan != purchase.WinningCan ||
		repeated.Yen != purchase.Yen ||
		len(repeated.Inventory) != len(purchase.Inventory) {
		t.Fatalf("idempotent vending replay: %#v, %v", repeated, err)
	}
	state, err = database.CharacterState(ctx, account.ID, character.ID)
	if err != nil || state.Character.Yen != 1900 {
		t.Fatalf("vending replay changed balance: %#v, %v", state, err)
	}
	if _, err := database.PurchaseVendingDrink(
		ctx,
		character.ID,
		vendingRequest,
		"dobuita-vm-0-0",
		"fruda_orange",
		100,
		false,
	); !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("conflicting vending replay err = %v", err)
	}

	progression, err := database.AwardExperience(
		ctx, character.ID, 100, "xp-"+suffix, "integration test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if progression.Level != 2 || progression.MaxHP != 100 || progression.CurrentHP != 100 {
		t.Fatalf("unexpected level-up state: %#v", progression)
	}
	progression, err = database.ApplyDamage(
		ctx, character.ID, 35, "damage-"+suffix, "integration test",
	)
	if err != nil || progression.CurrentHP != 65 || progression.MaxHP != 100 {
		t.Fatalf("unexpected damage state: %#v, %v", progression, err)
	}
	if _, err := database.ApplyDamage(
		ctx, character.ID, 35, "damage-"+suffix, "integration test retry",
	); !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("duplicate damage err = %v, want ErrDuplicateEvent", err)
	}
	progression, err = database.Heal(
		ctx, character.ID, 500, "heal-"+suffix, "integration test",
	)
	if err != nil || progression.CurrentHP != 100 || progression.MaxHP != 100 {
		t.Fatalf("unexpected healing state: %#v, %v", progression, err)
	}
}
