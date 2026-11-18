package milanuncios_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func TestPrettyStripsANSI(t *testing.T) {
	// 1. AdItem Pretty
	ad := &milanuncios.AdItem{
		ID:          "12345",
		Title:       "\x1b[31mMalicious Red\x1b[0m Title",
		Description: "Normal \x1b[2Jdescription",
		Price:       100,
	}
	var buf bytes.Buffer
	ad.Pretty(&buf, false)
	outputStr := buf.String()
	if strings.Contains(outputStr, "\x1b") {
		t.Errorf("AdItem.Pretty contains ANSI escapes: %q", outputStr)
	}
	if !strings.Contains(outputStr, "Malicious Red Title") {
		t.Errorf("AdItem.Pretty missing stripped title: %q", outputStr)
	}

	// 2. ChatMessageReceivedEvent Pretty
	chatEv := milanuncios.ChatMessageReceivedEvent{
		EventType:      "chat.message_received",
		ConversationID: "conv-1",
		AdID:           "ad-1",
		SenderName:     "\x1b[31mHacker\x1b[0m",
		Text:           "Click here: \x1b]0;Evil\x07hello",
		SentAt:         "2026-11-18 20:00:00",
	}
	buf.Reset()
	chatEv.Pretty(&buf, false)
	outputStr = buf.String()
	if strings.Contains(outputStr, "\x1b") {
		t.Errorf("ChatMessageReceivedEvent.Pretty contains ANSI escapes: %q", outputStr)
	}
	if !strings.Contains(outputStr, "Hacker") || !strings.Contains(outputStr, "hello") {
		t.Errorf("ChatMessageReceivedEvent.Pretty corrupted: %q", outputStr)
	}
}
