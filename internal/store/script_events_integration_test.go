package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

func TestPostgresScriptEventTransactionLifecycle(t *testing.T) {
	databaseURL := os.Getenv("NEW_YOKOSUKA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEW_YOKOSUKA_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetScriptCompiler(integrationYarnCompiler{})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := "HATO_TEST_" + suffix
	account, err := database.CreateRegisteredAccount(
		ctx, "script-event-"+suffix+"@example.test",
		"$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
	)
	if err != nil {
		t.Fatal(err)
	}
	character, err := database.CreateCharacter(ctx, account.ID, "Event"+suffix, "ryo", "D000", 0, 0, 0, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	created, err := database.CreateYarnScript(ctx, account.ID, YarnScriptCreateInput{
		Slug: "script-event-" + suffix, Title: "Script event integration",
		SourceText: "title: Start\n---\nRyo: Hello\n===\n",
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "talk", Area: "D000", Actor: actor, Priority: 50,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := database.db.Exec(`DELETE FROM characters WHERE id=$1`, character.ID); err != nil {
			t.Errorf("delete script-event character: %v", err)
		}
		if _, err := database.db.Exec(`UPDATE scripts SET current_published_version_id=NULL WHERE id=$1`, created.ID); err != nil {
			t.Errorf("clear script-event pointer: %v", err)
		}
		if _, err := database.db.Exec(`DELETE FROM scripts WHERE id=$1`, created.ID); err != nil {
			t.Errorf("delete script-event script: %v", err)
		}
		if _, err := database.db.Exec(`DELETE FROM accounts WHERE id=$1`, account.ID); err != nil {
			t.Errorf("delete script-event account: %v", err)
		}
	})
	if _, err := database.SubmitScriptVersion(ctx, account.ID, created.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE accounts SET role='admin' WHERE id=$1`, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.PublishScriptVersion(ctx, account.ID, created.ID, 1); err != nil {
		t.Fatal(err)
	}

	selector := scriptcontent.TriggerSelector{Kind: "talk", Area: "D000", Actor: actor}
	event, err := database.StartPublishedScriptEvent(ctx, account.ID, character.ID, selector)
	if err != nil {
		t.Fatal(err)
	}
	if event.ScriptID != created.ID || event.EntryNode != "Start" || event.State.Yen != 2000 || event.State.Scene != "D000" {
		t.Fatalf("unexpected started event: %#v", event)
	}
	if _, err := database.StartPublishedScriptEvent(ctx, account.ID, character.ID, selector); !errors.Is(err, ErrScriptEventActive) {
		t.Fatalf("second active event err=%v, want ErrScriptEventActive", err)
	}
	if _, err := database.RenewScriptEvent(ctx, event.RunID, event.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordScriptEventStep(ctx, event.RunID, event.LeaseToken, ScriptEventTraceStep{
		Ordinal: 1, RuntimeSequence: 1, Direction: "runtime", Kind: "nodeStart",
		Payload: json.RawMessage(`{"type":"nodeStart","sequence":1,"node":"Start"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordScriptEventStep(ctx, event.RunID, event.LeaseToken, ScriptEventTraceStep{
		Ordinal: 2, RuntimeSequence: 1, Direction: "controller", Kind: "continue",
		Payload: json.RawMessage(`{"type":"continue"}`),
	}); err != nil {
		t.Fatal(err)
	}
	newRevision, err := database.CompleteScriptEvent(ctx, event.RunID, event.LeaseToken, event.State.Revision, []ScriptEffect{
		{Sequence: 3, Name: "set_flag", Arguments: []scriptcontent.CompiledArgument{staticArgument("string", "hato.met")}},
		{Sequence: 4, Name: "set_progress", Arguments: []scriptcontent.CompiledArgument{staticArgument("string", "hato.stage"), staticArgument("number", "2")}},
		{Sequence: 5, Name: "increment_progress", Arguments: []scriptcontent.CompiledArgument{staticArgument("string", "hato.stage"), staticArgument("number", "1")}},
		{Sequence: 6, Name: "spend_yen", Arguments: []scriptcontent.CompiledArgument{staticArgument("number", "100"), staticArgument("string", "telephone")}},
		{Sequence: 7, Name: "give_item", Arguments: []scriptcontent.CompiledArgument{staticArgument("string", "toy_capsule"), staticArgument("number", "2")}},
	})
	if err != nil || newRevision != event.State.Revision+1 {
		t.Fatalf("complete event revision=%d err=%v", newRevision, err)
	}
	var flag bool
	var progress float64
	var yen int64
	var quantity int
	if err := database.db.QueryRowContext(ctx, `SELECT value FROM character_story_flags WHERE character_id=$1 AND key='hato.met'`, character.ID).Scan(&flag); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRowContext(ctx, `SELECT value FROM character_story_progress WHERE character_id=$1 AND key='hato.stage'`, character.ID).Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRowContext(ctx, `SELECT yen FROM characters WHERE id=$1`, character.ID).Scan(&yen); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRowContext(ctx, `SELECT quantity FROM character_inventory WHERE character_id=$1 AND item_key='toy_capsule'`, character.ID).Scan(&quantity); err != nil {
		t.Fatal(err)
	}
	if !flag || progress != 3 || yen != 1900 || quantity != 2 {
		t.Fatalf("committed state flag=%v progress=%v yen=%d quantity=%d", flag, progress, yen, quantity)
	}
	var traceCount int
	if err := database.db.QueryRowContext(ctx, `SELECT count(*) FROM script_event_trace WHERE run_id=$1`, event.RunID).Scan(&traceCount); err != nil {
		t.Fatal(err)
	}
	if traceCount != 2 {
		t.Fatalf("trace count=%d, want 2", traceCount)
	}
	if err := database.RecordScriptEventStep(ctx, event.RunID, event.LeaseToken, ScriptEventTraceStep{
		Ordinal: 3, RuntimeSequence: 2, Direction: "runtime", Kind: "complete",
		Payload: json.RawMessage(`{"type":"complete","sequence":2}`),
	}); !errors.Is(err, ErrScriptEventEnded) {
		t.Fatalf("terminal run accepted trace step: %v", err)
	}

	cancelled, err := database.StartPublishedScriptEvent(ctx, account.ID, character.ID, selector)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CancelScriptEvent(ctx, cancelled.RunID, cancelled.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CompleteScriptEvent(ctx, cancelled.RunID, cancelled.LeaseToken, cancelled.State.Revision, nil); !errors.Is(err, ErrScriptEventEnded) {
		t.Fatalf("cancelled completion err=%v, want ErrScriptEventEnded", err)
	}
	if _, err := database.StartPublishedScriptEvent(ctx, account.ID, character.ID,
		scriptcontent.TriggerSelector{Kind: "talk", Area: "D000", Actor: "INE_"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexact trigger err=%v, want ErrNotFound", err)
	}
	if _, err := database.SetScriptArchived(ctx, account.ID, created.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.StartPublishedScriptEvent(ctx, account.ID, character.ID, selector); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived trigger err=%v, want ErrNotFound", err)
	}
}

func staticArgument(kind, value string) scriptcontent.CompiledArgument {
	return scriptcontent.CompiledArgument{Type: kind, IsStatic: true, Value: &value}
}
