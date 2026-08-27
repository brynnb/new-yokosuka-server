package realtime

import (
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

func TestDiscordChatUsesGlobalChatPersistenceBroadcastAndSink(t *testing.T) {
	rig := newTestRig(t, 10)
	saver := &recordingChatSaver{}
	rig.hub.SetChatMessageSaver(saver)
	conn, _ := rig.connect(t)
	bridged := make(chan protocol.ChatMessage, 1)
	rig.hub.SetPublicChatSink(func(senderName, text string) {
		bridged <- protocol.ChatMessage{Name: senderName, Text: text}
	})

	if err := rig.hub.BroadcastExternalChat("Tester[Discord]", " hello   Yokosuka "); err != nil {
		t.Fatal(err)
	}
	var event protocol.ChatEvent
	readJSON(t, conn, &event)
	if event.Message.Name != "Tester[Discord]" || event.Message.Text != "hello Yokosuka" || event.Message.WorldID != "discord" {
		t.Fatalf("unexpected external chat event: %#v", event)
	}
	if len(saver.messages) != 1 || saver.messages[0].PlayerName != "Tester[Discord]" || saver.messages[0].Text != "hello Yokosuka" {
		t.Fatalf("external chat was not persisted: %#v", saver.messages)
	}
	select {
	case message := <-bridged:
		if message.Name != "Tester[Discord]" || message.Text != "hello Yokosuka" {
			t.Fatalf("unexpected outbound bridge message: %#v", message)
		}
	default:
		t.Fatal("external chat did not reach the outbound bridge")
	}
}
