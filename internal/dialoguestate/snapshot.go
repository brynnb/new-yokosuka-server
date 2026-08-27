package dialoguestate

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

const (
	Schema                         = "new-yokosuka-native-dialogue-snapshot-v1"
	StoryBank2ByteLength           = 128
	StoryBank3ByteLength           = 8
	StoryBank4ByteLength           = 32
	ProgressByteLength             = 325 * 12
	DynamicIdentityCount           = 24
	RandomHistoryCount             = 3
	MaxActorByteStates             = 512
	PersistentScriptBitsByteLength = 32
)

var freeRoamStoryBank2 = [StoryBank2ByteLength]byte{0xec}

var freeRoamStoryBank3 = [StoryBank3ByteLength]byte{
	0xb5, 0x66, 0x00, 0x00, 0xde, 0x03, 0x00, 0x00,
}

type StoryBanks struct {
	Bank2 []byte `json:"2"`
	Bank3 []byte `json:"3"`
	Bank4 []byte `json:"4"`
}

type StoryState struct {
	Banks StoryBanks `json:"banks"`
}

type ProgressIndices struct {
	DynamicCursor     int       `json:"dynamicCursor"`
	DynamicIdentities []*string `json:"dynamicIdentities"`
}

type RandomState struct {
	History []*int `json:"history"`
}

type GameState struct {
	ActorBytes map[string]uint8 `json:"actorBytes"`
	ScriptBits []byte           `json:"scriptBits"`
}

// Snapshot is the complete, bounded mutable state used by the recovered
// Dreamcast dialogue interpreters. Byte slices use Go's standard base64 JSON
// representation so the 3,900-byte progress table remains compact in transit.
type Snapshot struct {
	Schema          string          `json:"schema"`
	Revision        uint64          `json:"revision"`
	State           StoryState      `json:"state"`
	Progress        []byte          `json:"progress"`
	ProgressIndices ProgressIndices `json:"progressIndices"`
	Random          RandomState     `json:"random"`
	GameState       GameState       `json:"gameState"`
}

func Default() Snapshot {
	progress := make([]byte, ProgressByteLength)
	for index := 0; index < 325; index++ {
		binary.LittleEndian.PutUint32(
			progress[index*12+8:],
			math.Float32bits(9),
		)
	}
	return Snapshot{
		Schema: Schema,
		State: StoryState{Banks: StoryBanks{
			Bank2: append([]byte(nil), freeRoamStoryBank2[:]...),
			Bank3: append([]byte(nil), freeRoamStoryBank3[:]...),
			Bank4: make([]byte, StoryBank4ByteLength),
		}},
		Progress: progress,
		ProgressIndices: ProgressIndices{
			DynamicCursor:     DynamicIdentityCount - 1,
			DynamicIdentities: make([]*string, DynamicIdentityCount),
		},
		Random: RandomState{
			History: make([]*int, RandomHistoryCount),
		},
		GameState: GameState{
			ActorBytes: make(map[string]uint8),
			ScriptBits: make([]byte, PersistentScriptBitsByteLength),
		},
	}
}

func (snapshot Snapshot) Validate() error {
	if snapshot.Schema != Schema {
		return fmt.Errorf("unsupported dialogue snapshot schema %q", snapshot.Schema)
	}
	lengths := []struct {
		label string
		got   int
		want  int
	}{
		{"story bank 2", len(snapshot.State.Banks.Bank2), StoryBank2ByteLength},
		{"story bank 3", len(snapshot.State.Banks.Bank3), StoryBank3ByteLength},
		{"story bank 4", len(snapshot.State.Banks.Bank4), StoryBank4ByteLength},
		{"progress", len(snapshot.Progress), ProgressByteLength},
		{
			"persistent script bits",
			len(snapshot.GameState.ScriptBits),
			PersistentScriptBitsByteLength,
		},
		{
			"dynamic identity pool",
			len(snapshot.ProgressIndices.DynamicIdentities),
			DynamicIdentityCount,
		},
		{"random history", len(snapshot.Random.History), RandomHistoryCount},
	}
	for _, length := range lengths {
		if length.got != length.want {
			return fmt.Errorf(
				"%s has length %d; expected %d",
				length.label,
				length.got,
				length.want,
			)
		}
	}
	if snapshot.ProgressIndices.DynamicCursor < 0 ||
		snapshot.ProgressIndices.DynamicCursor >= DynamicIdentityCount {
		return errors.New("dynamic identity cursor is out of range")
	}
	for _, identity := range snapshot.ProgressIndices.DynamicIdentities {
		if identity == nil {
			continue
		}
		if utf8.RuneCountInString(*identity) != 4 || !utf8.ValidString(*identity) {
			return fmt.Errorf("invalid four-byte dialogue identity %q", *identity)
		}
		for _, value := range *identity {
			if value > 0xff {
				return fmt.Errorf("dialogue identity %q is not byte-oriented", *identity)
			}
		}
	}
	for _, value := range snapshot.Random.History {
		if value != nil && (*value < 0 || *value > 0xff) {
			return errors.New("dialogue random history value is out of range")
		}
	}
	if snapshot.GameState.ActorBytes == nil {
		return errors.New("native actor byte state must be an object")
	}
	if len(snapshot.GameState.ActorBytes) > MaxActorByteStates {
		return errors.New("native actor byte state exceeds its entry limit")
	}
	for identity := range snapshot.GameState.ActorBytes {
		if utf8.RuneCountInString(identity) != 4 || !utf8.ValidString(identity) {
			return fmt.Errorf("invalid native actor byte identity %q", identity)
		}
		for _, value := range identity {
			if value > 0xff {
				return fmt.Errorf(
					"native actor byte identity %q is not byte-oriented",
					identity,
				)
			}
		}
	}
	return nil
}
