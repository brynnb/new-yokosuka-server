package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestPostgresScriptReviewThreadsPreserveVersionDiscussion(t *testing.T) {
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
	passwordHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := database.CreateRegisteredAccount(ctx, "review-owner-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := database.CreateRegisteredAccount(ctx, "reviewer-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := database.CreateRegisteredAccount(ctx, "review-outsider-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	created, err := database.CreateYarnScript(ctx, owner.ID, YarnScriptCreateInput{
		Slug: "review-thread-" + suffix, Title: "Review thread integration",
		SourceText: "title: Start\n---\nRyo: I understand.\n===\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateScriptReviewThread(ctx, owner.ID, created.ID, 1, nil, "Draft thread"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mutable draft accepted a review thread: %v", err)
	}
	if _, err := database.SubmitScriptVersion(ctx, owner.ID, created.ID, 1); err != nil {
		t.Fatal(err)
	}
	mutableChild, err := database.CreateScriptVersion(ctx, owner.ID, created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `
		INSERT INTO script_review_threads (version_id,created_by) VALUES ($1,$2)
	`, mutableChild.ID, owner.ID); err == nil {
		t.Fatal("database accepted a review thread on a mutable draft")
	}
	t.Cleanup(func() {
		_, _ = database.db.Exec(`UPDATE scripts SET current_published_version_id=NULL,current_reference_version_id=NULL WHERE id=$1`, created.ID)
		_, _ = database.db.Exec(`DELETE FROM scripts WHERE id=$1`, created.ID)
		for _, accountID := range []int64{owner.ID, reviewer.ID, outsider.ID} {
			_, _ = database.db.Exec(`DELETE FROM accounts WHERE id=$1`, accountID)
		}
	})

	line := 3
	thread, err := database.CreateScriptReviewThread(
		ctx, owner.ID, created.ID, 1, &line, "Keep this line faithful to the original voice.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if thread.LineNumber == nil || *thread.LineNumber != 3 || len(thread.Comments) != 1 {
		t.Fatalf("created thread=%#v", thread)
	}
	badLine := 99
	if _, err := database.CreateScriptReviewThread(ctx, owner.ID, created.ID, 1, &badLine, "Bad anchor"); err == nil {
		t.Fatal("review accepted a line outside the immutable saved source")
	}
	if _, err := database.ListScriptReviewThreads(ctx, outsider.ID, created.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider listed private draft discussion: %v", err)
	}
	if _, err := database.SetScriptCollaborator(ctx, owner.ID, created.ID, reviewer.Email, "reviewer"); err != nil {
		t.Fatal(err)
	}
	comment, err := database.AddScriptReviewComment(
		ctx, reviewer.ID, created.ID, thread.ID, "Checked against the saved fixture.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_review_comments SET body='changed' WHERE id=$1`, comment.ID); err == nil {
		t.Fatal("immutable review comment was mutable")
	}
	resolved, err := database.SetScriptReviewThreadResolved(ctx, reviewer.ID, created.ID, thread.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != "resolved" || resolved.ResolvedBy == nil || *resolved.ResolvedBy != reviewer.ID || len(resolved.Comments) != 2 {
		t.Fatalf("resolved thread=%#v", resolved)
	}
	if _, err := database.SetScriptReviewThreadResolved(ctx, owner.ID, created.ID, thread.ID, false); err != nil {
		t.Fatal(err)
	}
	threads, err := database.ListScriptReviewThreads(ctx, reviewer.ID, created.ID, 1)
	if err != nil || len(threads) != 1 || threads[0].Status != "open" || len(threads[0].Comments) != 2 {
		t.Fatalf("threads=%#v err=%v", threads, err)
	}
	events, err := database.ListScriptModerationEvents(ctx, reviewer.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantActions := []string{
		"review-thread.reopened",
		"review-thread.resolved",
		"collaborator.added",
		"version.submitted",
	}
	if len(events) != len(wantActions) {
		t.Fatalf("moderation events=%#v", events)
	}
	for index, action := range wantActions {
		if events[index].Action != action {
			t.Fatalf("moderation event %d=%q, want %q", index, events[index].Action, action)
		}
	}
	if _, err := database.ListScriptModerationEvents(ctx, outsider.ID, created.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider listed private moderation history: %v", err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_moderation_events SET action='script.archived' WHERE id=$1`, events[0].ID); err == nil {
		t.Fatal("immutable moderation event was mutable")
	}
}
