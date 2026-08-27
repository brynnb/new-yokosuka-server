package scriptevent

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type WorldFacts struct {
	GameHour        *float64
	GameDate        *CalendarDate
	ActorPresence   map[string]bool
	ActorStates     map[string]map[string]float64
	ActorBounds     map[string]map[string]bool
	ObjectExistence map[string]bool
	ActivityResults map[string]string
}

type eventState struct {
	store.CharacterScriptState
	facts         WorldFacts
	randomInteger func(int64, int64) (int64, error)
	effects       []store.ScriptEffect
}

func (state *eventState) resolveQuery(event scriptruntime.Event) (scriptruntime.Value, error) {
	stringArgument := func(index int) (string, error) {
		if index >= len(event.Arguments) || event.Arguments[index].Value == nil ||
			event.Arguments[index].Type != "string" {
			return "", fmt.Errorf("%s argument %d is not a string", event.Name, index+1)
		}
		return *event.Arguments[index].Value, nil
	}
	switch event.Name {
	case "flag_set":
		key, err := stringArgument(0)
		return boolValue(state.Flags[key]), err
	case "progress_value":
		key, err := stringArgument(0)
		return numberValue(state.Progress[key]), err
	case "has_item":
		item, err := stringArgument(0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		quantity, err := eventInteger(event, 1)
		if err != nil || quantity <= 0 {
			return scriptruntime.Value{}, errors.New("has_item quantity must be a positive integer")
		}
		return boolValue(int64(state.Inventory[item]) >= quantity), nil
	case "yen":
		return numberValue(float64(state.Yen)), nil
	case "actor_present":
		actor, err := stringArgument(0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		value, known := state.facts.ActorPresence[actor]
		if !known {
			return scriptruntime.Value{}, fmt.Errorf("actor presence for %q was not supplied by the authoritative world", actor)
		}
		return boolValue(value), nil
	case "actor_state":
		actor, err := stringArgument(0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		key, err := stringArgument(1)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		value, known := state.facts.ActorStates[actor][key]
		if !known {
			return scriptruntime.Value{}, fmt.Errorf("actor state %q for %q was not supplied by the authoritative world", key, actor)
		}
		return numberValue(value), nil
	case "in_scene":
		scene, err := stringArgument(0)
		return boolValue(scene == state.Scene), err
	case "game_hour":
		if state.facts.GameHour == nil {
			return scriptruntime.Value{}, errors.New("game hour was not supplied by the authoritative world")
		}
		return numberValue(*state.facts.GameHour), nil
	case "game_date_on_or_after":
		month, err := eventInteger(event, 0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		day, err := eventInteger(event, 1)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		if err := validateMonthDay(month, day); err != nil {
			return scriptruntime.Value{}, err
		}
		if state.facts.GameDate == nil {
			return scriptruntime.Value{}, errors.New("game date was not supplied by the authoritative world")
		}
		if err := state.facts.GameDate.Validate(); err != nil {
			return scriptruntime.Value{}, err
		}
		current := state.facts.GameDate.Month*100 + state.facts.GameDate.Day
		requested := int(month)*100 + int(day)
		return boolValue(current >= requested), nil
	case "random_integer":
		minimum, err := eventInteger(event, 0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		maximum, err := eventInteger(event, 1)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		if minimum > maximum {
			return scriptruntime.Value{}, errors.New("random_integer minimum must not exceed maximum")
		}
		if state.randomInteger == nil {
			return scriptruntime.Value{}, errors.New("authoritative random source is unavailable")
		}
		value, err := state.randomInteger(minimum, maximum)
		if err != nil {
			return scriptruntime.Value{}, fmt.Errorf("select authoritative random integer: %w", err)
		}
		if value < minimum || value > maximum {
			return scriptruntime.Value{}, errors.New("authoritative random source returned an out-of-range value")
		}
		return numberValue(float64(value)), nil
	case "actor_in_bounds":
		actor, err := stringArgument(0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		bounds, err := stringArgument(1)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		value, known := state.facts.ActorBounds[actor][bounds]
		if !known {
			return scriptruntime.Value{}, fmt.Errorf("bounds %q for actor %q were not supplied by the authoritative world", bounds, actor)
		}
		return boolValue(value), nil
	case "object_exists":
		object, err := stringArgument(0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		value, known := state.facts.ObjectExistence[object]
		if !known {
			return scriptruntime.Value{}, fmt.Errorf("object existence for %q was not supplied by the authoritative world", object)
		}
		return boolValue(value), nil
	case "activity_result":
		activity, err := stringArgument(0)
		if err != nil {
			return scriptruntime.Value{}, err
		}
		value, known := state.facts.ActivityResults[activity]
		if !known {
			return scriptruntime.Value{}, fmt.Errorf("activity result for %q is not available", activity)
		}
		return scriptruntime.Value{Type: "string", Value: value}, nil
	default:
		return scriptruntime.Value{}, fmt.Errorf("query %q has no authoritative resolver", event.Name)
	}
}

func (state *eventState) stage(event scriptruntime.Event) error {
	effect := store.ScriptEffect{Sequence: event.Sequence, Name: event.Name, Arguments: event.Arguments}
	stringArgument := func(index int) (string, error) {
		if index >= len(event.Arguments) || event.Arguments[index].Value == nil || event.Arguments[index].Type != "string" {
			return "", fmt.Errorf("%s argument %d is not a string", event.Name, index+1)
		}
		return *event.Arguments[index].Value, nil
	}
	switch event.Name {
	case "set_flag", "clear_flag":
		key, err := stringArgument(0)
		if err != nil {
			return err
		}
		if event.Name == "set_flag" {
			state.Flags[key] = true
		} else {
			delete(state.Flags, key)
		}
	case "set_progress", "increment_progress":
		key, err := stringArgument(0)
		if err != nil {
			return err
		}
		value, err := eventNumber(event, 1)
		if err != nil {
			return err
		}
		if event.Name == "increment_progress" {
			if value <= 0 {
				return errors.New("progress increment must be a positive finite number")
			}
			value += state.Progress[key]
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return errors.New("progress value would not be finite")
			}
		}
		state.Progress[key] = value
	case "grant_yen", "spend_yen":
		amount, err := eventInteger(event, 0)
		if err != nil || amount <= 0 {
			return errors.New("yen amount must be a positive integer")
		}
		if event.Name == "spend_yen" {
			if state.Yen < amount {
				return store.ErrInsufficient
			}
			state.Yen -= amount
		} else {
			if amount > math.MaxInt64-state.Yen {
				return errors.New("yen balance would overflow")
			}
			state.Yen += amount
		}
	case "give_item", "remove_item":
		item, err := stringArgument(0)
		if err != nil {
			return err
		}
		quantity, err := eventInteger(event, 1)
		if err != nil || quantity <= 0 || quantity > math.MaxInt32 {
			return errors.New("item quantity must be a positive integer")
		}
		if event.Name == "remove_item" {
			if int64(state.Inventory[item]) < quantity {
				return store.ErrInsufficient
			}
			state.Inventory[item] -= int(quantity)
		} else {
			state.Inventory[item] += int(quantity)
		}
	default:
		return fmt.Errorf("command %q is not a staged durable effect", event.Name)
	}
	state.effects = append(state.effects, effect)
	return nil
}

func eventNumber(event scriptruntime.Event, index int) (float64, error) {
	if index >= len(event.Arguments) || event.Arguments[index].Value == nil || event.Arguments[index].Type != "number" {
		return 0, fmt.Errorf("%s argument %d is not a number", event.Name, index+1)
	}
	value, err := strconv.ParseFloat(*event.Arguments[index].Value, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s argument %d is not finite", event.Name, index+1)
	}
	return value, nil
}

func eventInteger(event scriptruntime.Event, index int) (int64, error) {
	value, err := eventNumber(event, index)
	if err != nil || math.Trunc(value) != value ||
		value > float64(scriptcontent.YarnMaxSafeInteger) ||
		value < -float64(scriptcontent.YarnMaxSafeInteger) {
		return 0, fmt.Errorf("%s argument %d is not an exactly representable Yarn integer", event.Name, index+1)
	}
	return int64(value), nil
}

func boolValue(value bool) scriptruntime.Value {
	if value {
		return scriptruntime.Value{Type: "bool", Value: "true"}
	}
	return scriptruntime.Value{Type: "bool", Value: "false"}
}

func numberValue(value float64) scriptruntime.Value {
	return scriptruntime.Value{Type: "number", Value: strconv.FormatFloat(value, 'g', -1, 64)}
}
