package realtime

import (
	"fmt"
	"strconv"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

var arcadeGameNames = map[string]string{
	"qte-0":   "Excite QTE",
	"qte-1":   "QTE Paddle Game",
	"darts-0": "Darts Seven (Board 1)",
	"darts-1": "Darts Seven (Board 2)",
}

func arcadeScoreText(score float64) string {
	return strconv.FormatFloat(score, 'f', -1, 64)
}

func (h *Hub) PublishArcadeHighScore(
	machineID string,
	score float64,
	playerName string,
	achievedAt time.Time,
) {
	gameName, ok := arcadeGameNames[machineID]
	if !ok {
		return
	}
	h.mu.RLock()
	arcadeRecipients := h.roomRecipientsLocked("arcade", "")
	allRecipients := h.allClientsLocked("")
	h.mu.RUnlock()
	h.sendMany(arcadeRecipients, protocol.ArcadeHighScoreEvent{
		Header:     protocol.NewHeader(protocol.TypeArcadeHighScore),
		MachineID:  machineID,
		Score:      score,
		PlayerName: playerName,
		AchievedAt: achievedAt.UnixMilli(),
	})
	h.sendMany(allRecipients, protocol.SystemMessage{
		Header: protocol.NewHeader(protocol.TypeSystemMessage),
		Text: fmt.Sprintf(
			"%s set a new high score of %s in %s!",
			playerName,
			arcadeScoreText(score),
			gameName,
		),
		SentAt: achievedAt.UnixMilli(),
	})
}
