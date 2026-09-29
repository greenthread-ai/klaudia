package agent

import (
	"testing"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestUserMessageWithImagesTextOnly(t *testing.T) {
	msg := userMessageWithImages("hello", nil)
	if len(msg.Content) != 1 {
		t.Fatalf("want 1 block, got %d", len(msg.Content))
	}
	if msg.Content[0].OfText == nil || msg.Content[0].OfText.Text != "hello" {
		t.Errorf("text block not preserved: %+v", msg.Content[0])
	}
}

func TestUserMessageWithImagesAttachesImageBlocks(t *testing.T) {
	imgs := []tools.ResultImage{{MediaType: "image/png", Base64: "abc123"}}
	msg := userMessageWithImages("look at this", imgs)
	if len(msg.Content) != 2 {
		t.Fatalf("want text + image = 2 blocks, got %d", len(msg.Content))
	}
	if msg.Content[0].OfText == nil || msg.Content[0].OfText.Text != "look at this" {
		t.Errorf("first block should be the text: %+v", msg.Content[0])
	}
	img := msg.Content[1].OfImage
	if img == nil || img.Source.OfBase64 == nil {
		t.Fatalf("second block should be a base64 image: %+v", msg.Content[1])
	}
	if got := img.Source.OfBase64.Data; got != "abc123" {
		t.Errorf("image data = %q, want abc123", got)
	}
	if got := string(img.Source.OfBase64.MediaType); got != "image/png" {
		t.Errorf("media type = %q, want image/png", got)
	}
}

func TestUserMessageWithImagesNoTextStillAttaches(t *testing.T) {
	imgs := []tools.ResultImage{{MediaType: "image/jpeg", Base64: "zzz"}}
	msg := userMessageWithImages("", imgs)
	if len(msg.Content) != 1 || msg.Content[0].OfImage == nil {
		t.Fatalf("want a single image block, got %+v", msg.Content)
	}
}
