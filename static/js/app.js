/**
 * ToonFlow Frontend Application
 * Handles WebSocket connection, UI interaction, and progress rendering.
 */
(function () {
  'use strict';

  // --- State ---
  let ws = null;
  let reconnectTimer = null;
  let isGenerating = false;

  // --- DOM refs ---
  const els = {
    wsStatus: document.getElementById('ws-status'),
    scriptInput: document.getElementById('script-input'),
    styleSelect: document.getElementById('style-select'),
    durationSelect: document.getElementById('duration-select'),
    resolutionSelect: document.getElementById('resolution-select'),
    fpsSelect: document.getElementById('fps-select'),
    btnStart: document.getElementById('btn-start'),
    btnCancel: document.getElementById('btn-cancel'),
    btnReset: document.getElementById('btn-reset'),
    progressBar: document.getElementById('progress-fill'),
    progressText: document.getElementById('progress-text'),
    currentStep: document.getElementById('current-step'),
    logArea: document.getElementById('log-area'),
    storyboardPreview: document.getElementById('storyboard-preview'),
    videoPreview: document.getElementById('video-preview'),
    outputVideo: document.getElementById('output-video'),
    downloadLink: document.getElementById('download-link'),
    emptyResult: document.getElementById('empty-result'),
  };

  // --- WebSocket ---
  function connectWS() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${proto}//${location.host}/ws`;

    els.wsStatus.textContent = '🟡 连接中...';
    els.wsStatus.className = 'status-connecting';

    ws = new WebSocket(url);

    ws.onopen = function () {
      els.wsStatus.textContent = '🟢 已连接';
      els.wsStatus.className = 'status-connected';
      log('WebSocket 已连接', 'info');
    };

    ws.onmessage = function (event) {
      try {
        const msg = JSON.parse(event.data);
        handleMessage(msg);
      } catch (e) {
        log('收到无效消息: ' + event.data, 'error');
      }
    };

    ws.onclose = function () {
      els.wsStatus.textContent = '🔴 已断开';
      els.wsStatus.className = 'status-disconnected';
      log('WebSocket 断开，3秒后重连...', 'error');
      scheduleReconnect();
    };

    ws.onerror = function () {
      log('WebSocket 错误', 'error');
    };
  }

  function scheduleReconnect() {
    if (reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      connectWS();
    }, 3000);
  }

  function sendWS(action, data) {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      log('WebSocket 未连接，无法发送', 'error');
      return;
    }
    ws.send(JSON.stringify(Object.assign({ action: action }, data)));
  }

  // --- Message handling ---
  function handleMessage(msg) {
    switch (msg.step) {
      case 'waiting':
        log('任务已接收，排队中...', 'info');
        break;
      case 'parse_script':
        log('📖 ' + (msg.msg || '剧本解析中...'), 'info');
        updateProgress(msg.progress, msg.msg || '剧本解析中...');
        break;
      case 'gen_storyboard':
        log('✅ ' + (msg.msg || '分镜生成完成'), 'success');
        updateProgress(msg.progress, msg.msg || '分镜生成完成');
        if (msg.data && msg.data.storyboard) {
          renderStoryboard(msg.data.storyboard);
        }
        break;
      case 'gen_image':
        log('🎨 ' + (msg.msg || '正在生成图片...'), 'info');
        updateProgress(msg.progress, msg.msg || '正在生成图片...');
        break;
      case 'merge_video':
        log('🎬 ' + (msg.msg || '视频合成中...'), 'info');
        updateProgress(msg.progress, msg.msg || '视频合成中...');
        break;
      case 'finish':
        log('🎉 ' + (msg.msg || '生成完成！'), 'success');
        updateProgress(100, msg.msg || '生成完成！');
        isGenerating = false;
        updateButtons();
        if (msg.data && msg.data.video_url) {
          showResult(msg.data.video_url);
        }
        break;
      case 'error':
        log('❌ 错误: ' + (msg.msg || '未知错误'), 'error');
        isGenerating = false;
        updateButtons();
        break;
      default:
        log('[' + msg.step + '] ' + (msg.msg || ''), msg.code === 0 ? 'info' : 'error');
        if (msg.progress > 0) {
          updateProgress(msg.progress, msg.msg);
        }
    }
  }

  // --- UI helpers ---
  function log(msg, type) {
    const div = document.createElement('div');
    div.className = 'log-entry ' + (type || '');
    const time = new Date().toLocaleTimeString();
    div.textContent = '[' + time + '] ' + msg;
    els.logArea.appendChild(div);
    els.logArea.scrollTop = els.logArea.scrollHeight;
  }

  function updateProgress(pct, stepText) {
    els.progressBar.style.width = pct + '%';
    els.progressText.textContent = Math.round(pct) + '%';
    if (stepText) {
      els.currentStep.textContent = stepText;
    }
  }

  function renderStoryboard(items) {
    if (!items || items.length === 0) return;
    els.storyboardPreview.innerHTML = '';
    items.forEach(function (item) {
      const img = document.createElement('img');
      img.src = '/output/' + item.local_path || '';
      img.alt = 'Shot ' + item.shot_number;
      img.title = item.description || '';
      els.storyboardPreview.appendChild(img);
    });
  }

  function showResult(videoUrl) {
    els.emptyResult.style.display = 'none';
    els.videoPreview.style.display = 'block';
    els.outputVideo.src = videoUrl;
    els.downloadLink.href = videoUrl;
  }

  function updateButtons() {
    els.btnStart.disabled = isGenerating;
    els.btnCancel.disabled = !isGenerating;
  }

  // --- Event handlers ---
  els.btnStart.addEventListener('click', function () {
    const script = els.scriptInput.value.trim();
    if (!script) {
      log('请输入剧本文本', 'error');
      return;
    }

    isGenerating = true;
    updateButtons();

    // Reset UI
    els.progressBar.style.width = '0%';
    els.progressText.textContent = '0%';
    els.currentStep.textContent = '准备中...';
    els.logArea.innerHTML = '';
    els.storyboardPreview.innerHTML = '';
    els.videoPreview.style.display = 'none';
    els.emptyResult.style.display = 'block';

    sendWS('start_generate', {
      script: script,
      style: els.styleSelect.value,
      frame_duration: parseFloat(els.durationSelect.value),
      resolution: els.resolutionSelect.value,
      fps: parseInt(els.fpsSelect.value),
    });

    log('发送生成任务...', 'info');
  });

  els.btnCancel.addEventListener('click', function () {
    sendWS('cancel_generate', {});
    log('已发送取消请求', 'info');
  });

  els.btnReset.addEventListener('click', function () {
    els.scriptInput.value = '';
    els.progressBar.style.width = '0%';
    els.progressText.textContent = '0%';
    els.currentStep.textContent = '等待开始...';
    els.logArea.innerHTML = '';
    els.storyboardPreview.innerHTML = '';
    els.videoPreview.style.display = 'none';
    els.emptyResult.style.display = 'block';
    isGenerating = false;
    updateButtons();
    log('已重置', 'info');
  });

  // --- Init ---
  connectWS();
})();
