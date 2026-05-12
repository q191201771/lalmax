package server

import "fmt"

type hookBuiltinHTTPPlugin struct {
	name string
	hub  *HttpNotify
}

func (p *hookBuiltinHTTPPlugin) Name() string {
	return p.name
}

func (p *hookBuiltinHTTPPlugin) OnHookEvent(event HookEvent) error {
	if p == nil || p.hub == nil {
		return nil
	}
	if !p.hub.cfg.Enable {
		return nil
	}

	switch event.Event {
	case HookEventServerStart:
		if p.hub.cfg.OnServerStart != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnServerStart, event)
		}
		if p.hub.cfg.ZlmOnServerStarted != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnServerStarted, event)
		}
	case HookEventUpdate:
		if p.hub.cfg.OnUpdate != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnUpdate, event)
		}
	case HookEventGroupStart:
		if p.hub.cfg.OnGroupStart != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnGroupStart, event)
		}
	case HookEventGroupStop:
		if p.hub.cfg.OnGroupStop != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnGroupStop, event)
		}
	case HookEventStreamActive:
		if p.hub.cfg.OnStreamActive != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnStreamActive, event)
		}
	case HookEventPubStart:
		if p.hub.cfg.OnPubStart != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnPubStart, event)
		}
	case HookEventPubStop:
		if p.hub.cfg.OnPubStop != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnPubStop, event)
		}
	case HookEventSubStart:
		if p.hub.cfg.OnSubStart != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnSubStart, event)
		}
	case HookEventSubStop:
		if p.hub.cfg.OnSubStop != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnSubStop, event)
		}
	case HookEventRelayPullStart:
		if p.hub.cfg.OnRelayPullStart != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnRelayPullStart, event)
		}
	case HookEventRelayPullStop:
		if p.hub.cfg.OnRelayPullStop != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnRelayPullStop, event)
		}
	case HookEventRtmpConnect:
		if p.hub.cfg.OnRtmpConnect != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnRtmpConnect, event)
		}
	case HookEventHlsMakeTs:
		if p.hub.cfg.OnHlsMakeTs != "" {
			p.hub.asyncPostEvent(p.hub.cfg.OnHlsMakeTs, event)
		}
	case HookEventStreamChanged:
		if p.hub.cfg.ZlmOnStreamChanged != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnStreamChanged, event)
		}
	case HookEventServerKeepalive:
		if p.hub.cfg.ZlmOnServerKeepalive != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnServerKeepalive, event)
		}
	case HookEventStreamNoneReader:
		if p.hub.cfg.ZlmOnStreamNoneReader != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnStreamNoneReader, event)
		}
	case HookEventRtpServerTimeout:
		if p.hub.cfg.ZlmOnRtpServerTimeout != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnRtpServerTimeout, event)
		}
	case HookEventRecordMp4:
		if p.hub.cfg.ZlmOnRecordMp4 != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnRecordMp4, event)
		}
	case HookEventPublish:
		if p.hub.cfg.ZlmOnPublish != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnPublish, event)
		}
	case HookEventPlay:
		if p.hub.cfg.ZlmOnPlay != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnPlay, event)
		}
	case HookEventStreamNotFound:
		if p.hub.cfg.ZlmOnStreamNotFound != "" {
			p.hub.asyncPostEvent(p.hub.cfg.ZlmOnStreamNotFound, event)
		}
	}

	return nil
}

func (h *HttpNotify) mustRegisterBuiltinHTTPPlugin() {
	if h == nil {
		return
	}

	_, err := h.RegisterPlugin(&hookBuiltinHTTPPlugin{
		name: "builtin-http-notify",
		hub:  h,
	}, HookPluginOptions{})
	if err != nil {
		panic(fmt.Sprintf("register builtin http hook plugin failed: %v", err))
	}
}
