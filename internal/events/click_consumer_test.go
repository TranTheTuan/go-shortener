package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeClick(t *testing.T) {
	b, _ := json.Marshal(ClickEvent{LinkID: 7, ClickedAt: time.Unix(0, 0).UTC(), Referrer: "r"})
	if click, ok := decodeClick(b); !ok || click.LinkID != 7 || click.Referrer != "r" {
		t.Errorf("valid decode = (%+v, %v), want linkID 7 ok", click, ok)
	}
	if _, ok := decodeClick([]byte("garbage")); ok {
		t.Error("poison payload should decode as ok=false")
	}
}
