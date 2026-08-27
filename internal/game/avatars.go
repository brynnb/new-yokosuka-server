package game

var validAvatars = map[string]struct{}{
	"ryo": {}, "fuku": {}, "ine": {}, "iwao": {}, "chai": {},
	"guizhang": {}, "masterChen": {}, "lanDi": {}, "jimmy": {},
	"terry": {}, "tony": {}, "smith": {}, "manInBlackA": {},
	"manInBlackB": {}, "youngRyo": {},
}

func ValidAvatar(id string) bool {
	if _, ok := validAvatars[id]; ok {
		return true
	}
	_, ok := validNPCAvatars[id]
	return ok
}
