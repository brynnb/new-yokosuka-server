package game

import "testing"

func TestValidAvatarAcceptsMainAndGeneratedNPCs(t *testing.T) {
	for _, id := range []string{"ryo", "s1-hos-l", "s1-dg3-m"} {
		if !ValidAvatar(id) {
			t.Fatalf("expected avatar %q to be valid", id)
		}
	}
	if ValidAvatar("not-a-real-avatar") {
		t.Fatal("unexpected unknown avatar accepted")
	}
}
