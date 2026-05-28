package logic

import (
	"strings"
	"sync"

	"github.com/q191201771/lal/pkg/base"
)

type customizePubEntry struct {
	streamName  string
	publisherID string
	resourceID  string
	kick        func()
}

// CustomizePubRegistry tracks lalmax-managed customize pub sessions for kick/disconnect.
type CustomizePubRegistry struct {
	mu sync.RWMutex
	// publisherID -> entry
	byPublisher map[string]*customizePubEntry
	// resourceID -> publisherID
	byResource map[string]string
}

var defaultCustomizePubRegistry = NewCustomizePubRegistry()

func NewCustomizePubRegistry() *CustomizePubRegistry {
	return &CustomizePubRegistry{
		byPublisher: make(map[string]*customizePubEntry),
		byResource:  make(map[string]string),
	}
}

func GetCustomizePubRegistry() *CustomizePubRegistry {
	return defaultCustomizePubRegistry
}

func RegisterCustomizePub(streamName, publisherID, resourceID string, kick func()) {
	if publisherID == "" || kick == nil {
		return
	}

	entry := &customizePubEntry{
		streamName:  streamName,
		publisherID: publisherID,
		resourceID:  resourceID,
		kick:        kick,
	}

	r := GetCustomizePubRegistry()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byPublisher[publisherID] = entry
	if resourceID != "" {
		r.byResource[resourceID] = publisherID
	}
}

func UnregisterCustomizePub(publisherID, resourceID string) {
	r := GetCustomizePubRegistry()
	r.mu.Lock()
	defer r.mu.Unlock()

	if publisherID != "" {
		delete(r.byPublisher, publisherID)
	}
	if resourceID != "" {
		delete(r.byResource, resourceID)
	}
}

func KickCustomizePub(streamName, sessionID string) bool {
	if sessionID == "" {
		return false
	}

	r := GetCustomizePubRegistry()
	r.mu.RLock()
	entry := r.findEntryLocked(sessionID)
	r.mu.RUnlock()

	if entry == nil {
		return false
	}
	if streamName != "" && entry.streamName != streamName {
		return false
	}

	entry.kick()
	return true
}

func (r *CustomizePubRegistry) findEntryLocked(sessionID string) *customizePubEntry {
	if entry, ok := r.byPublisher[sessionID]; ok {
		return entry
	}
	if publisherID, ok := r.byResource[sessionID]; ok {
		return r.byPublisher[publisherID]
	}
	if strings.HasPrefix(sessionID, base.UkPreCustomizePubSessionContext) {
		return r.byPublisher[sessionID]
	}
	return nil
}
