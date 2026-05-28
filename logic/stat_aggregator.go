package logic

import "github.com/q191201771/lal/pkg/base"

// StatAggregator merges lal native group state with lalmax extension subscribers and publishers.
type StatAggregator struct {
	groupManager IGroupManager
}

type StatGroupView struct {
	Group   base.StatGroup
	ExtSubs []base.StatSub
	ExtPub  base.StatPub
}

func NewStatAggregator(groupManager IGroupManager) *StatAggregator {
	if groupManager == nil {
		groupManager = GetGroupManagerInstance()
	}
	return &StatAggregator{groupManager: groupManager}
}

func (a *StatAggregator) ExtSubscribers(key StreamKey) []base.StatSub {
	if a == nil || a.groupManager == nil || !key.Valid() {
		return nil
	}

	exist, extGroup := a.groupManager.GetGroup(key)
	if !exist || extGroup == nil {
		return nil
	}

	extSubs := extGroup.StatSubscribers()
	if len(extSubs) == 0 {
		return nil
	}

	out := make([]base.StatSub, len(extSubs))
	copy(out, extSubs)
	return out
}

func (a *StatAggregator) ExtPublisher(key StreamKey) base.StatPub {
	if a == nil || a.groupManager == nil || !key.Valid() {
		return base.StatPub{}
	}

	exist, extGroup := a.groupManager.GetGroup(key)
	if !exist || extGroup == nil {
		return base.StatPub{}
	}

	return extGroup.StatPublisher()
}

func (a *StatAggregator) BuildGroupView(group base.StatGroup) StatGroupView {
	key := NewStreamKey(group.AppName, group.StreamName)

	extSubs := a.ExtSubscribers(key)
	if len(extSubs) != 0 {
		group.StatSubs = append(group.StatSubs, extSubs...)
	} else {
		extSubs = make([]base.StatSub, 0)
	}

	extPub := a.ExtPublisher(key)
	if extPub.SessionId != "" && group.StatPub.SessionId == "" {
		group.StatPub = extPub
	}

	return StatGroupView{
		Group:   group,
		ExtSubs: extSubs,
		ExtPub:  extPub,
	}
}

func (a *StatAggregator) BuildGroupsView(groups []base.StatGroup) []StatGroupView {
	if len(groups) == 0 {
		return nil
	}

	out := make([]StatGroupView, len(groups))
	for i, group := range groups {
		out[i] = a.BuildGroupView(group)
	}
	return out
}

func (a *StatAggregator) MergeGroup(group base.StatGroup) base.StatGroup {
	return a.BuildGroupView(group).Group
}

func (a *StatAggregator) MergeGroups(groups []base.StatGroup) []base.StatGroup {
	if len(groups) == 0 {
		return groups
	}

	out := make([]base.StatGroup, len(groups))
	for i, group := range groups {
		out[i] = a.MergeGroup(group)
	}
	return out
}

func (a *StatAggregator) FindGroupView(groups []base.StatGroup, key StreamKey) *StatGroupView {
	if !key.Valid() {
		return nil
	}

	var matched *StatGroupView
	for i := range groups {
		group := groups[i]
		if group.StreamName != key.StreamName {
			continue
		}

		if key.AppName != "" {
			if group.AppName != key.AppName {
				continue
			}
			view := a.BuildGroupView(group)
			return &view
		}

		if matched != nil {
			return nil
		}
		view := a.BuildGroupView(group)
		matched = &view
	}

	return matched
}

func (a *StatAggregator) FindGroup(groups []base.StatGroup, key StreamKey) *base.StatGroup {
	view := a.FindGroupView(groups, key)
	if view == nil {
		return nil
	}
	return &view.Group
}
