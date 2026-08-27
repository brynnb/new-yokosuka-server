package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

const ScriptEventLeaseDuration = 5 * time.Minute

type PublishedScriptEvent struct {
	RunID        int64                         `json:"runId"`
	LeaseToken   string                        `json:"-"`
	LeaseExpires time.Time                     `json:"leaseExpiresAt"`
	AccountID    int64                         `json:"accountId"`
	CharacterID  int64                         `json:"characterId"`
	ScriptID     int64                         `json:"scriptId"`
	ScriptSlug   string                        `json:"scriptSlug"`
	VersionID    int64                         `json:"versionId"`
	EntryNode    string                        `json:"entryNode"`
	Trigger      scriptcontent.TriggerSelector `json:"trigger"`
	Program      []byte                        `json:"-"`
	Lines        []scriptcontent.CompiledLine  `json:"lines"`
	State        CharacterScriptState          `json:"state"`
}

type CharacterScriptState struct {
	Revision  int64              `json:"revision"`
	Scene     string             `json:"scene"`
	Yen       int64              `json:"yen"`
	Flags     map[string]bool    `json:"flags"`
	Progress  map[string]float64 `json:"progress"`
	Inventory map[string]int     `json:"inventory"`
}

type ScriptEffect struct {
	Sequence  int                              `json:"sequence"`
	Name      string                           `json:"name"`
	Arguments []scriptcontent.CompiledArgument `json:"arguments"`
}

// ScriptEventTraceStep is one immutable runtime emission or controller input
// in the exact order observed by the authoritative event engine.
type ScriptEventTraceStep struct {
	Ordinal         int
	RuntimeSequence int
	Direction       string
	Kind            string
	Payload         json.RawMessage
}

type triggerCandidate struct {
	scriptID, versionID int64
	slug, entryNode     string
	priority            int
	program             []byte
	lines               []scriptcontent.CompiledLine
}

func resolvePublishedTrigger(
	ctx context.Context,
	tx *sql.Tx,
	selector scriptcontent.TriggerSelector,
	excludedVersionIDs map[int64]bool,
) (triggerCandidate, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT s.id, s.slug, v.id, t.node_id, t.priority, v.compiled_program, v.compiler_lines
		FROM scripts s
		JOIN script_versions v ON v.id = s.current_published_version_id
		JOIN script_version_triggers t ON t.version_id = v.id
		WHERE v.status = 'published'
		  AND s.archived_at IS NULL
		  AND v.content_format = 'yarn'
		  AND v.compile_status = 'valid'
		  AND v.command_schema_version = $1
		  AND t.kind = $2
		  AND t.area IS NOT DISTINCT FROM NULLIF($3, '')
		  AND t.actor IS NOT DISTINCT FROM NULLIF($4, '')
		  AND t.object_key IS NOT DISTINCT FROM NULLIF($5, '')
		  AND t.activity_key IS NOT DISTINCT FROM NULLIF($6, '')
		ORDER BY t.priority DESC, v.id`,
		scriptcontent.YarnCommandSchemaVersion, selector.Kind, selector.Area,
		selector.Actor, selector.Object, selector.Activity,
	)
	if err != nil {
		return triggerCandidate{}, fmt.Errorf("resolve published script trigger: %w", err)
	}
	defer rows.Close()
	candidates := make([]triggerCandidate, 0, 2)
	for rows.Next() {
		var candidate triggerCandidate
		var encodedLines []byte
		if err := rows.Scan(
			&candidate.scriptID, &candidate.slug, &candidate.versionID,
			&candidate.entryNode, &candidate.priority, &candidate.program, &encodedLines,
		); err != nil {
			return triggerCandidate{}, fmt.Errorf("scan published script trigger: %w", err)
		}
		if err := json.Unmarshal(encodedLines, &candidate.lines); err != nil {
			return triggerCandidate{}, fmt.Errorf("decode published script lines: %w", err)
		}
		if excludedVersionIDs[candidate.versionID] {
			continue
		}
		candidates = append(candidates, candidate)
		if len(candidates) == 2 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return triggerCandidate{}, err
	}
	if len(candidates) == 0 {
		return triggerCandidate{}, ErrNotFound
	}
	if len(candidates) > 1 && candidates[0].priority == candidates[1].priority {
		return triggerCandidate{}, ErrAmbiguousTrigger
	}
	return candidates[0], nil
}

func (s *Store) StartPublishedScriptEvent(
	ctx context.Context,
	accountID, characterID int64,
	requested scriptcontent.TriggerSelector,
) (PublishedScriptEvent, error) {
	return s.StartPublishedScriptEventExcluding(ctx, accountID, characterID, requested, nil)
}

// StartPublishedScriptEventExcluding starts the highest-priority matching
// candidate not already passed during the same trigger dispatch.
func (s *Store) StartPublishedScriptEventExcluding(
	ctx context.Context,
	accountID, characterID int64,
	requested scriptcontent.TriggerSelector,
	excludedVersionIDs []int64,
) (PublishedScriptEvent, error) {
	selector, err := scriptcontent.NormalizeTriggerSelector(requested)
	if err != nil {
		return PublishedScriptEvent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PublishedScriptEvent{}, err
	}
	defer tx.Rollback()

	var state CharacterScriptState
	if err := tx.QueryRowContext(ctx, `
		SELECT character.world_id, character.yen
		FROM characters character
		WHERE character.id=$1 AND character.account_id=$2 AND character.deleted_at IS NULL
		FOR UPDATE`, characterID, accountID).Scan(&state.Scene, &state.Yen); errors.Is(err, sql.ErrNoRows) {
		return PublishedScriptEvent{}, ErrNotFound
	} else if err != nil {
		return PublishedScriptEvent{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE script_event_runs
		SET status='expired', finished_at=now(), updated_at=now()
		WHERE character_id=$1 AND status='active' AND lease_expires_at <= now()`, characterID); err != nil {
		return PublishedScriptEvent{}, err
	}
	excluded := make(map[int64]bool, len(excludedVersionIDs))
	for _, versionID := range excludedVersionIDs {
		excluded[versionID] = true
	}
	candidate, err := resolvePublishedTrigger(ctx, tx, selector, excluded)
	if err != nil {
		return PublishedScriptEvent{}, err
	}
	if selector.Area != "" {
		state.Scene = selector.Area
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO character_script_state (character_id) VALUES ($1)
		ON CONFLICT (character_id) DO NOTHING`, characterID); err != nil {
		return PublishedScriptEvent{}, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT revision FROM character_script_state WHERE character_id=$1 FOR UPDATE`,
		characterID,
	).Scan(&state.Revision); err != nil {
		return PublishedScriptEvent{}, err
	}
	state.Flags, state.Progress, state.Inventory = map[string]bool{}, map[string]float64{}, map[string]int{}
	if err := loadCharacterScriptState(ctx, tx, characterID, &state); err != nil {
		return PublishedScriptEvent{}, err
	}
	token, err := newLeaseToken()
	if err != nil {
		return PublishedScriptEvent{}, err
	}
	expires := time.Now().UTC().Add(ScriptEventLeaseDuration)
	var runID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO script_event_runs (
			character_id,version_id,entry_node,trigger_kind,lease_token,
			lease_expires_at,state_revision
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id`, characterID, candidate.versionID, candidate.entryNode,
		selector.Kind, token, expires, state.Revision).Scan(&runID)
	if isUniqueViolation(err) {
		return PublishedScriptEvent{}, ErrScriptEventActive
	}
	if err != nil {
		return PublishedScriptEvent{}, fmt.Errorf("start script event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return PublishedScriptEvent{}, err
	}
	return PublishedScriptEvent{
		RunID: runID, LeaseToken: token, LeaseExpires: expires,
		AccountID: accountID, CharacterID: characterID,
		ScriptID: candidate.scriptID, ScriptSlug: candidate.slug,
		VersionID: candidate.versionID, EntryNode: candidate.entryNode,
		Trigger: selector, Program: candidate.program, Lines: candidate.lines, State: state,
	}, nil
}

func loadCharacterScriptState(ctx context.Context, tx *sql.Tx, characterID int64, state *CharacterScriptState) error {
	flagRows, err := tx.QueryContext(ctx, `SELECT key,value FROM character_story_flags WHERE character_id=$1`, characterID)
	if err != nil {
		return err
	}
	for flagRows.Next() {
		var key string
		var value bool
		if err := flagRows.Scan(&key, &value); err != nil {
			flagRows.Close()
			return err
		}
		state.Flags[key] = value
	}
	if err := flagRows.Close(); err != nil {
		return err
	}
	progressRows, err := tx.QueryContext(ctx, `SELECT key,value FROM character_story_progress WHERE character_id=$1`, characterID)
	if err != nil {
		return err
	}
	for progressRows.Next() {
		var key string
		var value float64
		if err := progressRows.Scan(&key, &value); err != nil {
			progressRows.Close()
			return err
		}
		state.Progress[key] = value
	}
	if err := progressRows.Close(); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT item_key,quantity FROM character_inventory WHERE character_id=$1`, characterID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var quantity int
		if err := rows.Scan(&key, &quantity); err != nil {
			return err
		}
		state.Inventory[key] = quantity
	}
	return rows.Err()
}

func (s *Store) RenewScriptEvent(ctx context.Context, runID int64, token string) (time.Time, error) {
	expires := time.Now().UTC().Add(ScriptEventLeaseDuration)
	var saved time.Time
	err := s.db.QueryRowContext(ctx, `
		UPDATE script_event_runs SET lease_expires_at=$3,updated_at=now()
		WHERE id=$1 AND lease_token=$2 AND status='active' AND lease_expires_at > now()
		RETURNING lease_expires_at`, runID, token, expires).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrScriptEventEnded
	}
	return saved, err
}

func (s *Store) RecordScriptEventStep(ctx context.Context, runID int64, token string, step ScriptEventTraceStep) error {
	if step.Ordinal <= 0 || step.RuntimeSequence < 0 ||
		(step.Direction != "runtime" && step.Direction != "controller") ||
		strings.TrimSpace(step.Kind) == "" || !json.Valid(step.Payload) {
		return errors.New("invalid script event trace step")
	}
	var payloadObject map[string]any
	if err := json.Unmarshal(step.Payload, &payloadObject); err != nil || payloadObject == nil {
		return errors.New("script event trace payload must be a JSON object")
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO script_event_trace (
			run_id,ordinal,runtime_sequence,direction,kind,payload
		)
		SELECT id,$3,$4,$5,$6,$7
		FROM script_event_runs
		WHERE id=$1 AND lease_token=$2 AND status='active'
		  AND lease_expires_at > now()`, runID, token, step.Ordinal,
		step.RuntimeSequence, step.Direction, step.Kind, step.Payload)
	if err != nil {
		return fmt.Errorf("record script event trace step: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrScriptEventEnded
	}
	return nil
}

func (s *Store) CancelScriptEvent(ctx context.Context, runID int64, token string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE script_event_runs
		SET status='cancelled',finished_at=now(),updated_at=now()
		WHERE id=$1 AND lease_token=$2 AND status='active'`, runID, token)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrScriptEventEnded
	}
	return nil
}

func (s *Store) FailScriptEvent(ctx context.Context, runID int64, token, code, message string) error {
	code, message = strings.TrimSpace(code), strings.TrimSpace(message)
	if code == "" || message == "" {
		return errors.New("script event failure code and message are required")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE script_event_runs
		SET status='failed',failure_code=$3,failure_message=$4,finished_at=now(),updated_at=now()
		WHERE id=$1 AND lease_token=$2 AND status='active'`, runID, token, code, message)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrScriptEventEnded
	}
	return nil
}

func (s *Store) CompleteScriptEvent(
	ctx context.Context,
	runID int64,
	token string,
	expectedRevision int64,
	effects []ScriptEffect,
) (int64, error) {
	return s.finishScriptEvent(ctx, runID, token, expectedRevision, effects, "completed", true)
}

// PassScriptEvent commits explicit gate-state effects while recording that the
// candidate declined the trigger so dispatch may continue at lower priority.
func (s *Store) PassScriptEvent(
	ctx context.Context,
	runID int64,
	token string,
	expectedRevision int64,
	effects []ScriptEffect,
) (int64, error) {
	return s.finishScriptEvent(ctx, runID, token, expectedRevision, effects, "passed", false)
}

func (s *Store) finishScriptEvent(
	ctx context.Context,
	runID int64,
	token string,
	expectedRevision int64,
	effects []ScriptEffect,
	status string,
	completionRequested bool,
) (int64, error) {
	if status != "completed" && status != "passed" {
		return 0, errors.New("invalid script event terminal status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var characterID, revision int64
	err = tx.QueryRowContext(ctx, `
		SELECT run.character_id,state.revision
		FROM script_event_runs run
		JOIN character_script_state state ON state.character_id=run.character_id
		WHERE run.id=$1 AND run.lease_token=$2 AND run.status='active'
		  AND run.lease_expires_at > now() AND run.state_revision=$3
		FOR UPDATE OF run,state`, runID, token, expectedRevision).Scan(&characterID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrScriptEventEnded
	}
	if err != nil {
		return 0, err
	}
	if revision != expectedRevision {
		return 0, ErrRevisionConflict
	}
	lastSequence := 0
	for _, effect := range effects {
		if effect.Sequence <= lastSequence {
			return 0, errors.New("script effects must have strictly increasing sequence numbers")
		}
		lastSequence = effect.Sequence
		arguments, _ := json.Marshal(effect.Arguments)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO script_event_effects (run_id,sequence,command_name,arguments)
			VALUES ($1,$2,$3,$4)`, runID, effect.Sequence, effect.Name, arguments); err != nil {
			return 0, err
		}
		if err := applyScriptEffect(ctx, tx, characterID, runID, effect); err != nil {
			return 0, fmt.Errorf("apply %s at sequence %d: %w", effect.Name, effect.Sequence, err)
		}
	}
	newRevision := revision
	if len(effects) > 0 {
		newRevision++
		if _, err := tx.ExecContext(ctx, `
			UPDATE character_script_state SET revision=$2,updated_at=now() WHERE character_id=$1`,
			characterID, newRevision); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE script_event_runs
		SET status=$2,completion_requested=$3,finished_at=now(),updated_at=now()
		WHERE id=$1`, runID, status, completionRequested); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newRevision, nil
}

func applyScriptEffect(ctx context.Context, tx *sql.Tx, characterID, runID int64, effect ScriptEffect) error {
	argument := func(index int, expected string) (string, error) {
		if index >= len(effect.Arguments) || !effect.Arguments[index].IsStatic ||
			effect.Arguments[index].Value == nil || effect.Arguments[index].Type != expected {
			return "", fmt.Errorf("argument %d must be a static %s", index+1, expected)
		}
		return *effect.Arguments[index].Value, nil
	}
	switch effect.Name {
	case "set_flag", "clear_flag":
		if len(effect.Arguments) != 1 {
			return errors.New("expected one argument")
		}
		key, err := argument(0, "string")
		if err != nil {
			return err
		}
		if effect.Name == "clear_flag" {
			_, err = tx.ExecContext(ctx, `DELETE FROM character_story_flags WHERE character_id=$1 AND key=$2`, characterID, key)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO character_story_flags (character_id,key,value) VALUES ($1,$2,true)
				ON CONFLICT (character_id,key) DO UPDATE SET value=true,updated_at=now()`, characterID, key)
		}
		return err
	case "set_progress", "increment_progress":
		if len(effect.Arguments) != 2 {
			return errors.New("expected two arguments")
		}
		key, err := argument(0, "string")
		if err != nil {
			return err
		}
		valueText, err := argument(1, "number")
		if err != nil {
			return err
		}
		value, err := strconv.ParseFloat(valueText, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("progress value must be a finite number")
		}
		if effect.Name == "increment_progress" {
			if value <= 0 {
				return errors.New("progress increment must be a positive finite number")
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO character_story_progress (character_id,key,value) VALUES ($1,$2,$3)
				ON CONFLICT (character_id,key) DO UPDATE SET value=character_story_progress.value+EXCLUDED.value,updated_at=now()`, characterID, key, value)
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO character_story_progress (character_id,key,value) VALUES ($1,$2,$3)
			ON CONFLICT (character_id,key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, characterID, key, value)
		return err
	case "grant_yen", "spend_yen":
		if len(effect.Arguments) != 2 {
			return errors.New("expected amount and reason")
		}
		amount, err := integerArgument(argument, 0)
		if err != nil || amount <= 0 {
			return errors.New("yen amount must be a positive integer")
		}
		reason, err := argument(1, "string")
		if err != nil || strings.TrimSpace(reason) == "" {
			return errors.New("yen reason is required")
		}
		delta := amount
		kind := "yen_credit"
		if effect.Name == "spend_yen" {
			delta, kind = -amount, "yen_spend"
		}
		var balance int64
		err = tx.QueryRowContext(ctx, `UPDATE characters SET yen=yen+$2,updated_at=now()
			WHERE id=$1 AND deleted_at IS NULL AND yen+$2 >= 0 RETURNING yen`, characterID, delta).Scan(&balance)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInsufficient
		}
		if err != nil {
			return err
		}
		return recordEconomyEvent(ctx, tx, characterID, effectEventKey(runID, effect.Sequence), kind, "", 0, delta, reason)
	case "give_item", "remove_item":
		if len(effect.Arguments) != 2 {
			return errors.New("expected item and quantity")
		}
		item, err := argument(0, "string")
		if err != nil {
			return err
		}
		quantity, err := integerArgument(argument, 1)
		if err != nil || quantity <= 0 || quantity > math.MaxInt32 {
			return errors.New("item quantity must be a positive integer")
		}
		return applyItemEffect(ctx, tx, characterID, runID, effect, item, int(quantity))
	default:
		return fmt.Errorf("command %q is not a durable script effect", effect.Name)
	}
}

func integerArgument(argument func(int, string) (string, error), index int) (int64, error) {
	text, err := argument(index, "number")
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
		value > float64(scriptcontent.YarnMaxSafeInteger) || value < -float64(scriptcontent.YarnMaxSafeInteger) {
		return 0, errors.New("value must be an exactly representable Yarn integer")
	}
	return int64(value), nil
}

func applyItemEffect(ctx context.Context, tx *sql.Tx, characterID, runID int64, effect ScriptEffect, item string, quantity int) error {
	if effect.Name == "give_item" {
		var newQuantity int
		err := tx.QueryRowContext(ctx, `INSERT INTO character_inventory (character_id,item_key,quantity)
			SELECT $1,key,$3 FROM item_definitions WHERE key=$2 AND $3 <= max_stack
			ON CONFLICT (character_id,item_key) DO UPDATE
			SET quantity=character_inventory.quantity+EXCLUDED.quantity,updated_at=now()
			WHERE character_inventory.quantity+EXCLUDED.quantity <=
				(SELECT max_stack FROM item_definitions WHERE key=EXCLUDED.item_key)
			RETURNING quantity`, characterID, item, quantity).Scan(&newQuantity)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInsufficient
		}
		if err != nil {
			return err
		}
		return recordEconomyEvent(ctx, tx, characterID, effectEventKey(runID, effect.Sequence), "item_grant", item, quantity, 0, "script event")
	}
	var current int
	err := tx.QueryRowContext(ctx, `SELECT quantity FROM character_inventory
		WHERE character_id=$1 AND item_key=$2 FOR UPDATE`, characterID, item).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) || current < quantity {
		return ErrInsufficient
	}
	if err != nil {
		return err
	}
	if current == quantity {
		_, err = tx.ExecContext(ctx, `DELETE FROM character_inventory WHERE character_id=$1 AND item_key=$2`, characterID, item)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE character_inventory SET quantity=quantity-$3,updated_at=now()
			WHERE character_id=$1 AND item_key=$2`, characterID, item, quantity)
	}
	if err != nil {
		return err
	}
	return recordEconomyEvent(ctx, tx, characterID, effectEventKey(runID, effect.Sequence), "item_consume", item, -quantity, 0, "script event")
}

func effectEventKey(runID int64, sequence int) string {
	return fmt.Sprintf("script:%d:%d", runID, sequence)
}

func newLeaseToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("create script event lease: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
