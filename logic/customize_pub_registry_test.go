package logic

import "testing"

func TestCustomizePubRegistryKickByPublisherID(t *testing.T) {
	kicked := false
	RegisterCustomizePub("test110", "CUSTOMIZEPUB-test-1", "", func() { kicked = true })
	defer UnregisterCustomizePub("CUSTOMIZEPUB-test-1", "")

	if !KickCustomizePub("test110", "CUSTOMIZEPUB-test-1") {
		t.Fatal("expected kick success")
	}
	if !kicked {
		t.Fatal("expected kick callback invoked")
	}
	if KickCustomizePub("other", "CUSTOMIZEPUB-test-1") {
		t.Fatal("expected stream name mismatch to fail")
	}
}

func TestCustomizePubRegistryKickByResourceID(t *testing.T) {
	kicked := false
	RegisterCustomizePub("test110", "CUSTOMIZEPUB-test-2", "whip-resource-1", func() { kicked = true })
	defer UnregisterCustomizePub("CUSTOMIZEPUB-test-2", "whip-resource-1")

	if !KickCustomizePub("", "whip-resource-1") {
		t.Fatal("expected kick by resource id")
	}
	if !kicked {
		t.Fatal("expected kick callback invoked")
	}
}
