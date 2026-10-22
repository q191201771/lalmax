(() => {
  'use strict';

  const state = { payload: null, search: '', timer: null, loading: false };
  const playerState = { streamKey: '', urls: {}, active: '', mpegts: null, pc: null, requestID: 0, hasAudio: false, flvHasAudio: false };
  const playerLabels = { rtc: 'WebRTC', fmp4: 'fMP4', flv: 'FLV' };
  const $ = (id) => document.getElementById(id);
  const query = new URLSearchParams(location.search);
  const token = query.get('token') || '';
  const page = location.pathname.endsWith('/runtime') ? 'runtime' : location.pathname.endsWith('/stream') ? 'detail' : 'streams';

  function escapeHTML(value) {
    return String(value == null ? '' : value).replace(/[&<>'"]/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' })[ch]);
  }

  function withToken(path) {
    if (!token) return path;
    const separator = path.includes('?') ? '&' : '?';
    return path + separator + 'token=' + encodeURIComponent(token);
  }

  function formatBitrate(value) {
    const n = Number(value) || 0;
    if (n >= 1000) return (n / 1000).toFixed(n >= 10000 ? 0 : 1) + ' Mbps';
    return Math.round(n) + ' Kbps';
  }

  function formatBytes(value) {
    let n = Number(value) || 0;
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
    return (i === 0 ? n.toFixed(0) : n.toFixed(n >= 10 ? 1 : 2)) + ' ' + units[i];
  }

  function formatDuration(seconds) {
    let sec = Math.max(0, Number(seconds) || 0);
    const days = Math.floor(sec / 86400); sec %= 86400;
    const hours = Math.floor(sec / 3600); sec %= 3600;
    const mins = Math.floor(sec / 60);
    if (days) return `${days}天 ${hours}小时`;
    if (hours) return `${hours}小时 ${mins}分`;
    return `${mins}分 ${Math.floor(sec % 60)}秒`;
  }

  function formatTime(value) {
    if (!value) return '-';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function healthLabel(value) {
    return value === 'bad' ? '异常' : value === 'warning' ? '注意' : '正常';
  }

  function metric(label, value, hint) {
    return `<article class="metric-card"><span>${escapeHTML(label)}</span><strong>${escapeHTML(value)}</strong><em>${escapeHTML(hint || '')}</em></article>`;
  }

  function showToast(message) {
    const toast = $('toast');
    toast.textContent = message;
    toast.classList.add('show');
    clearTimeout(showToast.timer);
    showToast.timer = setTimeout(() => toast.classList.remove('show'), 1600);
  }

  function setPlayerStatus(message, kind) {
    const status = $('player-status');
    status.textContent = message;
    status.className = 'player-status' + (kind ? ' ' + kind : '');
  }

  function setPlayerPlaceholder(message, hidden) {
    const placeholder = $('player-placeholder');
    placeholder.textContent = message || '';
    placeholder.hidden = Boolean(hidden);
  }

  function clearVideoElement() {
    const video = $('preview-video');
    video.pause();
    if (video.srcObject) {
      video.srcObject.getTracks().forEach(track => track.stop());
      video.srcObject = null;
    }
    video.removeAttribute('src');
    video.load();
  }

  function cleanupPlayer(message = '已停止') {
    playerState.requestID++;
    if (playerState.mpegts) {
      try { playerState.mpegts.pause(); } catch (_) {}
      try { playerState.mpegts.unload(); } catch (_) {}
      try { playerState.mpegts.detachMediaElement(); } catch (_) {}
      try { playerState.mpegts.destroy(); } catch (_) {}
      playerState.mpegts = null;
    }
    if (playerState.pc) {
      try { playerState.pc.close(); } catch (_) {}
      playerState.pc = null;
    }
    clearVideoElement();
    playerState.active = '';
    document.querySelectorAll('[data-player-protocol]').forEach(button => button.classList.remove('active'));
    setPlayerStatus(message, '');
    setPlayerPlaceholder(message, false);
  }

  function waitForIceGathering(pc) {
    if (pc.iceGatheringState === 'complete') return Promise.resolve();
    return new Promise(resolve => {
      const finish = () => {
        pc.removeEventListener('icegatheringstatechange', check);
        clearTimeout(timer);
        resolve();
      };
      const check = () => { if (pc.iceGatheringState === 'complete') finish(); };
      const timer = setTimeout(finish, 3000);
      pc.addEventListener('icegatheringstatechange', check);
    });
  }

  async function startWHEP(url, requestID) {
    if (!window.RTCPeerConnection) throw new Error('浏览器不支持 WebRTC');
    const video = $('preview-video');
    const pc = new RTCPeerConnection();
    playerState.pc = pc;
    pc.ontrack = event => {
      if (requestID !== playerState.requestID) return;
      video.srcObject = event.streams[0] || new MediaStream([event.track]);
      setPlayerPlaceholder('', true);
      video.play().catch(() => setPlayerStatus('WebRTC 已连接，点击播放', 'playing'));
    };
    pc.onconnectionstatechange = () => {
      if (requestID !== playerState.requestID) return;
      if (pc.connectionState === 'connected') setPlayerStatus('WebRTC 播放中', 'playing');
      if (['failed', 'disconnected'].includes(pc.connectionState)) setPlayerStatus('WebRTC 连接异常', 'error');
    };
    pc.addTransceiver('video', { direction: 'recvonly' });
    if (playerState.hasAudio) pc.addTransceiver('audio', { direction: 'recvonly' });
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    await waitForIceGathering(pc);
    const response = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/sdp' },
      body: pc.localDescription.sdp
    });
    if (!response.ok) throw new Error(`WHEP ${response.status}: ${await response.text()}`);
    const answer = await response.text();
    if (requestID !== playerState.requestID) return;
    await pc.setRemoteDescription({ type: 'answer', sdp: answer });
  }

  function startFMP4(url) {
    const video = $('preview-video');
    video.src = url;
    video.load();
    setPlayerPlaceholder('', true);
    video.play().catch(() => setPlayerStatus('fMP4 已加载，点击播放', 'playing'));
  }

  function startFLV(url) {
    if (!window.mpegts || !window.mpegts.isSupported()) throw new Error('当前浏览器不支持 MSE FLV 播放');
    const video = $('preview-video');
    window.mpegts.LoggingControl.enableAll = false;
    const player = window.mpegts.createPlayer({
      type: 'flv',
      isLive: true,
      hasVideo: true,
      hasAudio: playerState.flvHasAudio,
      url
    }, {
      enableWorker: false,
      enableStashBuffer: false,
      lazyLoad: false,
      autoCleanupSourceBuffer: true,
      autoCleanupMaxBackwardDuration: 30,
      autoCleanupMinBackwardDuration: 15
    });
    playerState.mpegts = player;
    player.attachMediaElement(video);
    if (window.mpegts.Events && window.mpegts.Events.ERROR) {
      player.on(window.mpegts.Events.ERROR, (type, detail, info) => {
        const message = info && info.msg ? info.msg : detail || type;
        setPlayerStatus(`FLV 播放失败：${message}`, 'error');
      });
    }
    player.load();
    setPlayerPlaceholder('', true);
    player.play().catch(() => setPlayerStatus('FLV 已加载，点击播放', 'playing'));
  }

  async function startPlayer(protocol) {
    const url = playerState.urls[protocol];
    if (!url) return;
    cleanupPlayer('正在连接…');
    playerState.active = protocol;
    playerState.requestID++;
    const requestID = playerState.requestID;
    document.querySelectorAll('[data-player-protocol]').forEach(button => button.classList.toggle('active', button.dataset.playerProtocol === protocol));
    setPlayerStatus(`${playerLabels[protocol]} 正在连接…`, '');
    setPlayerPlaceholder(`${playerLabels[protocol]} 正在连接…`, false);
    try {
      if (protocol === 'rtc') await startWHEP(url, requestID);
      if (protocol === 'fmp4') startFMP4(url);
      if (protocol === 'flv') startFLV(url);
    } catch (error) {
      if (requestID !== playerState.requestID) return;
      if (playerState.pc) {
        try { playerState.pc.close(); } catch (_) {}
        playerState.pc = null;
      }
      if (playerState.mpegts) {
        try { playerState.mpegts.destroy(); } catch (_) {}
        playerState.mpegts = null;
      }
      setPlayerStatus(`${playerLabels[protocol]} 播放失败`, 'error');
      setPlayerPlaceholder(error.message || '播放失败', false);
    }
  }

  function renderPlayer(stream) {
    const urls = stream.urls || {};
    const available = [
      ['rtc', urls.rtc],
      ['fmp4', urls.fmp4],
      ['flv', urls.flv]
    ].filter(([, value]) => value);
    $('player-protocols').innerHTML = available.map(([protocol]) => `<button class="protocol-tab" type="button" data-player-protocol="${protocol}">${playerLabels[protocol]}</button>`).join('');
    if (!available.length) {
      cleanupPlayer('没有可用的浏览器播放协议');
      return;
    }
    const streamKey = `${stream.app_name}/${stream.stream_name}`;
    playerState.urls = Object.fromEntries(available);
    playerState.hasAudio = Boolean(stream.audio_codec);
    playerState.flvHasAudio = /^(AAC|MP3)/i.test(stream.audio_codec || '');
    if (playerState.streamKey !== streamKey) {
      cleanupPlayer('准备播放…');
      playerState.streamKey = streamKey;
      startPlayer(available[0][0]);
      return;
    }
    document.querySelectorAll('[data-player-protocol]').forEach(button => button.classList.toggle('active', button.dataset.playerProtocol === playerState.active));
  }

  function updateNavigation() {
    document.querySelectorAll('[data-console-link]').forEach(link => link.href = withToken(link.getAttribute('href')));
    document.querySelectorAll('[data-api-link]').forEach(link => link.href = withToken(link.getAttribute('href')));
    document.querySelectorAll('[data-nav]').forEach(link => link.classList.toggle('active', link.dataset.nav === (page === 'detail' ? 'streams' : page)));
    const titles = {
      streams: ['流监控', '实时查看活动流、码率、会话和播放地址。'],
      detail: ['流详情', '定位单路流的输入、输出、帧率和会话信息。'],
      runtime: ['运行状态', '查看 lalmax 版本、Go 运行时和诊断入口。']
    };
    $('page-title').textContent = titles[page][0];
    $('page-subtitle').textContent = titles[page][1];
    $('view-streams').hidden = page !== 'streams';
    $('view-detail').hidden = page !== 'detail';
    $('view-runtime').hidden = page !== 'runtime';
  }

  function renderFeatures(features) {
    const labels = { http_flv: 'HTTP-FLV', fmp4: 'HTTP-fMP4', hls: 'HLS', rtc: 'WebRTC', srt: 'SRT', rtmp: 'RTMP', rtsp: 'RTSP' };
    $('feature-badges').innerHTML = Object.keys(labels).filter(key => features[key]).map(key => `<span class="badge">${labels[key]}</span>`).join('');
  }

  function renderOverview(payload) {
    const totals = payload.totals;
    $('overview-cards').innerHTML = [
      metric('活动流', totals.streams, '当前流组数量'),
      metric('观看会话', totals.players, '所有下游订阅'),
      metric('总输入码率', formatBitrate(totals.input_bitrate_kbits), '上行实时码率'),
      metric('总输出码率', formatBitrate(totals.output_bitrate_kbits), '下行实时码率')
    ].join('');
    renderFeatures(payload.features || {});
    renderStreamList(payload.streams || []);
  }

  function streamMatches(stream, needle) {
    if (!needle) return true;
    return [stream.stream_name, stream.display_app, stream.video_codec, stream.audio_codec, stream.source.protocol, stream.source.remote_addr]
      .join(' ').toLowerCase().includes(needle);
  }

  function detailURL(stream) {
    const params = new URLSearchParams({ stream: stream.stream_name });
    if (stream.app_name) params.set('app', stream.app_name);
    if (token) params.set('token', token);
    return '/console/stream?' + params.toString();
  }

  function renderStreamList(streams) {
    const needle = state.search.trim().toLowerCase();
    const filtered = streams.filter(stream => streamMatches(stream, needle));
    $('stream-count').textContent = `显示 ${filtered.length} / ${streams.length} 路`;
    if (!filtered.length) {
      $('stream-list').innerHTML = `<tr><td colspan="8" class="empty">${streams.length ? '没有匹配的流' : '当前没有活动流'}</td></tr>`;
      return;
    }
    $('stream-list').innerHTML = filtered.map(stream => {
      const codecs = [stream.video_codec, stream.audio_codec].filter(Boolean).map(codec => `<span class="codec">${escapeHTML(codec)}</span>`).join('') || '-';
      const source = stream.source || {};
      const resolution = stream.video_width ? `${stream.video_width}×${stream.video_height}` : '';
      return `<tr data-detail="${escapeHTML(detailURL(stream))}">
        <td><span class="health ${escapeHTML(stream.health)}">${healthLabel(stream.health)}</span></td>
        <td><span class="stream-name">${escapeHTML(stream.stream_name)}</span><span class="stream-app">${escapeHTML(stream.display_app)}</span></td>
        <td>${codecs}<span class="stream-app">${escapeHTML(resolution)}</span></td>
        <td>${escapeHTML(source.protocol || '-')}<span class="stream-app">${escapeHTML(source.remote_addr || source.base_type || '')}</span></td>
        <td>${formatBitrate(stream.input_bitrate_kbits)}</td><td>${formatBitrate(stream.output_bitrate_kbits)}</td>
        <td>${stream.players}</td><td>${stream.fps && stream.fps.length ? Number(stream.video_fps || 0).toFixed(1) : '-'}</td>
      </tr>`;
    }).join('');
  }

  function findDetailStream(streams) {
    const name = query.get('stream') || '';
    const app = query.get('app') || '';
    return streams.find(stream => stream.stream_name === name && stream.app_name === app) || null;
  }

  function renderURLs(stream) {
    const labels = { rtc: 'WebRTC', fmp4: 'fMP4', hls: 'HLS', flv: 'FLV', rtsp: 'RTSP', rtmp: 'RTMP', srt: 'SRT' };
    const entries = Object.entries(stream.urls || {}).filter(([, value]) => value);
    if (!entries.length) return '<div class="empty">没有可用地址</div>';
    return entries.map(([key, value]) => {
      const openable = value.startsWith('/') || /^https?:/i.test(value);
      return `<div class="url-item"><span class="url-label">${labels[key] || key.toUpperCase()}</span><span class="url-value" title="${escapeHTML(value)}">${escapeHTML(value)}</span><span class="url-actions"><button class="button small" data-copy="${escapeHTML(value)}">复制</button>${openable ? `<a class="button small" href="${escapeHTML(withToken(value))}" target="_blank" rel="noopener">打开</a>` : ''}</span></div>`;
    }).join('');
  }

  function sessionCard(session) {
    const bitrate = session.role === 'sub' ? (session.write_bitrate_kbits || session.bitrate_kbits) : (session.read_bitrate_kbits || session.bitrate_kbits);
    return `<article class="session-card"><div><span class="session-role">${escapeHTML(session.role || '-')}</span><span class="session-sub">${escapeHTML(session.base_type || '')}</span></div><div><strong>${escapeHTML(session.protocol || '-')}</strong><span class="session-sub">${escapeHTML(session.session_id || '-')}</span></div><div><strong>${escapeHTML(session.remote_addr || '-')}</strong><span class="session-sub">${escapeHTML(session.start_time || '-')}</span></div><div><strong>${formatBitrate(bitrate)}</strong><span class="session-sub">实时码率</span></div><div><strong>${formatBytes(session.read_bytes_sum + session.wrote_bytes_sum)}</strong><span class="session-sub">累计流量</span></div></article>`;
  }

  function renderFPS(samples) {
    if (!samples || !samples.length) return '<div class="empty">暂无帧率样本</div>';
    const max = Math.max(1, ...samples.map(item => Number(item.v) || 0));
    return samples.map(item => {
      const value = Number(item.v) || 0;
      const height = Math.max(2, Math.round(value / max * 150));
      const time = new Date(Number(item.unix_sec) * 1000).toLocaleTimeString();
      return `<span class="fps-bar" style="--height:${height}px" data-tip="${escapeHTML(time)} · ${value} fps"></span>`;
    }).join('');
  }

  function renderDetail(payload) {
    const stream = findDetailStream(payload.streams || []);
    if (!stream) {
      cleanupPlayer('流不存在或已经停止');
      playerState.streamKey = '';
      $('detail-name').textContent = '流不存在或已经停止';
      $('detail-meta').textContent = '返回列表选择仍在活动的流。';
      $('detail-metrics').innerHTML = '';
      $('detail-urls').innerHTML = '<div class="empty">无数据</div>';
      $('fps-chart').innerHTML = '<div class="empty">无数据</div>';
      $('session-list').innerHTML = '<div class="empty">无数据</div>';
      return;
    }
    $('detail-name').textContent = `${stream.display_app}/${stream.stream_name}`;
    $('detail-meta').textContent = `${stream.video_width || '-'}×${stream.video_height || '-'} · 来源 ${stream.source.protocol || '-'} · ${stream.source.remote_addr || '-'}`;
    $('detail-health').innerHTML = `<span class="health ${escapeHTML(stream.health)}">${healthLabel(stream.health)}</span>`;
    $('detail-codecs').innerHTML = [stream.video_codec, stream.audio_codec].filter(Boolean).map(codec => `<span class="badge">${escapeHTML(codec)}</span>`).join('');
    $('detail-metrics').innerHTML = [
      metric('输入码率', formatBitrate(stream.input_bitrate_kbits), `累计 ${formatBytes(stream.read_bytes_sum)}`),
      metric('输出码率', formatBitrate(stream.output_bitrate_kbits), `累计 ${formatBytes(stream.wrote_bytes_sum)}`),
      metric('视频帧率', stream.fps && stream.fps.length ? `${Number(stream.video_fps || 0).toFixed(1)} FPS` : '暂无样本', `${stream.video_width || '-'}×${stream.video_height || '-'}`),
      metric('观看会话', stream.players, '当前下游订阅数')
    ].join('');
    $('detail-urls').innerHTML = renderURLs(stream);
    $('fps-chart').innerHTML = renderFPS(stream.fps);
    const sessions = [stream.source, ...(stream.consumers || [])].filter(item => item && (item.session_id || item.protocol || item.base_type));
    $('session-list').innerHTML = sessions.length ? sessions.map(sessionCard).join('') : '<div class="empty">暂无会话数据</div>';
    renderPlayer(stream);
  }

  function renderRuntime(payload) {
    const rt = payload.runtime || {};
    $('runtime-cards').innerHTML = [
      metric('运行时间', formatDuration(rt.uptime_sec), `${rt.os || '-'} / ${rt.arch || '-'}`),
      metric('Goroutines', rt.goroutines || 0, `${rt.cpu_count || 0} logical CPUs`),
      metric('当前内存', formatBytes(rt.alloc_bytes), `Heap ${formatBytes(rt.heap_inuse_bytes)}`),
      metric('系统内存', formatBytes(rt.sys_bytes), `GC ${rt.gc_count || 0} 次`)
    ].join('');
    const server = payload.server || {};
    const items = [
      ['Server ID', server.server_id], ['lal 版本', server.lal_version], ['API 版本', server.api_version],
      ['Go 版本', rt.go_version], ['启动时间', server.start_time], ['构建信息', server.bin_info]
    ];
    $('server-info').innerHTML = items.map(([key, value]) => `<dt>${escapeHTML(key)}</dt><dd>${escapeHTML(value || '-')}</dd>`).join('');
    const links = [
      ['全部流统计', withToken('/api/stat/all_group')], ['lal 服务信息', withToken('/api/stat/lal_info')]
    ];
    if (payload.features && payload.features.pprof && payload.features.pprof_url) links.push(['Go pprof', payload.features.pprof_url]);
    $('diagnostic-links').innerHTML = links.map(([label, href]) => `<a class="diagnostic-link" href="${escapeHTML(href)}" target="_blank" rel="noopener"><strong>${escapeHTML(label)}</strong><span>打开 ↗</span></a>`).join('');
  }

  function render(payload) {
    state.payload = payload;
    $('updated-at').textContent = '更新于 ' + formatTime(payload.updated_at);
    if (page === 'streams') renderOverview(payload);
    if (page === 'detail') renderDetail(payload);
    if (page === 'runtime') renderRuntime(payload);
  }

  async function fetchConsolePayload() {
    if (state.loading) return;
    state.loading = true;
    $('refresh-now').disabled = true;
    const params = new URLSearchParams({ _ts: Date.now().toString() });
    if (token) params.set('token', token);
    try {
      const response = await fetch('/api/console/overview?' + params.toString(), { cache: 'no-store', credentials: 'same-origin' });
      const result = await response.json();
      if (!response.ok || result.error_code !== 0 || !result.data) throw new Error(result.desp || `HTTP ${response.status}`);
      $('error-banner').hidden = true;
      $('connection-dot').className = 'connection-dot online';
      $('connection-text').textContent = '监控已连接';
      render(result.data);
    } catch (error) {
      $('error-banner').hidden = false;
      $('error-banner').textContent = '监控数据获取失败：' + error.message + (token ? '' : '。如果接口已启用鉴权，请使用 /console?token=你的密钥。');
      $('connection-dot').className = 'connection-dot offline';
      $('connection-text').textContent = '监控连接失败';
    } finally {
      state.loading = false;
      $('refresh-now').disabled = false;
    }
  }

  function resetTimer() {
    clearInterval(state.timer);
    const delay = Number($('refresh-interval').value) || 0;
    if (delay > 0) state.timer = setInterval(fetchConsolePayload, delay);
  }

  document.addEventListener('click', event => {
    const protocol = event.target.closest('[data-player-protocol]');
    if (protocol) {
      startPlayer(protocol.dataset.playerProtocol);
      return;
    }
    const row = event.target.closest('[data-detail]');
    if (row) location.href = row.dataset.detail;
    const copy = event.target.closest('[data-copy]');
    if (copy) navigator.clipboard.writeText(copy.dataset.copy).then(() => showToast('地址已复制')).catch(() => showToast('复制失败'));
  });

  $('stream-search').addEventListener('input', event => {
    state.search = event.target.value;
    if (state.payload) renderStreamList(state.payload.streams || []);
  });
  $('refresh-now').addEventListener('click', fetchConsolePayload);
  $('refresh-interval').addEventListener('change', resetTimer);
  $('player-stop').addEventListener('click', () => cleanupPlayer('已停止'));
  $('preview-video').addEventListener('playing', () => {
    if (playerState.active) setPlayerStatus(`${playerLabels[playerState.active]} 播放中`, 'playing');
    setPlayerPlaceholder('', true);
  });
  $('preview-video').addEventListener('waiting', () => {
    if (playerState.active) setPlayerStatus(`${playerLabels[playerState.active]} 缓冲中…`, '');
  });
  $('preview-video').addEventListener('error', () => {
    if (playerState.active) setPlayerStatus(`${playerLabels[playerState.active]} 解码或网络错误`, 'error');
  });
  document.addEventListener('visibilitychange', () => { if (!document.hidden) fetchConsolePayload(); });
  window.addEventListener('beforeunload', () => cleanupPlayer('页面关闭'));

  updateNavigation();
  resetTimer();
  fetchConsolePayload();
})();
