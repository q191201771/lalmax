package logic

import (
	"sync"
	"time"

	"github.com/q191201771/lalmax/fmp4/hls"
	"github.com/q191201771/naza/pkg/nazalog"
)

type IGroupManager interface {
	GetOrCreateGroup(key StreamKey, uniqueKey string, hlssvr *hls.HlsServer, gopNum, singleGopMaxFrameNum int) (*Group, bool)
	RemoveGroup(key StreamKey)
	RemoveGroupIfMatch(key StreamKey, group *Group)
	GetGroup(key StreamKey) (bool, *Group)
	Iterate(onIterateGroup func(key StreamKey, group *Group) bool)
	Len() int
}

type ComplexGroupManager struct {
	mutex sync.RWMutex

	onlyStreamNameGroups    map[string]*Group
	appNameStreamNameGroups map[string]map[string]*Group
}

// 同时支持新路径 app/stream 和旧路径 stream 的查找方式。
func NewComplexGroupManager() *ComplexGroupManager {
	return &ComplexGroupManager{
		onlyStreamNameGroups:    make(map[string]*Group),
		appNameStreamNameGroups: make(map[string]map[string]*Group),
	}
}

var (
	defaultGroupManager *ComplexGroupManager
	groupManagerOnce    sync.Once
)

func GetGroupManagerInstance() *ComplexGroupManager {
	groupManagerOnce.Do(func() {
		defaultGroupManager = NewComplexGroupManager()
	})
	return defaultGroupManager
}

func (m *ComplexGroupManager) GetOrCreateGroup(key StreamKey, uniqueKey string, hlssvr *hls.HlsServer, gopNum, singleGopMaxFrameNum int) (*Group, bool) {
	if m == nil || !key.Valid() {
		return nil, false
	}

	for {
		m.mutex.Lock()
		ok, existing := m.getGroupLocked(key)
		if !ok {
			break
		}
		if !existing.closed.Load() {
			m.mutex.Unlock()
			return existing, false
		}
		m.mutex.Unlock()

		// 等旧 group 完成 HLS 清理后再发布替换 group，
		// 否则旧 group 的 OnStop 可能删掉新的 HLS session。
		existing.waitLifecycleIdle()

		m.mutex.Lock()
		ok, current := m.getGroupLocked(key)
		if !ok {
			break
		}
		if current == existing {
			break
		}
		if !current.closed.Load() {
			m.mutex.Unlock()
			return current, false
		}
		m.mutex.Unlock()
	}

	group := newGroup(m, uniqueKey, key, hlssvr, gopNum, singleGopMaxFrameNum)
	group.initHlsSession()
	m.setGroupLocked(key, group)
	m.mutex.Unlock()
	return group, true
}

func (m *ComplexGroupManager) GetOrCreateGroupByStreamName(uniqueKey, streamName string, hlssvr *hls.HlsServer, gopNum, singleGopMaxFrameNum int) (*Group, bool) {
	return m.GetOrCreateGroup(StreamKeyFromStreamName(streamName), uniqueKey, hlssvr, gopNum, singleGopMaxFrameNum)
}

func (m *ComplexGroupManager) setGroup(key StreamKey, group *Group) {
	if m == nil || !key.Valid() || group == nil {
		return
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.setGroupLocked(key, group)
}

func (m *ComplexGroupManager) setGroupLocked(key StreamKey, group *Group) {
	nazalog.Info("SetGroup, streamKey:", key.String())

	group.manager = m
	if key.AppName == "" {
		m.onlyStreamNameGroups[key.StreamName] = group
		return
	}

	groups, ok := m.appNameStreamNameGroups[key.AppName]
	if !ok {
		groups = make(map[string]*Group)
		m.appNameStreamNameGroups[key.AppName] = groups
	}
	groups[key.StreamName] = group
}

func (m *ComplexGroupManager) setGroupByStreamName(streamName string, group *Group) {
	m.setGroup(StreamKeyFromStreamName(streamName), group)
}

func (m *ComplexGroupManager) RemoveGroup(key StreamKey) {
	m.removeGroup(key, nil, false)
}

// 避免旧流晚到的 OnStop 或遍历删除误删同 key 的新流。
func (m *ComplexGroupManager) RemoveGroupIfMatch(key StreamKey, group *Group) {
	m.removeGroup(key, group, true)
}

func (m *ComplexGroupManager) removeGroup(key StreamKey, group *Group, shouldMatch bool) {
	if m == nil || !key.Valid() {
		return
	}

	nazalog.Info("RemoveGroup, streamKey:", key.String())

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if key.AppName == "" {
		if shouldMatch && m.onlyStreamNameGroups[key.StreamName] != group {
			return
		}
		delete(m.onlyStreamNameGroups, key.StreamName)
		return
	}

	deleted := false
	if groups, ok := m.appNameStreamNameGroups[key.AppName]; ok {
		if current, ok := groups[key.StreamName]; ok {
			if shouldMatch && current != group {
				return
			}
			delete(groups, key.StreamName)
			deleted = true
		}
		if len(groups) == 0 {
			delete(m.appNameStreamNameGroups, key.AppName)
		}
	}

	if !deleted {
		if shouldMatch && m.onlyStreamNameGroups[key.StreamName] != group {
			return
		}
		delete(m.onlyStreamNameGroups, key.StreamName)
	}
}

func (m *ComplexGroupManager) RemoveGroupByStreamName(streamName string) {
	m.RemoveGroup(StreamKeyFromStreamName(streamName))
}

func (m *ComplexGroupManager) GetGroup(key StreamKey) (bool, *Group) {
	if m == nil || !key.Valid() {
		return false, nil
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.getGroupLocked(key)
}

func (m *ComplexGroupManager) getGroupLocked(key StreamKey) (bool, *Group) {
	if key.AppName == "" {
		if group, ok := m.onlyStreamNameGroups[key.StreamName]; ok {
			return true, group
		}
		return m.getGroupByOnlyStreamNameLocked(key.StreamName)
	}

	if groups, ok := m.appNameStreamNameGroups[key.AppName]; ok {
		if group, ok := groups[key.StreamName]; ok {
			return true, group
		}
	}

	if group, ok := m.onlyStreamNameGroups[key.StreamName]; ok {
		return true, group
	}

	return false, nil
}

func (m *ComplexGroupManager) GetGroupByStreamName(streamName string) (bool, *Group) {
	return m.GetGroup(StreamKeyFromStreamName(streamName))
}

// WaitGroup 等待流就绪，轮询 interval 间隔，总超时 timeout
// 为什么：GB28181 设备推流有延迟，播放端先于推流端到达，需短暂等待
func (m *ComplexGroupManager) WaitGroup(key StreamKey, interval, timeout time.Duration) (bool, *Group) {
	deadline := time.Now().Add(timeout)
	for {
		if ok, g := m.GetGroup(key); ok {
			return true, g
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(interval)
	}
}

// streamName 单独查找只在匹配唯一 appName 时成功，避免跨 app 串流。
func (m *ComplexGroupManager) getGroupByOnlyStreamNameLocked(streamName string) (bool, *Group) {
	var found *Group
	matchCount := 0
	for _, groups := range m.appNameStreamNameGroups {
		if group, ok := groups[streamName]; ok {
			found = group
			matchCount++
			if matchCount > 1 {
				nazalog.Warn("streamName matched multiple appName groups, streamName:", streamName)
				return false, nil
			}
		}
	}
	return matchCount == 1, found
}

func (m *ComplexGroupManager) Iterate(onIterateGroup func(key StreamKey, group *Group) bool) {
	if m == nil || onIterateGroup == nil {
		return
	}

	type entry struct {
		key   StreamKey
		group *Group
	}
	entries := make([]entry, 0, m.Len())

	m.mutex.RLock()
	for streamName, group := range m.onlyStreamNameGroups {
		entries = append(entries, entry{key: StreamKeyFromStreamName(streamName), group: group})
	}
	for appName, groups := range m.appNameStreamNameGroups {
		for streamName, group := range groups {
			entries = append(entries, entry{key: NewStreamKey(appName, streamName), group: group})
		}
	}
	m.mutex.RUnlock()

	for _, item := range entries {
		if !onIterateGroup(item.key, item.group) {
			m.RemoveGroupIfMatch(item.key, item.group)
		}
	}
}

func (m *ComplexGroupManager) Len() int {
	if m == nil {
		return 0
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	count := len(m.onlyStreamNameGroups)
	for _, groups := range m.appNameStreamNameGroups {
		count += len(groups)
	}
	return count
}
