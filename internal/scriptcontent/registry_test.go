package scriptcontent

import "testing"

func TestCommandRegistryIsPinnedAndInternallyConsistent(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Version != YarnCommandSchemaVersion || len(registry.Entries) < 20 {
		t.Fatalf("unexpected registry: %#v", registry)
	}
	want := map[string]string{
		"flag_set":              "function",
		"game_date_on_or_after": "function",
		"start_camera":          "command",
		"play_player_motion":    "command",
		"play_sequence":         "command",
		"pass_trigger":          "command",
		"start_activity":        "command",
		"complete":              "command",
	}
	for _, entry := range registry.Entries {
		if kind, ok := want[entry.Name]; ok {
			if entry.Kind != kind {
				t.Fatalf("%s kind = %s, want %s", entry.Name, entry.Kind, kind)
			}
			delete(want, entry.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing representative entries: %#v", want)
	}
	for _, unsupported := range []string{
		"face_actor", "move_actor", "play_actor_motion", "play_sound",
		"play_music", "stop_music", "set_object_state", "attach_prop",
		"detach_prop", "transition_room", "run_script", "actor_state",
		"object_exists", "activity_result",
	} {
		for _, entry := range registry.Entries {
			if entry.Name == unsupported {
				t.Fatalf("unimplemented command %q remains authorable", unsupported)
			}
		}
	}
}
