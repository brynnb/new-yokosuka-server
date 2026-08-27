package contentfilter

import "testing"

func TestModerationPoliciesArePopulated(t *testing.T) {
	if len(disallowedNameFragments) < 20 {
		t.Fatalf("expected meaningful name moderation coverage, got %d filters",
			len(disallowedNameFragments))
	}
	if len(disallowedChatWords) < 90 {
		t.Fatalf("expected expanded chat moderation coverage, got %d filters",
			len(disallowedChatWords))
	}
}

func TestNameFilterUsesNormalizedSubstrings(t *testing.T) {
	if NameAllowed("FriendlyAssholeName") {
		t.Fatal("expected a disallowed substring to reject the name")
	}
	if NameAllowed("ModernTrAnNyName") {
		t.Fatal("expected a case-insensitive disallowed substring to reject the name")
	}
	if NameAllowed("F.u.c.k.face") {
		t.Fatal("expected punctuation-obfuscated profanity to reject the name")
	}
	if !NameAllowed("Classical") {
		t.Fatal("expected an innocent substring to stay allowed")
	}
}

func TestUnrelatedTermsAreNotReserved(t *testing.T) {
	for _, name := range []string{"Level", "Player", "Sony", "Speed"} {
		if !NameAllowed(name) {
			t.Errorf("expected unrelated name %q to remain available", name)
		}
	}
}

func TestServiceNamesAreReserved(t *testing.T) {
	for _, name := range []string{
		"Admin", "G-M", "moderator", "New Y0kosuka", "server", "support",
	} {
		if NameAllowed(name) {
			t.Errorf("expected service-like name %q to be reserved", name)
		}
	}
}

func TestMajorShenmueCharacterNamesAreReserved(t *testing.T) {
	blocked := []string{
		"Ryo",
		"R-y-o",
		"Ryo Hazuki",
		"Hazuki Ryo",
		"RyoHazuki",
		"Ine",
		"Ine-san",
		"Inesan",
		"Fuku-san",
		"Fukusan",
		"Nozomi",
		"Iwao",
		"Guizhang",
		"Chai",
		"Lan Di",
		"LanDi",
		"Master Chen",
		"Shenhua",
		"Ren",
		"Joy",
		"Xiuying",
		"Fangmei",
		"Wong",
		"Tom",
		"Mark",
		"Goro",
		"Terry",
		"Jimmy",
		"Charlie",
	}
	for _, name := range blocked {
		if NameAllowed(name) {
			t.Errorf("expected Shenmue character name %q to be reserved", name)
		}
	}
}

func TestShenmueSurnamesAndUnrelatedNamesRemainAvailable(t *testing.T) {
	allowed := []string{
		"Hazuki",
		"Chen",
		"Harasaki",
		"Fukuhara",
		"Hayata",
		"Kimberly",
		"Mihashi",
		"Johnson",
		"Pine",
		"Enjoy",
		"Tomato",
		"Trenton",
	}
	for _, name := range allowed {
		if !NameAllowed(name) {
			t.Errorf("expected non-reserved name %q to remain available", name)
		}
	}
}

func TestChatFilterCensorsWholeWordsAndObfuscation(t *testing.T) {
	if got := CensorChat("what the fuck"); got != "what the ****" {
		t.Fatalf("common profanity was not censored: %q", got)
	}
	if got := CensorChat("f.u.c.k this bullshit"); got != "******* this ********" {
		t.Fatalf("obfuscated profanity was not censored: %q", got)
	}
	if got := CensorChat("hello f4g world"); got != "hello *** world" {
		t.Fatalf("unexpected leetspeak result: %q", got)
	}
	if got := CensorChat("porch-monkey"); got != "************" {
		t.Fatalf("unexpected punctuation result: %q", got)
	}
	if got := CensorChat("scunthorpe jigsaw"); got != "scunthorpe jigsaw" {
		t.Fatalf("whole-word boundaries produced a false positive: %q", got)
	}
	if got := CensorChat("ch¡nk"); got != "*****" {
		t.Fatalf("unicode obfuscation must remain valid UTF-8: %q", got)
	}
	if got := CensorChat("paki kyke redskin"); got != "**** **** *******" {
		t.Fatalf("major slur variants were not censored: %q", got)
	}
	if got := CensorChat("retarded spastic"); got != "******** *******" {
		t.Fatalf("ableist slurs were not censored: %q", got)
	}
}
