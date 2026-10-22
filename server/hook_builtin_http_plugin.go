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
	cfg := p.hub.configSnapshot()
	if !cfg.Enable {
		return nil
	}

	switch event.Event {
	case HookEventServerStart:
		if cfg.OnServerStart != "" {
			p.hub.asyncPostEvent(cfg.OnServerStart, event)
		}
		if cfg.ZlmOnServerStarted != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnServerStarted, event)
		}
	case HookEventUpdate:
		if cfg.OnUpdate != "" {
			p.hub.asyncPostEvent(cfg.OnUpdate, event)
		}
	case HookEventGroupStart:
		if cfg.OnGroupStart != "" {
			p.hub.asyncPostEvent(cfg.OnGroupStart, event)
		}
	case HookEventGroupStop:
		if cfg.OnGroupStop != "" {
			p.hub.asyncPostEvent(cfg.OnGroupStop, event)
		}
	case HookEventStreamActive:
		if cfg.OnStreamActive != "" {
			p.hub.asyncPostEvent(cfg.OnStreamActive, event)
		}
	case HookEventPubStart:
		if cfg.OnPubStart != "" {
			p.hub.asyncPostEvent(cfg.OnPubStart, event)
		}
	case HookEventPubStop:
		if cfg.OnPubStop != "" {
			p.hub.asyncPostEvent(cfg.OnPubStop, event)
		}
	case HookEventSubStart:
		if cfg.OnSubStart != "" {
			p.hub.asyncPostEvent(cfg.OnSubStart, event)
		}
	case HookEventSubStop:
		if cfg.OnSubStop != "" {
			p.hub.asyncPostEvent(cfg.OnSubStop, event)
		}
	case HookEventRelayPullStart:
		if cfg.OnRelayPullStart != "" {
			p.hub.asyncPostEvent(cfg.OnRelayPullStart, event)
		}
	case HookEventRelayPullStop:
		if cfg.OnRelayPullStop != "" {
			p.hub.asyncPostEvent(cfg.OnRelayPullStop, event)
		}
	case HookEventRtmpConnect:
		if cfg.OnRtmpConnect != "" {
			p.hub.asyncPostEvent(cfg.OnRtmpConnect, event)
		}
	case HookEventHlsMakeTs:
		if cfg.OnHlsMakeTs != "" {
			p.hub.asyncPostEvent(cfg.OnHlsMakeTs, event)
		}
	case HookEventStreamChanged:
		if cfg.ZlmOnStreamChanged != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnStreamChanged, event)
		}
	case HookEventServerKeepalive:
		if cfg.ZlmOnServerKeepalive != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnServerKeepalive, event)
		}
	case HookEventStreamNoneReader:
		if cfg.ZlmOnStreamNoneReader != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnStreamNoneReader, event)
		}
	case HookEventRtpServerTimeout:
		if cfg.ZlmOnRtpServerTimeout != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnRtpServerTimeout, event)
		}
	case HookEventRecordMp4:
		if cfg.ZlmOnRecordMp4 != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnRecordMp4, event)
		}
	case HookEventPublish:
		if cfg.ZlmOnPublish != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnPublish, event)
		}
	case HookEventPlay:
		if cfg.ZlmOnPlay != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnPlay, event)
		}
	case HookEventStreamNotFound:
		if cfg.ZlmOnStreamNotFound != "" {
			p.hub.asyncPostEvent(cfg.ZlmOnStreamNotFound, event)
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
