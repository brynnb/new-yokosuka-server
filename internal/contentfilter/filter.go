package contentfilter

import (
	"strings"
	"unicode"
)

// Character names use a compact project-owned policy. Only terms that are
// unambiguously abusive as substrings belong here; chat uses the broader
// whole-word policy below. compactName makes this list case-, punctuation-,
// and common-leetspeak-insensitive without importing an external blocklist.
var disallowedNameFragments = compactAll([]string{
	"asshole",
	"bastard",
	"bitch",
	"bullshit",
	"cunt",
	"fuck",
	"motherfucker",
	"pussy",
	"slut",
	"whore",
	"nigger",
	"nigga",
	"niglet",
	"porchmonkey",
	"jigaboo",
	"jiggaboo",
	"tarbaby",
	"pickaninny",
	"chingchong",
	"zipperhead",
	"wetback",
	"beaner",
	"raghead",
	"sandnigger",
	"sandnigga",
	"towelhead",
	"faggot",
	"tranny",
	"tranney",
	"mongoloid",
	"retard",
})

var reservedServiceNames = map[string]struct{}{
	"admin":            {},
	"administrator":    {},
	"gamemaster":       {},
	"gm":               {},
	"moderator":        {},
	"newyokosuka":      {},
	"newyokosukastaff": {},
	"server":           {},
	"staff":            {},
	"support":          {},
	"system":           {},
}

// Major character names are reserved separately from the moderation filter.
// Exact normalized matching prevents impersonation without rejecting surnames
// or innocent names containing a short character name.
var reservedShenmueGivenNames = map[string]struct{}{
	"chai":     {},
	"charlie":  {},
	"fangmei":  {},
	"fuku":     {},
	"goro":     {},
	"guizhang": {},
	"ine":      {},
	"iwao":     {},
	"jimmy":    {},
	"joy":      {},
	"mark":     {},
	"masayuki": {},
	"nozomi":   {},
	"ren":      {},
	"ryo":      {},
	"shenhua":  {},
	"terry":    {},
	"tom":      {},
	"wong":     {},
	"xiuying":  {},
}

var reservedShenmueCompactNames = map[string]struct{}{
	"fangmeixun":       {},
	"goromihashi":      {},
	"guizhangchen":     {},
	"harasakinozomi":   {},
	"hayataine":        {},
	"hazukiiwao":       {},
	"hazukiryo":        {},
	"hongxiuying":      {},
	"inehayata":        {},
	"iwaohazuki":       {},
	"jimmyyan":         {},
	"landi":            {},
	"lingshenhua":      {},
	"longsunzhao":      {},
	"markkimberly":     {},
	"masterchen":       {},
	"masayukifukuhara": {},
	"nozomiharasaki":   {},
	"renwuying":        {},
	"ryohazuki":        {},
	"shenhualing":      {},
	"terryryan":        {},
	"tomjohnson":       {},
	"wuyingren":        {},
	"xiuyinghong":      {},
	"xunfangmei":       {},
	"yaowen":           {},
	"yaowenchen":       {},
	"zhaolongsun":      {},
}

var reservedNameHonorifics = []string{"san", "sama", "chan", "kun"}

var allowedShenmueSurnames = map[string]struct{}{
	"chen":     {},
	"fukuhara": {},
	"harasaki": {},
	"hayata":   {},
	"hazuki":   {},
	"hong":     {},
	"johnson":  {},
	"kimberly": {},
	"ling":     {},
	"mihashi":  {},
	"ryan":     {},
	"xun":      {},
	"yan":      {},
	"zhao":     {},
}

var disallowedChatWords = normalizeAll([]string{
	// Common severe profanity. Inflections are explicit because chat matching
	// uses whole words to avoid censoring innocent substrings.
	"fuck",
	"fucks",
	"fucked",
	"fucking",
	"fucker",
	"fuckers",
	"motherfucker",
	"motherfuckers",
	"motherfucking",
	"shit",
	"shits",
	"shitty",
	"bullshit",
	"bullshitting",
	"bitch",
	"bitches",
	"bitching",
	"son of a bitch",
	"cunt",
	"cunts",
	"asshole",
	"assholes",
	"bastard",
	"bastards",
	"dick",
	"dicks",
	"cock",
	"cocks",
	"pussy",
	"pussies",
	"slut",
	"sluts",
	"whore",
	"whores",

	// Racial and ethnic slurs, including common spelling variants.
	"nigger",
	"nigga",
	"niggas",
	"niglet",
	"niglets",
	"coon",
	"porch monkey",
	"porchmonkey",
	"jigaboo",
	"jiggaboo",
	"jig",
	"spook",
	"tarbaby",
	"tar baby",
	"darky",
	"pickaninny",
	"sambo",
	"chink",
	"ching chong",
	"chingchong",
	"gook",
	"zipperhead",
	"spic",
	"wetback",
	"beaner",
	"kike",
	"kyke",
	"kykes",
	"heeb",
	"raghead",
	"sandnigger",
	"sandnigga",
	"towelhead",
	"paki",
	"pakis",
	"jap",
	"japs",
	"wop",
	"wops",
	"redskin",
	"redskins",

	// Homophobic, transphobic, and ableist slurs.
	"faggot",
	"faggots",
	"fag",
	"fags",
	"homo",
	"homos",
	"lesbo",
	"lesbos",
	"tranny",
	"tranney",
	"trannies",
	"trannys",
	"dyke",
	"retard",
	"retards",
	"retarded",
	"mongoloid",
	"mongoloids",
	"spastic",
	"spaz",
})

var leetMap = map[rune]rune{
	'0': 'o',
	'1': 'i',
	'3': 'e',
	'4': 'a',
	'5': 's',
	'7': 't',
	'8': 'b',
	'9': 'g',
	'@': 'a',
	'$': 's',
	'!': 'i',
	'¡': 'i',
	'+': 't',
	'(': 'c',
	'|': 'l',
}

func compactAll(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if compact := compactName(value); compact != "" {
			result = append(result, compact)
		}
	}
	return result
}

func normalizeAll(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizeText(value); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func normalizeText(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, current := range strings.ToLower(value) {
		if mapped, exists := leetMap[current]; exists {
			result.WriteRune(mapped)
		} else if unicode.IsLetter(current) || unicode.IsDigit(current) ||
			current == ' ' {
			result.WriteRune(current)
		}
	}
	return result.String()
}

func compactName(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, current := range strings.ToLower(value) {
		if mapped, exists := leetMap[current]; exists {
			current = mapped
		}
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			result.WriteRune(current)
		}
	}
	return result.String()
}

func reservedShenmueName(value string) bool {
	isReservedGivenName := func(candidate string) bool {
		if _, reserved := reservedShenmueGivenNames[candidate]; reserved {
			return true
		}
		for _, honorific := range reservedNameHonorifics {
			if strings.HasSuffix(candidate, honorific) {
				base := strings.TrimSuffix(candidate, honorific)
				if _, reserved := reservedShenmueGivenNames[base]; reserved {
					return true
				}
			}
		}
		return false
	}

	compact := compactName(value)
	if isReservedGivenName(compact) {
		return true
	}
	if _, reserved := reservedShenmueCompactNames[compact]; reserved {
		return true
	}
	for _, component := range strings.FieldsFunc(value, func(current rune) bool {
		return !unicode.IsLetter(current) && !unicode.IsDigit(current)
	}) {
		if isReservedGivenName(compactName(component)) {
			return true
		}
	}
	return false
}

// NameAllowed applies local moderation plus service and character impersonation
// protection to the normalized form a player can present to other clients.
func NameAllowed(name string) bool {
	lower := strings.ToLower(name)
	if _, surname := allowedShenmueSurnames[lower]; surname {
		return true
	}
	compact := compactName(name)
	if _, reserved := reservedServiceNames[compact]; reserved {
		return false
	}
	for _, fragment := range disallowedNameFragments {
		if strings.Contains(compact, fragment) {
			return false
		}
	}
	return !reservedShenmueName(name)
}

type normalizedRune struct {
	value         rune
	originalIndex int
}

// CensorChat performs whole-word, punctuation-insensitive and leetspeak-aware
// matching while keeping the returned string valid UTF-8.
func CensorChat(message string) string {
	original := []rune(message)
	normalized := make([]normalizedRune, 0, len(original))
	for index, current := range original {
		lower := unicode.ToLower(current)
		if mapped, exists := leetMap[lower]; exists {
			normalized = append(normalized, normalizedRune{
				value: mapped, originalIndex: index,
			})
		} else if unicode.IsLetter(lower) || unicode.IsDigit(lower) ||
			lower == ' ' {
			normalized = append(normalized, normalizedRune{
				value: lower, originalIndex: index,
			})
		}
	}

	for _, word := range disallowedChatWords {
		wordRunes := []rune(word)
		for start := 0; start+len(wordRunes) <= len(normalized); start++ {
			end := start + len(wordRunes)
			if start > 0 && isWordCharacter(normalized[start-1].value) {
				continue
			}
			if end < len(normalized) &&
				isWordCharacter(normalized[end].value) {
				continue
			}
			matched := true
			for offset, expected := range wordRunes {
				if normalized[start+offset].value != expected {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
			originalStart := normalized[start].originalIndex
			originalEnd := normalized[end-1].originalIndex + 1
			for index := originalStart; index < originalEnd; index++ {
				if original[index] != ' ' {
					original[index] = '*'
				}
			}
		}
	}
	return string(original)
}

func isWordCharacter(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value)
}
