/**
 * AI-VIDEO — 短剧 AI 创作平台
 * AI-VIDEO 产品逻辑：项目制工作流、资产管理、分镜编辑、视频轨道
 */
(function () {
  'use strict';

  const AUTH_TOKEN_KEY = 'ai_video_token';
  const LEGACY_AUTH_TOKEN_KEY = 'toonflow_token';
  const GENERAL_SETTINGS_KEY = 'ai_video_general_settings';
  const LEGACY_GENERAL_SETTINGS_KEY = 'toonflow_general_settings';

  function migrateStorageKey(newKey, legacyKey) {
    const legacy = localStorage.getItem(legacyKey);
    if (legacy && !localStorage.getItem(newKey)) {
      localStorage.setItem(newKey, legacy);
    }
    if (legacy) localStorage.removeItem(legacyKey);
  }
  migrateStorageKey(AUTH_TOKEN_KEY, LEGACY_AUTH_TOKEN_KEY);
  migrateStorageKey(GENERAL_SETTINGS_KEY, LEGACY_GENERAL_SETTINGS_KEY);

  // ======================== 状态 ========================
  let authToken = localStorage.getItem(AUTH_TOKEN_KEY) || '';
  let currentUser = null;
  let editingProjectId = null;
  let ws = null;
  let reconnectTimer = null;
  let currentProject = null;
  let currentEpisode = null;
  let episodes = [];
  let sourceTexts = [];
  let wbStage = 'source';
  let planningType = 'skeleton';
  const planningTypeLabels = { skeleton: '故事骨架', strategy: '改编策略', script: '剧本' };
  const planningActionMap = {
    generate_skeleton: 'skeleton',
    generate_strategy: 'strategy',
    generate_script: 'script',
  };
  const workflowUserLabels = {
    analyze_events: '事件分析',
    split_episodes: 'AI 分集',
    generate_skeleton: '生成故事骨架',
    generate_strategy: '生成改编策略',
    generate_script: '生成剧本',
    generate_storyboard: '生成分镜',
    extract_assets: '从剧本提取资产',
    batch_generate_shot_images: '批量生成图片',
    batch_generate_shot_videos: '批量生成视频',
    generate_shot_video: '生成视频',
    delete_shot_clip: '删除视频版本',
  };
  const workflowLoadingLabels = {
    analyze_events: '分析中',
    split_episodes: '分集中',
    generate_skeleton: '生成中',
    generate_strategy: '生成中',
    generate_script: '生成中',
    generate_storyboard: '生成中',
    extract_assets: '提取中',
    batch_generate_shot_images: '生成中',
    batch_generate_shot_videos: '生成中',
    generate_shot_video: '生成中',
    delete_shot_clip: '删除中',
  };
  let storyboards = [];
  let shotClips = [];        // 分镜视频版本
  let timeline = null;       // 时间线编辑状态
  let assets = [];           // 当前项目的资产列表
  let editingAssetId = null; // 编辑中的资产 id
  let assetFilter = 'all';   // all | role | scene | prop
  let isGenerating = false;
  let clipVersionDropdownShot = null;
  let chatStreamSession = null;
  let pendingWorkflowUI = null;

  // ======================== DOM 引用 ========================
  const els = {
    // 页面导航
    navBtns: document.querySelectorAll('.nav-btn'),
    pages: document.querySelectorAll('.page'),
    // 项目
    projectList: document.getElementById('project-list'),
    projectEmpty: document.getElementById('project-empty'),
    projectName: document.getElementById('project-name'),
    assetList: document.getElementById('asset-list'),
    // 分镜
    storyboardList: document.getElementById('storyboard-list'),
    storyboardEmpty: document.getElementById('storyboard-empty'),
    storyboardCount: document.getElementById('storyboard-count'),
    storyboardSelectedCount: document.getElementById('storyboard-selected-count'),
    btnSelectAllShots: document.getElementById('btn-select-all-shots'),
    timelineVideoClips: document.getElementById('timeline-video-clips'),
    timelineAudioClips: document.getElementById('timeline-audio-clips'),
    timelinePreview: document.getElementById('timeline-preview'),
    narrationSegments: document.getElementById('narration-segments'),
    narrationDurationHint: document.getElementById('narration-duration-hint'),
    narrationPreview: document.getElementById('narration-preview'),
    videoExportArea: document.getElementById('video-export-area'),
    outputVideo: document.getElementById('output-video'),
    downloadLink: document.getElementById('download-link'),
    // 任务
    taskList: document.getElementById('task-list'),
    taskStats: document.getElementById('task-stats'),
    // 设置
    vendorList: document.getElementById('vendor-list'),
    // 状态栏
    statusLeft: document.getElementById('status-left'),
    activeTasks: document.getElementById('active-tasks-display'),
    projectDisplay: document.getElementById('project-display'),
    wsStatus: document.getElementById('ws-status'),
    wsStatusLabel: document.getElementById('ws-status-label'),
    // 弹窗
    modalNewProject: document.getElementById('modal-new-project'),
    modalAsset: document.getElementById('modal-asset'),
    modalVendor: document.getElementById('modal-vendor'),
    modalMediaPreview: document.getElementById('modal-media-preview'),
    mediaPreviewTitle: document.getElementById('media-preview-title'),
    mediaPreviewImage: document.getElementById('media-preview-image'),
    mediaPreviewVideo: document.getElementById('media-preview-video'),
    mediaPreviewPanel: document.getElementById('media-preview-panel'),
    mediaPreviewResizeGrip: document.getElementById('media-preview-resize-grip'),
    loginOverlay: document.getElementById('login-overlay'),
    userBadge: document.getElementById('user-badge'),
    btnLogout: document.getElementById('btn-logout'),
  };

  // ======================== 鉴权 ========================
  function unwrapApiBody(body) {
    if (!body || typeof body !== 'object' || Array.isArray(body)) return body;
    if (body.log_id != null && 'data' in body) return body.data;
    return body;
  }

  function normalizeAssetList(list) {
    if (Array.isArray(list)) return list;
    if (list && Array.isArray(list.data)) return list.data;
    if (list && Array.isArray(list.assets)) return list.assets;
    return [];
  }

  function apiFetch(url, options) {
    options = options || {};
    options.credentials = 'same-origin';
    options.headers = options.headers || {};
    if (authToken) {
      options.headers['Authorization'] = 'Bearer ' + authToken;
    }
    if (options.body && !options.headers['Content-Type']) {
      options.headers['Content-Type'] = 'application/json';
    }
    return fetch(url, options).then(r => {
      if (r.status === 401) {
        handleLogout(false);
        throw new Error('unauthorized');
      }
      const logId = r.headers.get('X-Log-ID');
      const origJson = r.json.bind(r);
      r.json = () => origJson().then(body => unwrapApiBody(body));
      r.logId = logId;
      return r;
    });
  }

  function showLogin() {
    els.loginOverlay.style.display = 'flex';
    document.getElementById('app').style.visibility = 'hidden';
  }

  function hideLogin() {
    els.loginOverlay.style.display = 'none';
    document.getElementById('app').style.visibility = 'visible';
  }

  function updateUserUI() {
    if (currentUser) {
      els.userBadge.textContent = '👤 ' + currentUser.username;
      els.userBadge.style.display = 'inline';
      els.btnLogout.style.display = 'inline-block';
    } else {
      els.userBadge.style.display = 'none';
      els.btnLogout.style.display = 'none';
    }
  }

  function handleLogout(clearMsg) {
    authToken = '';
    currentUser = null;
    localStorage.removeItem(AUTH_TOKEN_KEY);
    if (ws) { ws.close(); ws = null; }
    updateUserUI();
    showLogin();
    if (clearMsg !== false) toast('已退出登录', 'info');
  }

  function login(username, password) {
    return fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    }).then(r => {
      if (!r.ok) throw new Error('login failed');
      return r.json();
    }).then(data => {
      authToken = data.token;
      currentUser = { id: data.user_id, username: data.username };
      localStorage.setItem(AUTH_TOKEN_KEY, authToken);
      hideLogin();
      updateUserUI();
      bootApp();
      toast('欢迎回来，' + data.username, 'success');
    });
  }

  function checkSession() {
    if (!authToken) {
      showLogin();
      return Promise.reject(new Error('no token'));
    }
    return apiFetch('/api/me').then(r => r.json()).then(user => {
      currentUser = user;
      hideLogin();
      updateUserUI();
      bootApp();
    }).catch(() => {
      showLogin();
    });
  }

  document.getElementById('login-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const username = document.getElementById('login-username').value.trim();
    const password = document.getElementById('login-password').value;
    login(username, password).catch(() => toast('登录失败，请检查账号密码', 'error'));
  });

  els.btnLogout.addEventListener('click', () => {
    apiFetch('/api/logout', { method: 'POST' }).finally(() => handleLogout());
  });

  // ======================== 页面切换 ========================
  function showPage(pageId) {
    els.navBtns.forEach(b => b.classList.toggle('active', b.dataset.page === pageId));
    els.pages.forEach(p => p.classList.toggle('active', p.id === 'page-' + pageId));
  }

  // 工作台 tab 切换
  document.querySelectorAll('.wb-nav-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      switchWorkbenchPanel(btn.dataset.wb);
    });
  });

  document.querySelectorAll('.planning-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      setPlanningTab(tab.dataset.plan);
      loadPlanningContent();
    });
  });

  // 设置 tab 切换
  document.querySelectorAll('.settings-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      document.querySelectorAll('.settings-tab').forEach(t => t.classList.remove('active'));
      document.querySelectorAll('.settings-panel').forEach(p => p.classList.remove('active'));
      tab.classList.add('active');
      document.getElementById('panel-' + tab.dataset.tab).classList.add('active');
    });
  });

  // 导航按钮
  document.querySelectorAll('.nav-btn').forEach(btn => {
    btn.addEventListener('click', () => showPage(btn.dataset.page));
  });

  // ======================== WebSocket ========================
  function connectWS() {
    if (!authToken) return;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(proto + '//' + location.host + '/ws?token=' + encodeURIComponent(authToken));

    ws.onopen = () => {
      setWSStatus('connected');
      toast('WebSocket 已连接', 'success');
    };

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        onWSMessage(msg);
      } catch (e) {
        toast('收到无效消息', 'error');
      }
    };

    ws.onclose = () => {
      setWSStatus('disconnected');
      scheduleReconnect();
    };

    ws.onerror = () => setWSStatus('connecting');
  }

  function setWSStatus(status) {
    els.wsStatus.className = 'status-dot ' + status;
    if (els.wsStatusLabel) {
      const labels = { connected: '已连接', connecting: '连接中', disconnected: '未连接' };
      els.wsStatusLabel.textContent = labels[status] || status;
      els.wsStatusLabel.className = 'ws-status-label ' + status;
    }
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
      toast('WebSocket 未连接', 'error');
      return null;
    }
    ws.send(JSON.stringify(Object.assign({ action }, data)));
    return true;
  }

  // ======================== WS 消息处理 ========================
  function onWSMessage(msg) {
    if (msg.data && msg.data.task_update) {
      loadTasks();
      handleGenerationTaskUpdate(msg);
    }
    if (msg.step === 'workflow_error') {
      if (msg.data && msg.data.project_id && currentProject && msg.data.project_id !== currentProject.id) {
        return;
      }
      updateChatProgress('', 0);
      finishPendingWorkflowUI();
      appendChatMessage('assistant', '⚠️ ' + (msg.msg || '操作失败'));
      toast(msg.msg || '操作失败', 'error');
      setStatus('就绪');
      return;
    }
    if (msg.step === 'workflow_done') {
      if (msg.data && msg.data.project_id && currentProject && msg.data.project_id !== currentProject.id) {
        return;
      }
      updateChatProgress('', 0);
      finishPendingWorkflowUI();
      if (msg.data && msg.data.reply) {
        appendChatMessage('assistant', msg.data.reply);
      }
      applyWorkflowResult(msg.data && msg.data.action, msg.data || {});
      setStatus('就绪');
      return;
    }
    if (msg.step === 'chat_progress') {
      if (msg.data && msg.data.project_id && currentProject && msg.data.project_id !== currentProject.id) {
        return;
      }
      updateChatProgress(msg.msg || '处理中...', msg.progress);
      setStatus(msg.msg || '处理中...');
      const progressAction = msg.data && msg.data.action;
      if (progressAction && planningActionMap[progressAction] && msg.progress > 0 && msg.progress < 100) {
        showPlanningWorkInProgress(progressAction, msg.msg);
      }
      if (msg.progress >= 100 && msg.data && msg.data.action === 'extract_assets' && currentProject) {
        loadProjectAssets(currentProject.id);
        switchWorkbenchPanel('assets');
      }
      return;
    }
    if (msg.step === 'chat_stream') {
      if (!chatStreamSession || !msg.data) return;
      if (msg.data.project_id && currentProject && msg.data.project_id !== currentProject.id) return;
      if (msg.data.log_id && chatStreamSession.logId && msg.data.log_id !== chatStreamSession.logId) return;
      if (msg.data.log_id) chatStreamSession.logId = msg.data.log_id;
      enqueueChatStreamDelta(msg.data.delta || '');
      return;
    }
    if (msg.step === 'chat_stream_end') {
      if (!chatStreamSession || !msg.data) return;
      if (msg.data.project_id && currentProject && msg.data.project_id !== currentProject.id) return;
      if (msg.data.log_id && chatStreamSession.logId && msg.data.log_id !== chatStreamSession.logId) return;
      flushChatStreamQueue();
      return;
    }
    switch (msg.step) {
      case 'waiting':
        setStatus('任务已接收，排队中...');
        break;
      case 'parse_script':
        setStatus('📖 剧本解析中...');
        updateProgress(msg.progress);
        break;
      case 'gen_storyboard':
        setStatus('✅ 分镜生成完成');
        updateProgress(msg.progress);
        if (msg.data && msg.data.storyboard) {
          storyboards = normalizeStoryboards(msg.data.storyboard);
          renderStoryboards();
        }
        break;
      case 'gen_image':
        setStatus('🎨 AI 绘图中 (' + (msg.data?.current_shot || '?') + '/' + (msg.data?.total_shots || '?') + ')');
        updateProgress(msg.progress);
        if (msg.data && msg.data.shot) {
          const shot = normalizeStoryboards([msg.data.shot])[0];
          const idx = storyboards.findIndex(s => s.shot_number === shot.shot_number);
          if (idx >= 0) {
            const wasSelected = storyboards[idx].selected;
            storyboards[idx] = Object.assign({}, storyboards[idx], shot);
            storyboards[idx].selected = wasSelected;
          } else {
            storyboards.push(shot);
          }
          renderStoryboards();
          updateVideoTracksFromStoryboards();
        }
        break;
      case 'merge_video':
        setStatus('🎬 视频合成中...');
        updateProgress(msg.progress);
        break;
      case 'finish':
        setStatus('🎉 生成完成！');
        updateProgress(100);
        isGenerating = false;
        loadTasks();
        if (msg.data && msg.data.storyboard) {
          storyboards = normalizeStoryboards(msg.data.storyboard);
          renderStoryboards();
          updateVideoTracksFromStoryboards();
        }
        if (msg.data && msg.data.video_url) {
          showVideoResult(msg.data.video_url);
        }
        if (currentProject) loadProjectStoryboards(currentProject.id, currentEpisode?.id);
        toast('生成完成！', 'success');
        break;
      case 'error':
        setStatus('❌ ' + (msg.msg || '生成失败'));
        isGenerating = false;
        loadTasks();
        toast('错误: ' + (msg.msg || '未知错误'), 'error');
        break;
      default:
        if (msg.progress > 0) updateProgress(msg.progress);
        if (msg.data && msg.data.task_id) loadTasks();
    }
  }

  function handleGenerationTaskUpdate(msg) {
    const d = msg.data || {};
    const state = d.state || msg.step || '';
    if (d.project_id && currentProject && d.project_id !== currentProject.id) return;

    if (state === 'video_gen' || state === 'drawing') {
      setStatus(msg.msg || '生成中...');
      return;
    }
    if (state !== 'done' && state !== 'error') return;

    if (state === 'done' && currentProject) {
      const mode = d.mode || '';
      if (mode === 'video' || mode === 'images' || !mode) {
        loadProjectStoryboards(currentProject.id, currentEpisode?.id);
        loadShotClips();
      }
      if (msg.msg) toast(msg.msg, 'success');
    }
    if (state === 'error' && msg.msg) {
      toast(msg.msg, 'error');
    }
  }

  function updateProgress(pct) {
    // 在任务列表中更新对应任务的进度
    setStatus('进度: ' + Math.round(pct) + '%');
  }

  // ======================== 项目 CRUD ========================
  function loadProjects() {
    apiFetch('/api/projects').then(r => r.json()).then(list => {
      renderProjectList(list);
    }).catch(() => {});
  }

  function renderProjectList(projects) {
    if (!projects || projects.length === 0) {
      els.projectList.innerHTML = '';
      els.projectEmpty.style.display = 'block';
      return;
    }
    els.projectEmpty.style.display = 'none';
    els.projectList.innerHTML = projects.map(p => `
      <div class="project-card" data-id="${p.id}">
        <div class="project-card-title">
          ${escapeHtml(p.name || '未命名项目')}
          <span class="project-card-status status-${p.status || 'draft'}">${statusLabel(p.status)}</span>
        </div>
        <div style="font-size:12px;color:var(--text-secondary);margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">
          ${escapeHtml(p.intro || '')}
        </div>
        <div class="project-card-meta">
          <span>🎨 ${escapeHtml(p.art_style || '默认')}</span>
          <span>📐 ${escapeHtml(p.video_ratio || '16:9')}</span>
          <span>📅 ${p.create_time ? new Date(p.create_time).toLocaleDateString() : '-'}</span>
        </div>
        <div class="project-card-actions">
          <button class="btn btn-sm btn-outline btn-edit-project" data-id="${p.id}">编辑</button>
          <button class="btn btn-sm btn-outline btn-delete-project" data-id="${p.id}">删除</button>
        </div>
      </div>
    `).join('');

    els.projectList.querySelectorAll('.project-card').forEach(card => {
      card.addEventListener('click', (e) => {
        if (e.target.closest('.project-card-actions')) return;
        selectProject(card.dataset.id);
      });
    });
    els.projectList.querySelectorAll('.btn-edit-project').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        openProjectModal(btn.dataset.id);
      });
    });
    els.projectList.querySelectorAll('.btn-delete-project').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        deleteProject(btn.dataset.id);
      });
    });
  }

  function deleteProject(id) {
    if (!confirm('确定删除该项目？关联资产和分镜也会一并删除。')) return;
    apiFetch('/api/projects/' + id, { method: 'DELETE' }).then(() => {
      if (currentProject && currentProject.id === id) {
        currentProject = null;
        els.projectName.style.display = 'none';
        els.projectDisplay.textContent = '当前项目: 无';
        assets = [];
        storyboards = [];
        renderAssets();
        renderStoryboards();
      }
      loadProjects();
      toast('项目已删除', 'info');
    }).catch(() => toast('删除失败', 'error'));
  }

  function statusLabel(s) {
    const map = { draft: '草稿', processing: '进行中', done: '已完成', error: '失败' };
    return map[s] || s;
  }

  function selectProject(id) {
    apiFetch('/api/projects/' + id).then(r => r.json()).then(proj => {
      currentProject = proj;
      assets = normalizeAssetList(proj.assets);
      renderAssets();
      els.projectName.textContent = proj.name;
      els.projectName.style.display = 'inline';
      els.projectDisplay.textContent = '当前项目: ' + proj.name;
      document.getElementById('wb-project-name').textContent = proj.name;
      showPage('production');
      loadWorkbench();
      toast('已进入工作台: ' + proj.name, 'info');
    }).catch(() => toast('加载项目失败', 'error'));
  }

  function switchWorkbenchPanel(panel) {
    wbStage = panel;
    document.querySelectorAll('.wb-nav-btn').forEach(b => b.classList.toggle('active', b.dataset.wb === panel));
    document.querySelectorAll('.wb-panel').forEach(p => p.classList.remove('active'));
    const el = document.getElementById('wb-panel-' + panel);
    if (el) el.classList.add('active');
    if (panel === 'assets' && currentProject) {
      loadProjectAssets(currentProject.id);
    }
    if (panel === 'planning') loadPlanningContent();
    if (panel === 'storyboard' && currentProject) {
      loadProjectStoryboards(currentProject.id, currentEpisode?.id);
      loadShotClips();
    }
    if (panel === 'video' && currentProject && currentEpisode) {
      loadShotClips();
      loadTimeline();
    }
  }

  function loadWorkbench() {
    if (!currentProject) return;
    loadSourceTexts();
    loadEpisodes();
    loadChatMessages();
    loadProjectAssets(currentProject.id);
    loadProjectStoryboards(currentProject.id, currentEpisode?.id);
  }

  function loadSourceTexts() {
    if (!currentProject) return;
    apiFetch('/api/projects/' + currentProject.id + '/source-texts')
      .then(r => r.json())
      .then(list => {
        sourceTexts = list || [];
        renderSourceTexts();
      }).catch(() => {});
  }

  function renderSourceTexts() {
    const wrap = document.getElementById('source-text-table');
    if (!wrap) return;
    if (!sourceTexts.length) {
      wrap.innerHTML = '<div class="empty-state-sm"><p>还没有原文，点击「导入原文」开始</p></div>';
      return;
    }
    wrap.innerHTML = `<table class="data-table">
      <thead><tr><th>序号</th><th>卷</th><th>章节</th><th>内容</th><th>事件</th><th>操作</th></tr></thead>
      <tbody>${sourceTexts.map((s, i) => `
        <tr>
          <td>${i + 1}</td>
          <td>${escapeHtml(s.volume || '')}</td>
          <td>${escapeHtml(s.chapter_name || '')}</td>
          <td><span class="content-preview" title="${escapeHtml(s.content || '')}">${escapeHtml((s.content || '').slice(0, 40))}...</span></td>
          <td class="events-cell ${s.events ? 'has-events' : 'no-events'}">${escapeHtml(s.events ? s.events.slice(0, 80) + (s.events.length > 80 ? '...' : '') : '未分析')}</td>
          <td><button class="btn btn-sm btn-outline" onclick="window._app.deleteSource('${s.id}')">删除</button></td>
        </tr>`).join('')}</tbody></table>`;
  }

  function loadEpisodes() {
    if (!currentProject) return;
    apiFetch('/api/projects/' + currentProject.id + '/episodes')
      .then(r => r.json())
      .then(list => {
        episodes = list || [];
        renderEpisodeSelect();
        renderEpisodeList();
        if (episodes.length && !currentEpisode) {
          currentEpisode = episodes[0];
          renderEpisodeSelect();
        }
      }).catch(() => {});
  }

  function renderEpisodeSelect() {
    const sel = document.getElementById('wb-episode-select');
    if (!sel) return;
    if (!episodes.length) {
      sel.style.display = 'none';
      return;
    }
    sel.style.display = 'inline-block';
    sel.innerHTML = episodes.map(ep =>
      `<option value="${ep.id}" ${currentEpisode && currentEpisode.id === ep.id ? 'selected' : ''}>${escapeHtml(ep.title || ('EP' + ep.episode_num))}</option>`
    ).join('');
  }

  function renderEpisodeList() {
    const wrap = document.getElementById('episode-list');
    if (!wrap) return;
    if (!episodes.length) {
      wrap.innerHTML = '<div class="empty-state-sm"><p>导入原文后，使用「AI 分集」或对话让 AI 自动分集</p></div>';
      return;
    }
    wrap.innerHTML = episodes.map(ep => `
      <div class="episode-card ${currentEpisode && currentEpisode.id === ep.id ? 'active' : ''}" data-id="${ep.id}">
        <div class="episode-card-title">${escapeHtml(ep.title)}</div>
        <div class="episode-card-meta">时长 ${ep.params?.target_duration_minutes || 3} 分钟 · ${ep.params?.video_ratio || '16:9'} · ${ep.status || 'draft'}</div>
        <div class="content-preview" style="margin-top:8px;">${escapeHtml((ep.script_content || ep.events_ref || '').slice(0, 120))}</div>
      </div>`).join('');
    wrap.querySelectorAll('.episode-card').forEach(card => {
      card.addEventListener('click', () => {
        currentEpisode = episodes.find(e => e.id === card.dataset.id);
        renderEpisodeSelect();
        renderEpisodeList();
        loadPlanningContent();
        loadChatMessages();
      });
    });
  }

  function setPlanningTab(type) {
    planningType = type || 'skeleton';
    document.querySelectorAll('.planning-tab').forEach(t => {
      t.classList.toggle('active', t.dataset.plan === planningType);
    });
  }

  function sanitizePlanningContent(content) {
    if (!content) return '';
    const lines = String(content).split('\n');
    const out = [];
    for (const line of lines) {
      const t = line.trim().replace(/^`+|`+$/g, '');
      if (/^ACTION\s*[:：]/i.test(t)) continue;
      if (/^SHOT\s*[:：]/i.test(t)) continue;
      out.push(line);
    }
    return out.join('\n').trim();
  }

  function showPlanningContent(content) {
    const el = document.getElementById('planning-content');
    if (!el) return;
    const cleaned = sanitizePlanningContent(content);
    const text = cleaned ? cleaned : '暂无内容，请让 AI 生成或点击下方按钮';
    el.textContent = text;
  }

  function loadPlanningContent() {
    if (!currentProject || !currentEpisode) return;
    showPlanningContent('加载中...');
    apiFetch('/api/projects/' + currentProject.id + '/agent-work?type=' + planningType + '&episode_id=' + encodeURIComponent(currentEpisode.id))
      .then(r => r.json())
      .then(data => {
        showPlanningContent(data.content || '');
      }).catch(() => {
        showPlanningContent('');
      });
  }

  function loadChatMessages() {
    if (!currentProject) return;
    const epId = currentEpisode ? currentEpisode.id : '';
    apiFetch('/api/projects/' + currentProject.id + '/chat?episode_id=' + encodeURIComponent(epId))
      .then(r => r.json())
      .then(msgs => {
        const box = document.getElementById('wb-chat-messages');
        if (!box) return;
        if (!msgs.length) {
          box.innerHTML = '<div class="wb-chat-msg assistant">你好！我是 AI-VIDEO 创作助手。\n\n建议流程：\n1. 导入原文\n2. 事件分析 + AI 分集\n3. 选择一集，生成故事骨架 → 改编策略 → 剧本\n4. 生成分镜 → 提取资产 → 图片 → 视频\n\n操作方式：\n· 点击界面按钮 → 直接执行对应步骤\n· 聊天对话 → 仅在你明确要求且 AI 输出 ACTION 时才执行流程；否则为普通问答\n\n直接告诉我你想做什么即可。</div>';
          return;
        }
        box.innerHTML = msgs.map(m =>
          `<div class="wb-chat-msg ${m.role}">${escapeHtml(m.content)}</div>`
        ).join('');
        box.scrollTop = box.scrollHeight;
      }).catch(() => {});
  }

  function updateChatProgress(message, progress) {
    const wrap = document.getElementById('wb-chat-progress');
    const fill = document.getElementById('wb-chat-progress-fill');
    const text = document.getElementById('wb-chat-progress-text');
    if (!wrap || !fill || !text) return;
    if (!message) {
      wrap.style.display = 'none';
      fill.style.width = '0%';
      text.textContent = '';
      return;
    }
    wrap.style.display = 'block';
    fill.style.width = Math.min(100, Math.max(0, progress || 0)) + '%';
    text.textContent = message;
  }

  function scrollChatToBottom() {
    const box = document.getElementById('wb-chat-messages');
    if (box) box.scrollTop = box.scrollHeight;
  }

  function appendChatMessage(role, content) {
    const box = document.getElementById('wb-chat-messages');
    if (!box || !content) return;
    box.innerHTML += `<div class="wb-chat-msg ${role}">${escapeHtml(content)}</div>`;
    scrollChatToBottom();
  }

  function finishPendingWorkflowUI() {
    if (pendingWorkflowUI && pendingWorkflowUI.btn) {
      pendingWorkflowUI.btn.disabled = false;
      pendingWorkflowUI.btn.textContent = pendingWorkflowUI.origLabel;
    }
    pendingWorkflowUI = null;
  }

  function applyPlanningWorkResult(action, data) {
    const type = planningActionMap[action];
    if (!type) return;
    if (data && data.action_result && data.action_result.error) {
      toast('生成失败: ' + data.action_result.error, 'error');
      loadPlanningContent();
      return;
    }
    setPlanningTab(type);
    switchWorkbenchPanel('planning');
    const work = data && data.work;
    if (typeof work === 'string' && work.trim()) {
      showPlanningContent(work);
    } else {
      loadPlanningContent();
    }
    if (type === 'script') loadEpisodes();
    toast((planningTypeLabels[type] || '内容') + '已生成', 'success');
  }

  function showPlanningWorkInProgress(action, message) {
    const type = planningActionMap[action];
    if (!type) return;
    setPlanningTab(type);
    switchWorkbenchPanel('planning');
    const label = planningTypeLabels[type] || '';
    showPlanningContent(message || ('正在生成' + label + '，请稍候...'));
  }

  function applyStoryboardResult(data) {
    switchWorkbenchPanel('storyboard');
    const items = normalizeStoryboards(data && data.work);
    if (items.length > 0) {
      storyboards = items;
      renderStoryboards();
      updateVideoTracksFromStoryboards();
      toast('已生成 ' + items.length + ' 个分镜，请先从剧本提取资产再生图', 'success');
    } else if (currentProject) {
      loadProjectStoryboards(currentProject.id, currentEpisode?.id);
    }
  }

  function applyExtractAssetsResult(data) {
    const n = data && data.action_result && data.action_result.result && data.action_result.result.assets;
    loadProjectAssets(currentProject.id).then(() => {
      switchWorkbenchPanel('assets');
      toast(typeof n === 'number' ? ('已提取 ' + n + ' 项资产') : '资产已刷新', 'success');
    });
  }

  function applyWorkflowResult(action, data) {
    if (!action) return;
    if (action === 'analyze_events') {
      loadSourceTexts();
      const result = data && data.action_result && data.action_result.result;
      const n = result && result.analyzed;
      toast(n != null ? ('事件分析完成，共 ' + n + ' 章') : '事件分析完成', 'success');
      return;
    }
    if (action === 'split_episodes') {
      loadSourceTexts();
      const eps = data && data.work;
      if (Array.isArray(eps) && eps.length) {
        episodes = eps;
        currentEpisode = eps[0];
        loadEpisodes();
        switchWorkbenchPanel('script');
        toast('已分 ' + eps.length + ' 集', 'success');
      } else {
        loadEpisodes();
        switchWorkbenchPanel('script');
        toast('分集完成', 'success');
      }
      return;
    }
    if (planningActionMap[action]) {
      applyPlanningWorkResult(action, data);
      return;
    }
    if (action === 'generate_storyboard') {
      applyStoryboardResult(data);
      return;
    }
    if (action === 'extract_assets') {
      if (data && data.action_result && data.action_result.error) {
        toast('资产提取失败: ' + data.action_result.error, 'error');
        return;
      }
      applyExtractAssetsResult(data);
      return;
    }
    if (action === 'generate_shot_image') {
      const shot = data && data.action_result && data.action_result.result && data.action_result.result.shot_number;
      if (shot) {
        submitShotImagesViaWS([shot], '为第 ' + shot + ' 镜生成图片');
      }
      return;
    }
    if (action === 'batch_generate_shot_images') {
      isGenerating = true;
      loadTasks();
      if (currentProject) {
        loadProjectStoryboards(currentProject.id, currentEpisode?.id);
      }
      return;
    }
    if (action === 'generate_shot_video' || action === 'batch_generate_shot_videos') {
      loadTasks();
      loadShotClips();
      return;
    }
    if (action === 'delete_shot_clip') {
      loadShotClips();
      toast('视频版本已删除', 'info');
    }
  }

  function submitShotImagesViaWS(shotNumbers, userLabel) {
    if (!currentProject || !currentEpisode) {
      toast('请先选择项目与集', 'warning');
      return Promise.resolve();
    }
    if (!shotNumbers || !shotNumbers.length) {
      toast('请勾选分镜，或点击卡片上的「🎨 生成图片」', 'warning');
      return Promise.resolve();
    }
    if (isGenerating) {
      toast('已有生成任务进行中，请稍候', 'warning');
      return Promise.resolve();
    }
    return loadProjectAssets(currentProject.id).then(list => {
      if (!list || list.length === 0) {
        toast('请先从剧本提取资产后再生图', 'warning');
        switchWorkbenchPanel('assets');
        return;
      }
      runWorkflowViaWS(
        'batch_generate_shot_images',
        userLabel || ('为 ' + shotNumbers.length + ' 个分镜生成图片'),
        null,
        '生成中',
        { shotNumbers: shotNumbers }
      );
    });
  }

  function runWorkflowViaWS(action, userLabel, btn, loadingLabel, opts) {
    if (!currentProject) {
      toast('请先选择项目', 'warning');
      return;
    }
    opts = opts || {};
    const needsEpisode = {
      generate_skeleton: 1,
      generate_strategy: 1,
      generate_script: 1,
      generate_storyboard: 1,
      extract_assets: 1,
      generate_shot_image: 1,
      batch_generate_shot_images: 1,
      generate_shot_video: 1,
      batch_generate_shot_videos: 1,
    };
    if (needsEpisode[action] && !currentEpisode) {
      toast('请先选择一集', 'warning');
      return;
    }
    if (action === 'batch_generate_shot_videos' || action === 'generate_shot_video') {
      const shots = opts.shotNumbers || [];
      if (!shots.length) {
        toast('请至少选择一个分镜', 'warning');
        return;
      }
      const missingImage = shots.filter(n => {
        const sb = storyboards.find(s => s.shot_number === n);
        return !sb || !sb.image_url;
      });
      if (missingImage.length) {
        toast('请先生成图片：第 ' + missingImage.join('、') + ' 镜', 'warning');
        return;
      }
    }
    if (planningActionMap[action]) {
      showPlanningWorkInProgress(action);
    }
    const sent = sendWS('run_workflow', {
      action: 'run_workflow',
      workflow_action: action,
      project_id: currentProject.id,
      episode_id: currentEpisode ? currentEpisode.id : '',
      shot_numbers: opts.shotNumbers || [],
      workflow_params: opts.params || {},
      clip_id: opts.clipId || '',
    });
    if (!sent) return;
    appendChatMessage('user', userLabel);
    updateChatProgress((loadingLabel || userLabel) + '...', 5);
    setStatus((loadingLabel || userLabel) + '...');
    if (btn) {
      pendingWorkflowUI = { btn: btn, origLabel: btn.textContent };
      btn.disabled = true;
      btn.textContent = loadingLabel || '处理中';
    }
  }

  function beginChatStreamBubble(hint) {
    const box = document.getElementById('wb-chat-messages');
    if (!box) return null;
    const id = 'chat-stream-' + Date.now();
    const hintText = hint ? escapeHtml(hint) : '';
    box.innerHTML += `<div class="wb-chat-msg assistant streaming" id="${id}">${hintText}<span class="chat-stream-cursor">▍</span></div>`;
    scrollChatToBottom();
    chatStreamSession = {
      el: document.getElementById(id),
      cursor: document.querySelector('#' + id + ' .chat-stream-cursor'),
      queue: '',
      timer: null,
      logId: null,
    };
    return chatStreamSession.el;
  }

  function enqueueChatStreamDelta(delta) {
    if (!chatStreamSession || !chatStreamSession.el || !delta) return;
    chatStreamSession.queue += delta;
    if (!chatStreamSession.timer) {
      chatStreamSession.timer = setInterval(tickChatStreamTypewriter, 18);
    }
  }

  function tickChatStreamTypewriter() {
    if (!chatStreamSession || !chatStreamSession.el) return;
    if (!chatStreamSession.queue) {
      clearInterval(chatStreamSession.timer);
      chatStreamSession.timer = null;
      return;
    }
    const backlog = chatStreamSession.queue.length;
    const step = backlog > 80 ? 6 : backlog > 30 ? 3 : 1;
    const chunk = chatStreamSession.queue.slice(0, step);
    chatStreamSession.queue = chatStreamSession.queue.slice(step);
    const cursor = chatStreamSession.cursor;
    if (cursor && cursor.parentNode === chatStreamSession.el) {
      chatStreamSession.el.insertBefore(document.createTextNode(chunk), cursor);
    } else {
      chatStreamSession.el.appendChild(document.createTextNode(chunk));
    }
    scrollChatToBottom();
  }

  function flushChatStreamQueue() {
    if (!chatStreamSession) return;
    if (chatStreamSession.timer) {
      clearInterval(chatStreamSession.timer);
      chatStreamSession.timer = null;
    }
    if (chatStreamSession.queue && chatStreamSession.el) {
      const cursor = chatStreamSession.cursor;
      const rest = chatStreamSession.queue;
      chatStreamSession.queue = '';
      if (cursor && cursor.parentNode === chatStreamSession.el) {
        chatStreamSession.el.insertBefore(document.createTextNode(rest), cursor);
      } else {
        chatStreamSession.el.appendChild(document.createTextNode(rest));
      }
    }
    scrollChatToBottom();
  }

  function finalizeChatStreamBubble(finalText) {
    flushChatStreamQueue();
    if (chatStreamSession && chatStreamSession.el) {
      const text = (finalText != null && finalText !== '') ? String(finalText) : chatStreamSession.el.textContent.replace(/▍$/, '');
      chatStreamSession.el.textContent = text;
      chatStreamSession.el.classList.remove('streaming');
    }
    chatStreamSession = null;
    scrollChatToBottom();
  }

  function sendChat(message, silent) {
    if (!currentProject) { toast('请先选择项目', 'warning'); return Promise.reject(); }
    const box = document.getElementById('wb-chat-messages');
    if (!silent && box) {
      box.innerHTML += `<div class="wb-chat-msg user">${escapeHtml(message)}</div>`;
      box.scrollTop = box.scrollHeight;
    }
    updateChatProgress('等待 AI 响应...', 5);
    beginChatStreamBubble('');
    return apiFetch('/api/projects/' + currentProject.id + '/chat', {
      method: 'POST',
      body: JSON.stringify({
        message: message,
        episode_id: currentEpisode ? currentEpisode.id : '',
        stage: wbStage,
      }),
      signal: AbortSignal.timeout(10 * 60 * 1000),
    }).then(r => r.json()).then(res => {
      updateChatProgress('', 0);
      if (res.action && res.action.type) {
        finalizeChatStreamBubble(res.reply || '');
        handleChatAction(res);
      } else {
        finalizeChatStreamBubble(res.reply || '');
      }
      return res;
    }).catch(err => {
      updateChatProgress('', 0);
      finalizeChatStreamBubble('⚠️ 请求失败: ' + (err.message || err));
      throw err;
    });
  }

  function handleChatAction(res) {
    if (!res.action || !res.action.type) return;
    const t = res.action.type;
    if (t === 'generate_shot_image') {
      applyWorkflowResult(t, { action_result: res.action, work: res.work });
      return;
    }
    if (t === 'analyze_events' || t === 'split_episodes') {
      applyWorkflowResult(t, { work: res.work, action_result: res.action });
      return;
    }
    if (t === 'generate_skeleton' || t === 'generate_strategy' || t === 'generate_script') {
      applyPlanningWorkResult(t, { work: res.work, action_result: res.action });
      return;
    }
    if (t === 'generate_storyboard' || t === 'extract_assets') {
      applyWorkflowResult(t, { work: res.work, action_result: res.action });
      return;
    }
    toast('已执行: ' + t, 'success');
  }

  function getEpisodeScript() {
    if (currentEpisode && currentEpisode.script_content) return currentEpisode.script_content;
    return '';
  }

  function normalizeStoryboards(list) {
    if (!Array.isArray(list)) return [];
    return list.map((sb, i) => ({
      shot_number: sb.shot_number ?? sb.ShotNumber ?? (i + 1),
      scene: sb.scene ?? sb.Scene ?? '',
      description: sb.description ?? sb.Description ?? '',
      camera: sb.camera ?? sb.Camera ?? '固定镜头',
      duration: sb.duration ?? sb.Duration ?? 3,
      prompt: sb.prompt ?? sb.Prompt ?? sb.description ?? sb.Description ?? '',
      image_url: sb.image_url ?? sb.ImageURL ?? '',
      image_remote_url: sb.image_remote_url ?? sb.ImageRemoteURL ?? '',
      selected: sb.selected === true,
    }));
  }

  function getSelectedShotNumbers() {
    return storyboards.filter(sb => sb.selected === true).map(sb => sb.shot_number);
  }

  function countSelectedStoryboards() {
    return storyboards.filter(sb => sb.selected === true).length;
  }

  function updateStoryboardSelectionUI() {
    const selected = countSelectedStoryboards();
    const total = storyboards.length;
    if (els.storyboardSelectedCount) {
      els.storyboardSelectedCount.textContent = total > 0 ? `已选 ${selected}/${total}` : '已选 0/0';
    }
    if (els.btnSelectAllShots) {
      els.btnSelectAllShots.textContent = total > 0 && selected === total ? '取消全选' : '全选';
    }
  }

  function setAllStoryboardsSelected(selected) {
    storyboards.forEach(sb => { sb.selected = selected; });
    renderStoryboards();
  }

  function loadProjectStoryboards(projectId, episodeId) {
    let url = '/api/storyboards?project_id=' + encodeURIComponent(projectId);
    if (episodeId) {
      url += '&episode_id=' + encodeURIComponent(episodeId);
    }
    return apiFetch(url).then(r => r.json()).then(list => {
        storyboards = normalizeStoryboards(list || []);
        renderStoryboards();
        updateVideoTracksFromStoryboards();
      })
      .catch(() => {
        storyboards = [];
        renderStoryboards();
      });
  }

  function updateVideoTracksFromStoryboards() { /* deprecated: use timeline editor */ }

  function clipsForShot(shotNumber) {
    return (shotClips || []).filter(c => c.shot_number === shotNumber);
  }

  function loadShotClips() {
    if (!currentProject || !currentEpisode) { shotClips = []; return Promise.resolve([]); }
    return apiFetch('/api/projects/' + currentProject.id + '/shot-clips?episode_id=' +
      encodeURIComponent(currentEpisode.id))
      .then(r => r.json())
      .then(list => { shotClips = list || []; renderStoryboards(); return shotClips; })
      .catch(() => { shotClips = []; });
  }

  function generateShotVideo(shotNumber) {
    if (!currentProject || !currentEpisode) { toast('请先选择项目与集', 'warning'); return Promise.resolve(); }
    const sb = storyboards.find(s => s.shot_number === shotNumber);
    if (!sb || !sb.image_url) {
      toast('请先生成该分镜图片', 'warning');
      return Promise.resolve();
    }
    runWorkflowViaWS(
      'generate_shot_video',
      '为第 ' + shotNumber + ' 镜生成视频',
      null,
      '生成中',
      { shotNumbers: [shotNumber] }
    );
    return Promise.resolve();
  }

  async function batchGenerateShotVideos() {
    const shots = getSelectedShotNumbers();
    if (!shots.length) {
      toast('请至少勾选一个分镜', 'warning');
      return;
    }
    if (!currentProject || !currentEpisode) {
      toast('请先选择项目与集', 'warning');
      return;
    }
    runWorkflowViaWS(
      'batch_generate_shot_videos',
      '批量生成视频（' + shots.length + ' 镜）',
      document.getElementById('btn-batch-gen-video'),
      '生成中',
      { shotNumbers: shots }
    );
  }

  function selectShotClip(clipId) {
    return apiFetch('/api/shot-clips/' + clipId + '/select', { method: 'PUT' })
      .then(r => r.json())
      .then(() => loadShotClips())
      .catch(() => toast('选版失败', 'error'));
  }

  function deleteShotClip(clipId) {
    if (!confirm('确定删除此视频版本？')) return Promise.resolve();
    runWorkflowViaWS(
      'delete_shot_clip',
      '删除视频版本',
      null,
      '删除中',
      { clipId: clipId }
    );
    return Promise.resolve();
  }

  function closeClipVersionDropdown() {
    clipVersionDropdownShot = null;
    syncClipVersionDropdownUI();
  }

  function toggleClipVersionDropdown(shotNum) {
    clipVersionDropdownShot = clipVersionDropdownShot === shotNum ? null : shotNum;
    syncClipVersionDropdownUI();
  }

  function syncClipVersionDropdownUI() {
    if (!els.storyboardList) return;
    els.storyboardList.querySelectorAll('.sb-version-dropdown').forEach(el => {
      const shot = parseInt(el.dataset.shot, 10);
      const open = shot === clipVersionDropdownShot;
      el.classList.toggle('is-open', open);
      const btn = el.querySelector('.sb-version-toggle');
      if (btn) btn.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
    els.storyboardList.querySelectorAll('.storyboard-card').forEach(card => {
      card.classList.toggle('has-version-menu-open', !!card.querySelector('.sb-version-dropdown.is-open'));
    });
  }

  function renderStoryboardClipVersionMenu(shotNum, sortedVersions, versionBtnLabel) {
    const isOpen = clipVersionDropdownShot === shotNum;
    const items = sortedVersions.map(v => {
      const title = '第 ' + shotNum + ' 镜 — 视频 v' + v.version;
      const label = 'v' + v.version + (v.is_selected ? ' ✓' : '') + (v.source === 'fallback' ? ' ·兜底' : '');
      return `<div class="clip-version-row">
        <button type="button" class="clip-version-chip ${v.is_selected ? 'selected' : ''}" data-clip-id="${escapeHtml(v.id)}" title="切换到此版本">${label}</button>
        ${v.file_url
          ? `<button type="button" class="btn btn-sm btn-outline clip-version-preview-btn" data-url="${escapeHtml(v.file_url)}" data-title="${escapeHtml(title)}">预览</button>`
          : ''}
        <button type="button" class="btn btn-sm btn-outline clip-version-del-btn" data-clip-id="${escapeHtml(v.id)}">删除</button>
      </div>`;
    }).join('');
    return `<div class="sb-version-dropdown${isOpen ? ' is-open' : ''}" data-shot="${shotNum}">
      <button type="button" class="btn btn-sm btn-outline sb-version-toggle sb-col-btn" data-shot="${shotNum}" aria-expanded="${isOpen}"${sortedVersions.length ? '' : ' disabled title="暂无视频版本"'}>
        ${versionBtnLabel}<span class="sb-version-caret" aria-hidden="true">▾</span>
      </button>
      <div class="sb-version-menu" role="menu">${items || '<div class="clip-versions-empty">暂无版本</div>'}</div>
    </div>`;
  }

  function clipPlayDuration(clip) {
    if (!clip) return 0;
    const start = clip.start || 0;
    let end = clip.end;
    if (!end || end <= 0) end = clip.duration || 0;
    if (end <= start) return clip.duration > 0 ? clip.duration : 3;
    return end - start;
  }

  function timelineVideoDuration() {
    const track = getVideoTrack();
    if (!track || !track.clips) return 0;
    return track.clips.reduce((sum, c) => sum + clipPlayDuration(c), 0);
  }

  function renderNarrationPanel() {
    const dur = timelineVideoDuration();
    if (els.narrationDurationHint) {
      els.narrationDurationHint.textContent = dur > 0
        ? ('成片时长：' + dur.toFixed(1) + ' 秒')
        : '成片时长：—';
    }
    const plan = timeline && timeline.narration;
    const segs = plan && plan.segments ? plan.segments : [];
    if (!els.narrationSegments) return;
    if (!segs.length) {
      els.narrationSegments.innerHTML = '<div class="empty-state-sm"><p>点击「生成旁白方案」根据成片时长自动撰写解说词</p></div>';
    } else {
      els.narrationSegments.innerHTML = segs.map((seg, i) => `
        <div class="narration-seg-item" data-seg-index="${i}">
          <div class="narration-seg-meta">${(seg.start || 0).toFixed(1)}s ~ ${(seg.end || 0).toFixed(1)}s${seg.shot_number ? ' · 第' + seg.shot_number + '镜' : ''}</div>
          <textarea class="narration-seg-text" data-i="${i}" rows="2">${escapeHtml(seg.text || '')}</textarea>
        </div>
      `).join('');
      els.narrationSegments.querySelectorAll('.narration-seg-text').forEach(ta => {
        ta.onchange = () => {
          if (!timeline.narration || !timeline.narration.segments) return;
          timeline.narration.segments[parseInt(ta.dataset.i, 10)].text = ta.value;
        };
      });
    }
    if (els.narrationPreview) {
      const audioURL = plan && plan.audio_url;
      if (audioURL) {
        els.narrationPreview.style.display = 'block';
        els.narrationPreview.src = audioURL;
      } else {
        els.narrationPreview.style.display = 'none';
        els.narrationPreview.removeAttribute('src');
      }
    }
  }

  function planNarration() {
    if (!currentProject || !currentEpisode) {
      toast('请先选择项目与集', 'warning');
      return;
    }
    if (timelineVideoDuration() <= 0) {
      toast('时间线没有视频片段，请先载入分镜视频', 'warning');
      return;
    }
    toast('正在生成旁白方案…', 'info');
    apiFetch('/api/projects/' + currentProject.id + '/narration/plan', {
      method: 'POST',
      body: JSON.stringify({ episode_id: currentEpisode.id }),
      signal: AbortSignal.timeout(5 * 60 * 1000),
    }).then(r => r.json()).then(res => {
      if (res.error) throw new Error(res.error);
      if (res.timeline) timeline = res.timeline;
      renderTimelineEditor();
      toast('旁白方案已生成，可编辑后点击「合成配音」', 'success');
    }).catch(err => toast('生成旁白方案失败: ' + (err.message || err), 'error'));
  }

  function synthesizeNarration() {
    if (!currentProject || !currentEpisode) {
      toast('请先选择项目与集', 'warning');
      return;
    }
    const plan = timeline && timeline.narration;
    if (!plan || !plan.segments || !plan.segments.length) {
      toast('请先生成旁白方案', 'warning');
      return;
    }
    toast('正在合成旁白配音（可能需要几分钟）…', 'info');
    apiFetch('/api/projects/' + currentProject.id + '/narration/synthesize', {
      method: 'POST',
      body: JSON.stringify({
        episode_id: currentEpisode.id,
        segments: plan.segments,
        voice: plan.voice || '',
      }),
      signal: AbortSignal.timeout(15 * 60 * 1000),
    }).then(r => r.json()).then(res => {
      if (res.error) throw new Error(res.error);
      if (res.timeline) timeline = res.timeline;
      renderTimelineEditor();
      toast('旁白配音已合成并加入音频轨道，可导出成片', 'success');
    }).catch(err => toast('合成配音失败: ' + (err.message || err), 'error'));
  }

  function loadTimeline() {
    if (!currentProject || !currentEpisode) return Promise.resolve(null);
    return apiFetch('/api/projects/' + currentProject.id + '/timeline?episode_id=' +
      encodeURIComponent(currentEpisode.id))
      .then(r => r.json())
      .then(tl => { timeline = tl; renderTimelineEditor(); return tl; })
      .catch(() => { timeline = null; renderTimelineEditor(); });
  }

  function saveTimeline() {
    if (!currentProject || !currentEpisode || !timeline) return;
    timeline.project_id = currentProject.id;
    timeline.episode_id = currentEpisode.id;
    apiFetch('/api/projects/' + currentProject.id + '/timeline', {
      method: 'PUT',
      body: JSON.stringify(timeline),
    }).then(() => toast('时间线已保存', 'success'))
      .catch(() => toast('保存失败', 'error'));
  }

  function exportTimeline() {
    if (!currentProject || !currentEpisode) return;
    if (timeline) saveTimeline();
    toast('正在导出成片…', 'info');
    apiFetch('/api/projects/' + currentProject.id + '/timeline/export', {
      method: 'POST',
      body: JSON.stringify({ episode_id: currentEpisode.id, timeline: timeline }),
      signal: AbortSignal.timeout(10 * 60 * 1000),
    }).then(r => r.json()).then(res => {
      if (res.error) throw new Error(res.error);
      showVideoResult(res.video_url);
      toast('导出完成', 'success');
    }).catch(err => toast('导出失败: ' + (err.message || err), 'error'));
  }

  function reloadTimelineFromSelected() {
    loadTimeline().then(() => toast('已从选中分镜视频载入时间线', 'success'));
  }

  function getVideoTrack() {
    if (!timeline) return null;
    return (timeline.tracks || []).find(t => t.type === 'video') || null;
  }

  function getAudioTrack() {
    if (!timeline) return null;
    return (timeline.tracks || []).find(t => t.type === 'audio') || null;
  }

  function renderTimelineEditor() {
    if (!els.timelineVideoClips) return;
    const vTrack = getVideoTrack();
    const aTrack = getAudioTrack();
    const vClips = vTrack ? vTrack.clips || [] : [];
    const aClips = aTrack ? aTrack.clips || [] : [];

    if (vClips.length === 0) {
      els.timelineVideoClips.innerHTML = '<div class="empty-state-sm"><p>暂无片段，点击「载入选中分镜」或先在分镜页生成视频</p></div>';
    } else {
      els.timelineVideoClips.innerHTML = vClips.map((clip, i) => `
        <div class="timeline-clip-item" data-v-index="${i}">
          <div class="timeline-clip-header">
            <span class="timeline-clip-label">${escapeHtml(clip.label || ('片段 ' + (i + 1)))}</span>
            <div class="timeline-clip-actions">
              <button class="btn btn-sm btn-outline tl-preview-btn" data-url="${escapeHtml(clip.file_url || '')}">预览</button>
              <button class="btn btn-sm btn-outline tl-up-btn" data-i="${i}">↑</button>
              <button class="btn btn-sm btn-outline tl-down-btn" data-i="${i}">↓</button>
              <button class="btn btn-sm btn-outline tl-del-btn" data-i="${i}">×</button>
            </div>
          </div>
          <div class="timeline-clip-trim">
            <div><label>入点 (s)</label><input type="number" step="0.1" min="0" class="tl-trim-start" data-i="${i}" value="${clip.start || 0}"></div>
            <div><label>出点 (s)</label><input type="number" step="0.1" min="0" class="tl-trim-end" data-i="${i}" value="${clip.end || clip.duration || 3}"></div>
          </div>
        </div>
      `).join('');
    }

    if (aClips.length === 0) {
      els.timelineAudioClips.innerHTML = '<div class="empty-state-sm"><p>可添加背景音乐或配音 URL</p></div>';
    } else {
      els.timelineAudioClips.innerHTML = aClips.map((clip, i) => `
        <div class="timeline-clip-item">
          <div class="timeline-clip-header">
            <span class="timeline-clip-label">${escapeHtml(clip.label || ('🎵 音频 ' + (i + 1)))}</span>
            <button class="btn btn-sm btn-outline tl-audio-del" data-i="${i}">×</button>
          </div>
          <div style="font-size:11px;color:var(--text-secondary);word-break:break-all">${escapeHtml(clip.file_url || '')}</div>
          <div class="timeline-clip-trim" style="margin-top:6px">
            <div><label>起始 (s)</label><input type="number" step="0.1" min="0" class="tl-audio-start" data-i="${i}" value="${clip.start || 0}"></div>
          </div>
        </div>
      `).join('');
    }

    bindTimelineEvents();
    renderNarrationPanel();
  }

  function bindTimelineEvents() {
    if (!els.timelineVideoClips) return;
    els.timelineVideoClips.querySelectorAll('.tl-preview-btn').forEach(btn => {
      btn.onclick = () => { if (els.timelinePreview) els.timelinePreview.src = btn.dataset.url; };
    });
    els.timelineVideoClips.querySelectorAll('.tl-trim-start').forEach(inp => {
      inp.onchange = () => {
        const track = getVideoTrack(); if (!track) return;
        const i = parseInt(inp.dataset.i, 10);
        track.clips[i].start = parseFloat(inp.value) || 0;
      };
    });
    els.timelineVideoClips.querySelectorAll('.tl-trim-end').forEach(inp => {
      inp.onchange = () => {
        const track = getVideoTrack(); if (!track) return;
        const i = parseInt(inp.dataset.i, 10);
        track.clips[i].end = parseFloat(inp.value) || track.clips[i].duration;
      };
    });
    els.timelineVideoClips.querySelectorAll('.tl-up-btn').forEach(btn => {
      btn.onclick = () => moveTimelineClip('video', parseInt(btn.dataset.i, 10), -1);
    });
    els.timelineVideoClips.querySelectorAll('.tl-down-btn').forEach(btn => {
      btn.onclick = () => moveTimelineClip('video', parseInt(btn.dataset.i, 10), 1);
    });
    els.timelineVideoClips.querySelectorAll('.tl-del-btn').forEach(btn => {
      btn.onclick = () => {
        const track = getVideoTrack(); if (!track) return;
        track.clips.splice(parseInt(btn.dataset.i, 10), 1);
        renderTimelineEditor();
      };
    });
    if (els.timelineAudioClips) {
      els.timelineAudioClips.querySelectorAll('.tl-audio-del').forEach(btn => {
        btn.onclick = () => {
          const track = getAudioTrack(); if (!track) return;
          track.clips.splice(parseInt(btn.dataset.i, 10), 1);
          renderTimelineEditor();
        };
      });
      els.timelineAudioClips.querySelectorAll('.tl-audio-start').forEach(inp => {
        inp.onchange = () => {
          const track = getAudioTrack(); if (!track) return;
          track.clips[parseInt(inp.dataset.i, 10)].start = parseFloat(inp.value) || 0;
        };
      });
    }
  }

  function moveTimelineClip(type, index, delta) {
    const track = type === 'video' ? getVideoTrack() : getAudioTrack();
    if (!track || !track.clips) return;
    const newIdx = index + delta;
    if (newIdx < 0 || newIdx >= track.clips.length) return;
    const tmp = track.clips[index];
    track.clips[index] = track.clips[newIdx];
    track.clips[newIdx] = tmp;
    renderTimelineEditor();
  }

  function addTimelineAudio() {
    const url = document.getElementById('timeline-audio-url')?.value?.trim();
    if (!url) { toast('请输入音频 URL', 'warning'); return; }
    if (!timeline) timeline = { tracks: [{ type: 'video', clips: [] }, { type: 'audio', clips: [] }] };
    let aTrack = getAudioTrack();
    if (!aTrack) {
      aTrack = { type: 'audio', clips: [] };
      timeline.tracks.push(aTrack);
    }
    aTrack.clips.push({ id: 'aud_' + Date.now(), file_url: url, start: 0, duration: 0 });
    document.getElementById('timeline-audio-url').value = '';
    renderTimelineEditor();
  }

  function primaryClipForShot(shotNumber) {
    const clips = clipsForShot(shotNumber);
    if (!clips.length) return null;
    const selected = clips.find(c => c.is_selected);
    if (selected) return selected;
    return clips.slice().sort((a, b) => (b.version || 0) - (a.version || 0))[0];
  }

  function openMediaPreview(type, url, title) {
    if (!url || !els.modalMediaPreview) return;
    if (els.mediaPreviewTitle) els.mediaPreviewTitle.textContent = title || '预览';
    if (els.mediaPreviewImage) els.mediaPreviewImage.style.display = 'none';
    if (els.mediaPreviewVideo) {
      els.mediaPreviewVideo.pause();
      els.mediaPreviewVideo.style.display = 'none';
      els.mediaPreviewVideo.removeAttribute('src');
    }
    if (type === 'image' && els.mediaPreviewImage) {
      els.mediaPreviewImage.src = url;
      els.mediaPreviewImage.style.display = 'block';
    } else if (els.mediaPreviewVideo) {
      els.mediaPreviewVideo.src = url;
      els.mediaPreviewVideo.style.display = 'block';
      els.mediaPreviewVideo.play().catch(() => {});
    }
    els.modalMediaPreview.style.display = 'flex';
  }

  function closeMediaPreview() {
    if (!els.modalMediaPreview) return;
    if (els.mediaPreviewVideo) {
      els.mediaPreviewVideo.pause();
      els.mediaPreviewVideo.removeAttribute('src');
      els.mediaPreviewVideo.load();
    }
    if (els.mediaPreviewImage) els.mediaPreviewImage.removeAttribute('src');
    els.modalMediaPreview.style.display = 'none';
  }

  function renderStoryboardImageColumn(sb, i) {
    const shotNum = sb.shot_number || i + 1;
    const imageTitle = `第 ${shotNum} 镜 — 图片`;
    const previewBtn = sb.image_url
      ? `<button type="button" class="btn btn-sm btn-outline sb-media-preview-btn sb-col-btn" data-preview-type="image" data-url="${escapeHtml(sb.image_url)}" data-title="${escapeHtml(imageTitle)}">🖼 预览图片</button>`
      : '<button type="button" class="btn btn-sm btn-outline sb-col-btn" disabled title="暂无图片">🖼 预览图片</button>';
    const imageBlock = sb.image_url
      ? `<div class="sb-thumb sb-thumb-image">
           <img src="${escapeHtml(sb.image_url)}" alt="${escapeHtml(imageTitle)}" loading="lazy" decoding="async">
           <button type="button" class="sb-thumb-hit sb-media-preview-btn" data-preview-type="image" data-url="${escapeHtml(sb.image_url)}" data-title="${escapeHtml(imageTitle)}" title="点击放大预览" aria-label="预览图片"></button>
         </div>`
      : '<div class="sb-thumb sb-thumb-empty">暂无图片</div>';
    return `<div class="sb-media-col sb-media-col-image">
      <div class="sb-media-col-toolbar">
        ${previewBtn}
        <span class="sb-media-col-title">图片</span>
        <button class="btn btn-sm btn-outline sb-gen-image-btn sb-col-btn" type="button" data-shot="${shotNum}">🎨 生成图片</button>
      </div>
      ${imageBlock}
    </div>`;
  }

  function renderStoryboardVideoColumn(sb, i) {
    const shotNum = sb.shot_number || i + 1;
    const versions = clipsForShot(shotNum);
    const sortedVersions = versions.slice().sort((a, b) => (a.version || 0) - (b.version || 0));
    const clip = primaryClipForShot(shotNum);
    const videoTitle = clip && clip.file_url ? `第 ${shotNum} 镜 — 视频 v${clip.version}` : '';
    const previewBtn = clip && clip.file_url
      ? `<button type="button" class="btn btn-sm btn-outline sb-media-preview-btn sb-col-btn" data-preview-type="video" data-url="${escapeHtml(clip.file_url)}" data-title="${escapeHtml(videoTitle)}">▶ 预览视频</button>`
      : '<button type="button" class="btn btn-sm btn-outline sb-col-btn" disabled title="暂无视频">▶ 预览视频</button>';

    let videoBlock = '<div class="sb-thumb sb-thumb-empty">暂无视频</div>';
    let videoMeta = '';
    if (clip && clip.file_url) {
      const poster = sb.image_url ? ` poster="${escapeHtml(sb.image_url)}"` : '';
      videoBlock = `<div class="sb-thumb sb-thumb-video">
          <video src="${escapeHtml(clip.file_url)}" muted playsinline preload="metadata"${poster}></video>
          <span class="sb-thumb-play" aria-hidden="true">▶</span>
          <button type="button" class="sb-thumb-hit sb-media-preview-btn" data-preview-type="video" data-url="${escapeHtml(clip.file_url)}" data-title="${escapeHtml(videoTitle)}" title="点击放大预览" aria-label="预览视频"></button>
        </div>`;
      videoMeta = `<div class="sb-media-video-meta">v${clip.version}${clip.is_selected ? ' ✓' : ''}${clip.source === 'fallback' ? ' ·兜底' : ''}</div>`;
    }

    const versionBtnLabel = sortedVersions.length
      ? ('版本 · v' + (clip ? clip.version : sortedVersions[sortedVersions.length - 1].version)
        + (sortedVersions.length > 1 ? '（共' + sortedVersions.length + '版）' : ''))
      : '版本';

    return `<div class="sb-media-col sb-media-col-video">
      <div class="sb-media-col-version-bar">
        ${renderStoryboardClipVersionMenu(shotNum, sortedVersions, versionBtnLabel)}
      </div>
      <div class="sb-media-col-toolbar">
        ${previewBtn}
        <span class="sb-media-col-title">视频</span>
        <button class="btn btn-sm btn-primary sb-gen-video-btn sb-col-btn" type="button" data-shot="${shotNum}">🎬 生成视频</button>
      </div>
      ${videoBlock}
      ${videoMeta}
    </div>`;
  }

  function startGeneration(mode, shotNumbers) {
    if (mode === 'images') {
      const selectedShots = Array.isArray(shotNumbers) && shotNumbers.length
        ? shotNumbers
        : getSelectedShotNumbers();
      if (!currentProject) { toast('请先选择或创建项目', 'warning'); return Promise.resolve(); }
      if (!currentEpisode) { toast('请先选择一集', 'warning'); return Promise.resolve(); }
      if (storyboards.length === 0) { toast('请先生成分镜', 'warning'); return Promise.resolve(); }
      return submitShotImagesViaWS(
        selectedShots,
        selectedShots.length === 1
          ? ('为第 ' + selectedShots[0] + ' 镜生成图片')
          : ('批量生成图片（' + selectedShots.length + ' 镜）')
      );
    }
    if (mode === 'video') {
      toast('请在「视频」页使用时间线手动导出成片', 'info');
      switchWorkbenchPanel('video');
      return Promise.resolve();
    }
    if (!currentProject) { toast('请先选择或创建项目', 'warning'); return Promise.resolve(); }
    const script = getEpisodeScript();
    if ((mode === 'full' || mode === 'parse') && !script) {
      toast('请先在 AI策划 中为当前集生成剧本', 'warning');
      switchWorkbenchPanel('planning');
      return Promise.resolve();
    }
    const sent = sendWS('start_generate', {
      action: 'start_generate',
      mode: mode,
      project_id: currentProject.id,
      episode_id: currentEpisode ? currentEpisode.id : '',
      shot_numbers: [],
      script: script,
      style: currentProject.art_style || '',
      frame_duration: 3,
      resolution: currentProject.video_ratio === '9:16'
        ? '720x1280'
        : (getGeneralSetting('default_resolution', '1280x720')),
      fps: parseInt(getGeneralSetting('default_fps', '24'), 10) || 24,
    });
    if (sent) {
      isGenerating = true;
      setStatus('发送生成任务...');
      setTimeout(loadTasks, 500);
    }
    return Promise.resolve();
  }

  function generateShotImage(shotNumber) {
    return submitShotImagesViaWS([shotNumber], '为第 ' + shotNumber + ' 镜生成图片');
  }

  // ======================== 资产 CRUD ========================
  function loadProjectAssets(projectId) {
    if (!projectId) return Promise.resolve([]);
    const primary = '/api/projects/' + encodeURIComponent(projectId) + '/assets';
    const fallback = '/api/assets?project_id=' + encodeURIComponent(projectId);
    function fetchList(url) {
      return apiFetch(url).then(r => {
        if (r.status === 404 && url === primary) return fetchList(fallback);
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.json();
      });
    }
    return fetchList(primary)
      .then(list => {
        assets = normalizeAssetList(list);
        renderAssets();
        return assets;
      })
      .catch(err => {
        console.error('loadProjectAssets failed', err);
        assets = [];
        renderAssets();
        toast('加载资产失败，请重启服务后刷新页面', 'error');
        return [];
      });
  }

  function filteredAssets() {
    if (assetFilter === 'all') return assets;
    return assets.filter(a => a.type === assetFilter);
  }

  function setAssetFilter(filter) {
    assetFilter = filter || 'all';
    document.querySelectorAll('.asset-filter-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.filter === assetFilter);
    });
    renderAssets();
  }

  function renderAssets() {
    const listEl = document.getElementById('asset-list');
    const countEl = document.getElementById('asset-count-label');
    const shown = filteredAssets();
    if (countEl) {
      if (assets.length === 0) {
        countEl.textContent = '';
      } else if (assetFilter === 'all') {
        countEl.textContent = '共 ' + assets.length + ' 项';
      } else {
        countEl.textContent = '显示 ' + shown.length + ' / ' + assets.length + ' 项';
      }
    }
    if (!listEl) return;
    if (!assets.length) {
      listEl.innerHTML = '<div class="empty-state-sm"><p>暂无资产，点击「从剧本提取资产」或上方 ＋ 按钮添加</p></div>';
      return;
    }
    if (!shown.length) {
      const labels = { role: '角色', scene: '场景', prop: '道具' };
      listEl.innerHTML = '<div class="empty-state-sm"><p>当前分类「' + (labels[assetFilter] || assetFilter) + '」下暂无资产</p></div>';
      return;
    }
    const icons = { role: '👤', scene: '🏞️', prop: '📦' };
    listEl.innerHTML = shown.map(a => `
      <div class="asset-item" data-id="${a.id}" onclick="window._app.editAsset(${a.id})">
        <div class="asset-thumb">${a.file_url ? '<img src="' + escapeHtml(a.file_url) + '" alt="">' : (icons[a.type] || '📋')}</div>
        <div class="asset-info">
          <div class="asset-name">${escapeHtml(a.name)}</div>
          <div class="asset-type-label">${a.type} ${escapeHtml(a.desc ? '— ' + a.desc.substring(0, 30) : '')}${a.file_url ? ' · 有参考图' : ''}</div>
        </div>
        <div class="asset-actions">
          <button class="btn btn-sm btn-outline" title="编辑" onclick="event.stopPropagation(); window._app.editAsset(${a.id})">✎</button>
          <button class="btn btn-sm btn-outline" title="删除" onclick="event.stopPropagation(); window._app.deleteAsset(${a.id})">×</button>
        </div>
      </div>
    `).join('');
  }

  window._app = {
    deleteSource: function(id) {
      if (!currentProject) return;
      apiFetch('/api/projects/' + currentProject.id + '/source-texts/' + id, { method: 'DELETE' })
        .then(() => { loadSourceTexts(); toast('已删除', 'info'); })
        .catch(() => toast('删除失败', 'error'));
    },
    deleteAsset: function(id) {
      apiFetch('/api/assets/' + id, { method: 'DELETE' }).then(() => {
        assets = assets.filter(a => a.id !== id);
        renderAssets();
        toast('资产已删除', 'info');
      }).catch(() => toast('删除失败', 'error'));
    },
    editAsset: function(id) {
      const a = assets.find(x => x.id === id);
      if (!a) return;
      openAssetModal(a.type, a);
    },
    deleteVendor: function(id) {
      apiFetch('/api/vendors/' + id, { method: 'DELETE' }).then(() => {
        loadVendors();
        toast('供应商已删除', 'info');
      }).catch(() => toast('删除失败', 'error'));
    },
    editVendor: function(id, name, url) {
      openVendorModal('edit', { id, name, url });
    },
    toggleVendor: function(el, id) {
      el.classList.toggle('on');
      const enabled = el.classList.contains('on');
      apiFetch('/api/vendors/' + id, {
        method: 'PUT',
        body: JSON.stringify({ enable: enabled ? 1 : 0 }),
      }).catch(() => toast('切换状态失败', 'error'));
    },
    generateShotVideo: generateShotVideo,
    selectShotClip: selectShotClip,
    deleteShotClip: deleteShotClip,
  };

  // ======================== 画风列表 ========================
  function loadStyles() {
    const styleList = document.getElementById('style-list');
    if (!styleList) return;
    apiFetch('/api/styles').then(r => r.json()).then(list => {
      const icons = ['🎭', '🖌️', '📐', '💕', '🎪', '🏯', '🧸', '🌃', '👘', '🏙️', '🌆'];
      els.styleList = styleList;
      styleList.innerHTML = (list || []).map((s, i) => `
        <div class="style-item ${currentProject && currentProject.art_style === s.name ? 'selected' : ''}" data-name="${escapeHtml(s.name)}">
          <span class="style-icon">${icons[i % icons.length]}</span>
          <span class="style-name">${escapeHtml(s.label || s.name)}</span>
        </div>
      `).join('');

      els.styleList.querySelectorAll('.style-item').forEach(item => {
        item.addEventListener('click', () => {
          els.styleList.querySelectorAll('.style-item').forEach(s => s.classList.remove('selected'));
          item.classList.add('selected');
          if (currentProject) {
            currentProject.art_style = item.dataset.name;
            toast('已选择画风: ' + item.dataset.name, 'info');
          }
        });
      });
    }).catch(() => {});
  }

  // ======================== 分镜渲染 ========================
  function renderStoryboards() {
    if (!storyboards || storyboards.length === 0) {
      els.storyboardList.innerHTML = '';
      els.storyboardEmpty.style.display = 'block';
      els.storyboardCount.textContent = '0 个分镜';
      updateStoryboardSelectionUI();
      return;
    }
    els.storyboardEmpty.style.display = 'none';
    els.storyboardCount.textContent = storyboards.length + ' 个分镜';

    els.storyboardList.innerHTML = storyboards.map((sb, i) => `
      <div class="storyboard-card ${sb.selected === true ? 'is-selected' : ''}" data-index="${i}">
        <div class="storyboard-card-header">
          <label class="storyboard-card-select">
            <input type="checkbox" class="sb-select-cb" data-index="${i}" ${sb.selected === true ? 'checked' : ''}>
            <span class="storyboard-card-title">🎬 第 ${sb.shot_number || i + 1} 镜 — ${escapeHtml(sb.scene || '未命名场景')}</span>
          </label>
          <span class="storyboard-card-duration">${sb.duration || 3}s</span>
        </div>
        <div class="storyboard-card-body">
          <div class="storyboard-card-content">
            <div class="storyboard-field">
              <div class="storyboard-field-label">画面描述</div>
              <div class="storyboard-field-value">
                <textarea rows="2">${escapeHtml(sb.description || '')}</textarea>
              </div>
            </div>
            <div class="storyboard-field">
              <div class="storyboard-field-label">运镜</div>
              <div class="storyboard-field-value">${escapeHtml(sb.camera || '固定镜头')}</div>
            </div>
            <div class="storyboard-field">
              <div class="storyboard-field-label">AI 绘图 Prompt</div>
              <div class="storyboard-field-value">
                <textarea rows="3">${escapeHtml(sb.prompt || '')}</textarea>
              </div>
            </div>
          </div>
          ${renderStoryboardImageColumn(sb, i)}
          ${renderStoryboardVideoColumn(sb, i)}
        </div>
      </div>
    `).join('');
    els.storyboardList.querySelectorAll('.sb-gen-image-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        generateShotImage(parseInt(btn.dataset.shot, 10));
      });
    });
    els.storyboardList.querySelectorAll('.sb-gen-video-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        generateShotVideo(parseInt(btn.dataset.shot, 10));
      });
    });
    if (clipVersionDropdownShot != null && !clipsForShot(clipVersionDropdownShot).length) {
      clipVersionDropdownShot = null;
    }
    syncClipVersionDropdownUI();
    updateStoryboardSelectionUI();
  }

  // ======================== 视频轨道 ========================
  function showVideoResult(url) {
    if (els.videoExportArea) els.videoExportArea.style.display = 'block';
    if (els.outputVideo) els.outputVideo.src = url;
    if (els.downloadLink) { els.downloadLink.href = url; els.downloadLink.download = 'ai-video_export.mp4'; }
  }

  // ======================== 任务列表 ========================
  function loadTasks() {
    apiFetch('/api/tasks').then(r => r.json()).then(list => {
      renderTasks(list || []);
    }).catch(() => {});
  }

  function taskModeLabel(mode) {
    const map = { images: '生成图片', video: '生成视频', full: '一键出片', parse: '解析剧本' };
    return map[mode] || mode || '';
  }

  function formatShotsLabel(shots) {
    if (!shots || !shots.length) return '全部分镜';
    if (shots.length === 1) return '第' + shots[0] + '镜';
    return shots.map(n => '第' + n + '镜').join('、');
  }

  function renderTasks(tasks) {
    if (!tasks || tasks.length === 0) {
      els.taskList.innerHTML = '<div class="empty-state"><div class="empty-icon">📋</div><p>暂无任务</p></div>';
      els.taskStats.textContent = '';
      if (els.activeTasks) els.activeTasks.textContent = '';
      return;
    }

    const counts = { waiting: 0, parsing: 0, storyboarding: 0, drawing: 0, video_gen: 0, merging: 0, done: 0, error: 0 };
    tasks.forEach(t => { if (counts[t.state] !== undefined) counts[t.state]++; });
    els.taskStats.textContent = `等待:${counts.waiting} 绘图:${counts.drawing} 视频:${counts.video_gen} 完成:${counts.done} 失败:${counts.error}`;

    const active = tasks.filter(t => t.state !== 'done' && t.state !== 'error');
    if (els.activeTasks) {
      els.activeTasks.textContent = active.length ? `${active.length} 个进行中` : '';
    }

    els.taskList.innerHTML = tasks.map(t => {
      const title = t.title || taskModeLabel(t.mode) || t.step || t.id;
      const epNote = t.episode_title
        ? `第${t.episode_num || '?'}集 ${t.episode_title}`
        : (t.episode_num ? `第${t.episode_num}集` : '');
      const note = [t.project_name, epNote, formatShotsLabel(t.generate_shots)].filter(Boolean).join(' · ');
      return `
      <div class="task-item">
        <span class="task-state-badge task-state-${t.state}">${stateLabel(t.state)}</span>
        <div class="task-info">
          <div class="task-title">${escapeHtml(title)}</div>
          <div class="task-step">${escapeHtml(note)}${t.error_message ? ' · ' + escapeHtml(t.error_message) : ''}</div>
        </div>
        <div class="task-progress-bar">
          <div class="task-progress-fill" style="width:${(t.progress || 0)}%"></div>
        </div>
        <span class="task-time">${t.created_at ? new Date(t.created_at).toLocaleTimeString() : ''}</span>
      </div>`;
    }).join('');
  }

  function stateLabel(s) {
    const map = {
      waiting: '等待中', parsing: '解析中', storyboarding: '分镜中',
      drawing: '绘图中', video_gen: '生成视频', merging: '合成中',
      done: '已完成', error: '失败',
    };
    return map[s] || s;
  }

  // ======================== 供应商管理 ========================
  let editingVendorId = null;

  function isLikelyAPIURL(s) {
    return /^https?:\/\//i.test((s || '').trim());
  }

  function loadVendorActiveStatus() {
    const el = document.getElementById('vendor-active-status');
    if (!el) return;
    apiFetch('/api/vendors/active').then(r => r.json()).then(info => {
      if (!info.configured) {
        el.innerHTML = '<div class="vendor-status-warn">⚠ 未配置有效的 API Key，请添加或编辑供应商</div>';
        return;
      }
      el.innerHTML = `<div class="vendor-status-ok">当前凭证：${escapeHtml(info.key_hint || '****')} · ${escapeHtml(info.base_url || '')} · 来源：${escapeHtml(info.source || '')}</div>`;
    }).catch(() => {});
  }

  function loadVendors() {
    loadVendorActiveStatus();
    apiFetch('/api/vendors').then(r => r.json()).then(list => {
      els.vendorList.innerHTML = (list || []).map(v => `
        <div class="vendor-card">
          <div class="vendor-card-info">
            <div class="vendor-card-name">${escapeHtml(v.name || v.id)}</div>
            <div class="vendor-card-version">${escapeHtml(v.url || '')}</div>
            <div class="vendor-card-models">${v.enable ? '已启用' : '已禁用'}</div>
          </div>
          <div class="vendor-card-actions">
            <button class="btn btn-sm btn-outline" onclick="window._app.editVendor('${v.id}', decodeURIComponent('${encodeURIComponent(v.name || '')}'), decodeURIComponent('${encodeURIComponent(v.url || '')}'))">编辑</button>
            <button class="btn btn-sm btn-outline" onclick="window._app.deleteVendor('${v.id}')">删除</button>
          </div>
          <div class="toggle-switch ${v.enable ? 'on' : ''}" onclick="window._app.toggleVendor(this, '${v.id}')"></div>
        </div>
      `).join('');
    }).catch(() => {});
  }

  function openVendorModal(mode, vendor) {
    editingVendorId = mode === 'edit' ? vendor.id : null;
    document.getElementById('vendor-modal-title').textContent = mode === 'edit' ? '编辑供应商' : '添加供应商';
    document.getElementById('vendor-name').value = vendor?.name || 'Agnes-AI';
    document.getElementById('vendor-url').value = vendor?.url || 'https://apihub.agnes-ai.com/v1';
    document.getElementById('vendor-key').value = '';
    document.getElementById('vendor-key').placeholder = mode === 'edit' ? '留空则不修改 Key' : '从 platform.agnes-ai.com 获取，不是 API 地址';
    els.modalVendor.style.display = 'flex';
  }

  // ======================== 供应商弹窗事件 ========================
  document.getElementById('btn-add-vendor').addEventListener('click', () => {
    openVendorModal('add');
  });

  document.getElementById('btn-close-vendor-modal').addEventListener('click', () => els.modalVendor.style.display = 'none');
  document.getElementById('btn-cancel-vendor').addEventListener('click', () => els.modalVendor.style.display = 'none');
  els.modalVendor.addEventListener('click', (e) => { if (e.target === els.modalVendor) els.modalVendor.style.display = 'none'; });

  document.getElementById('btn-save-vendor').addEventListener('click', () => {
    const name = document.getElementById('vendor-name').value.trim();
    const url = document.getElementById('vendor-url').value.trim();
    const key = document.getElementById('vendor-key').value.trim();
    if (!name) { toast('请输入供应商名称', 'warning'); return; }
    if (!url) { toast('请输入API地址', 'warning'); return; }
    if (!editingVendorId && !key) { toast('请输入API Key', 'warning'); return; }
    if (key && isLikelyAPIURL(key)) {
      toast('API Key 不能是 URL，请填写 Agnes 控制台的密钥', 'warning');
      return;
    }
    if (key && key === url) {
      toast('API Key 与 API 地址相同，请检查是否填错字段', 'warning');
      return;
    }

    const body = { name, url };
    if (key) body.api_key = key;

    const req = editingVendorId
      ? apiFetch('/api/vendors/' + editingVendorId, { method: 'PATCH', body: JSON.stringify(body) })
      : apiFetch('/api/vendors', { method: 'POST', body: JSON.stringify({ name, url, api_key: key }) });

    req.then(async (r) => {
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error(err.error || '保存失败');
      }
      els.modalVendor.style.display = 'none';
      editingVendorId = null;
      loadVendors();
      toast('供应商已保存', 'success');
    }).catch((e) => toast(e.message || '保存失败', 'error'));
  });

  // ======================== 事件绑定 ========================
  // 新建项目
  document.getElementById('btn-new-project').addEventListener('click', () => {
    openProjectModal(null);
  });

  document.getElementById('btn-close-modal').addEventListener('click', closeModal);
  document.getElementById('btn-cancel-project').addEventListener('click', closeModal);
  els.modalNewProject.addEventListener('click', (e) => { if (e.target === els.modalNewProject) closeModal(); });

  function closeModal() {
    editingProjectId = null;
    els.modalNewProject.style.display = 'none';
  }

  const GENRE_LABELS = {
    comedy: '喜剧', drama: '剧情', romance: '爱情 / 甜宠', comedy_romance: '轻喜剧爱情',
    urban: '都市', workplace: '职场', campus: '校园 / 青春', family: '家庭 / 伦理',
    historical: '古装 / 历史', wuxia: '武侠', xianxia: '仙侠 / 玄幻',
    transmigration: '穿越 / 重生', revenge: '复仇 / 爽文', mystery: '悬疑', thriller: '惊悚',
    crime: '犯罪', horror: '恐怖', scifi: '科幻', fantasy: '奇幻', action: '动作',
    war: '战争', sports: '体育 / 竞技', slice_of_life: '日常 / 治愈', musical: '音乐 / 歌舞',
    documentary: '纪实', inspirational: '励志', female_lead: '大女主', male_lead: '大男主',
    palace: '宫斗', republic_era: '民国',
  };

  function setTypeFields(value) {
    const sel = document.getElementById('proj-type');
    const custom = document.getElementById('proj-type-custom');
    if (!sel || !custom) return;
    const opts = [...sel.options].map(o => o.value).filter(v => v && v !== '__custom__');
    if (value && !opts.includes(value)) {
      sel.value = '__custom__';
      custom.value = value;
    } else {
      sel.value = value || '';
      custom.value = '';
    }
  }

  function readTypeValue() {
    const custom = document.getElementById('proj-type-custom');
    const customVal = custom ? custom.value.trim() : '';
    if (customVal) return customVal;
    const sel = document.getElementById('proj-type');
    if (!sel || sel.value === '__custom__') return '';
    return sel.value;
  }

  function formatProjectType(type) {
    if (!type) return '未设置';
    return GENRE_LABELS[type] || type;
  }

  function setArtStyleFields(value) {
    const sel = document.getElementById('proj-artstyle');
    const custom = document.getElementById('proj-artstyle-custom');
    if (!sel || !custom) return;
    const opts = [...sel.options].map(o => o.value).filter(v => v && v !== '__custom__');
    if (value && !opts.includes(value)) {
      sel.value = '__custom__';
      custom.value = value;
    } else {
      sel.value = value || '';
      custom.value = '';
    }
  }

  function readArtStyleValue() {
    const custom = document.getElementById('proj-artstyle-custom');
    const customVal = custom ? custom.value.trim() : '';
    if (customVal) return customVal;
    const sel = document.getElementById('proj-artstyle');
    if (!sel || sel.value === '__custom__') return '';
    return sel.value;
  }

  function openProjectModal(projectId) {
    editingProjectId = projectId;
    document.getElementById('project-modal-title').textContent = projectId ? '编辑项目' : '新建项目';
    document.getElementById('btn-save-project').textContent = projectId ? '保存修改' : '创建项目';
    loadStylesForSelect().then(() => {
      if (!projectId) {
        document.getElementById('proj-name').value = '';
        document.getElementById('proj-intro').value = '';
        setTypeFields('');
        setArtStyleFields('');
        document.getElementById('proj-ratio').value = '16:9';
        document.getElementById('proj-image-model').value = '';
        return;
      }
      return apiFetch('/api/projects/' + projectId).then(r => r.json()).then(proj => {
        document.getElementById('proj-name').value = proj.name || '';
        document.getElementById('proj-intro').value = proj.intro || '';
        setTypeFields(proj.type || '');
        setArtStyleFields(proj.art_style || '');
        document.getElementById('proj-ratio').value = proj.video_ratio || '16:9';
        document.getElementById('proj-image-model').value = proj.image_model || '';
      });
    });
    els.modalNewProject.style.display = 'flex';
  }

  function loadStylesForSelect() {
    return apiFetch('/api/styles').then(r => r.json()).then(list => {
      const sel = document.getElementById('proj-artstyle');
      const customVal = document.getElementById('proj-artstyle-custom')?.value || '';
      const selected = sel.value;
      const preset = (list || []).map(s =>
        `<option value="${escapeHtml(s.name)}">${escapeHtml(s.label || s.name)}</option>`
      ).join('');
      sel.innerHTML =
        '<option value="">默认画风</option>' +
        preset +
        '<option value="__custom__">自定义…</option>';
      setArtStyleFields(customVal || (selected && selected !== '__custom__' ? selected : ''));
    }).catch(() => {
      const sel = document.getElementById('proj-artstyle');
      if (sel && sel.options.length <= 2) {
        sel.innerHTML = '<option value="">默认画风</option><option value="__custom__">自定义…</option>';
      }
    });
  }

  document.getElementById('proj-type').addEventListener('change', () => {
    const sel = document.getElementById('proj-type');
    const custom = document.getElementById('proj-type-custom');
    if (sel?.value !== '__custom__' && custom) custom.value = '';
    if (sel?.value === '__custom__' && custom) custom.focus();
  });
  document.getElementById('proj-artstyle').addEventListener('change', () => {
    const sel = document.getElementById('proj-artstyle');
    const custom = document.getElementById('proj-artstyle-custom');
    if (sel?.value !== '__custom__' && custom) custom.value = '';
  });

  document.getElementById('btn-save-project').addEventListener('click', () => {
    const name = document.getElementById('proj-name').value.trim();
    if (!name) { toast('请输入项目名称', 'warning'); return; }

    const typeVal = readTypeValue();
    const artStyleVal = readArtStyleValue();
    if (document.getElementById('proj-type').value === '__custom__' && !typeVal) {
      toast('请输入自定义题材类型', 'warning');
      return;
    }
    if (document.getElementById('proj-artstyle').value === '__custom__' && !artStyleVal) {
      toast('请输入自定义画风', 'warning');
      return;
    }

    const data = {
      name: name,
      intro: document.getElementById('proj-intro').value,
      type: typeVal,
      art_style: artStyleVal,
      video_ratio: document.getElementById('proj-ratio').value,
      image_model: document.getElementById('proj-image-model').value,
      status: 'draft',
    };

    const isEdit = !!editingProjectId;
    const req = isEdit
      ? apiFetch('/api/projects/' + editingProjectId, { method: 'PUT', body: JSON.stringify(data) })
      : apiFetch('/api/projects', { method: 'POST', body: JSON.stringify(data) });

    req.then(r => r.json()).then(proj => {
      if (!isEdit) {
        currentProject = proj;
        document.getElementById('wb-project-name').textContent = proj.name || name;
        showPage('production');
        loadWorkbench();
      } else if (currentProject && currentProject.id === editingProjectId) {
        currentProject = Object.assign({}, currentProject, data, { id: editingProjectId });
        els.projectName.textContent = data.name;
        els.projectDisplay.textContent = '当前项目: ' + data.name;
      }
      closeModal();
      loadProjects();
      toast(isEdit ? '项目已更新' : '项目 "' + name + '" 已创建', 'success');
    }).catch(() => toast(isEdit ? '更新失败' : '创建项目失败', 'error'));
  });

  // 添加资产
  document.getElementById('btn-add-role').addEventListener('click', () => openAssetModal('role'));
  document.getElementById('btn-add-scene').addEventListener('click', () => openAssetModal('scene'));
  document.getElementById('btn-add-prop').addEventListener('click', () => openAssetModal('prop'));

  document.querySelectorAll('.asset-filter-btn').forEach(btn => {
    btn.addEventListener('click', () => setAssetFilter(btn.dataset.filter));
  });

  function openAssetModal(type, asset) {
    editingAssetId = asset ? asset.id : null;
    const typeLabel = type === 'role' ? '角色' : type === 'scene' ? '场景' : '道具';
    document.getElementById('asset-modal-title').textContent = (asset ? '编辑' : '添加') + typeLabel;
    document.getElementById('asset-type').value = type;
    document.getElementById('asset-name').value = asset ? (asset.name || '') : '';
    document.getElementById('asset-desc').value = asset ? (asset.desc || '') : '';
    document.getElementById('asset-file-url').value = asset ? (asset.file_url || '') : '';
    els.modalAsset.style.display = 'flex';
  }

  function closeAssetModal() {
    editingAssetId = null;
    els.modalAsset.style.display = 'none';
  }

  document.getElementById('btn-close-asset-modal').addEventListener('click', closeAssetModal);
  document.getElementById('btn-cancel-asset').addEventListener('click', closeAssetModal);

  document.getElementById('btn-save-asset').addEventListener('click', () => {
    const data = {
      project_id: currentProject ? currentProject.id : '',
      name: document.getElementById('asset-name').value.trim(),
      desc: document.getElementById('asset-desc').value,
      type: document.getElementById('asset-type').value,
      file_url: document.getElementById('asset-file-url').value.trim(),
    };
    if (!data.name) { toast('请输入名称', 'warning'); return; }

    const isEdit = editingAssetId != null;
    const req = isEdit
      ? apiFetch('/api/assets/' + editingAssetId, { method: 'PUT', body: JSON.stringify(data) })
      : apiFetch('/api/assets', { method: 'POST', body: JSON.stringify(data) });

    req.then(() => {
      closeAssetModal();
      if (currentProject) loadProjectAssets(currentProject.id);
      toast(isEdit ? '资产已更新' : '资产已添加', 'success');
    }).catch(() => toast(isEdit ? '更新失败' : '添加失败', 'error'));
  });

  // 工作台事件
  document.getElementById('wb-episode-select').addEventListener('change', (e) => {
    currentEpisode = episodes.find(ep => ep.id === e.target.value) || null;
    renderEpisodeList();
    loadPlanningContent();
    loadChatMessages();
    if (currentProject) {
      loadProjectStoryboards(currentProject.id, currentEpisode?.id);
      loadShotClips();
      if (wbStage === 'video') loadTimeline();
    }
  });

  document.getElementById('btn-wb-chat-send').addEventListener('click', () => {
    const input = document.getElementById('wb-chat-input');
    const msg = input.value.trim();
    if (!msg) return;
    input.value = '';
    sendChat(msg).catch(() => toast('发送失败', 'error'));
  });

  document.getElementById('wb-chat-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      document.getElementById('btn-wb-chat-send').click();
    }
  });

  document.getElementById('btn-import-source').addEventListener('click', () => {
    document.getElementById('source-volume').value = '正文卷';
    document.getElementById('source-chapter').value = '';
    document.getElementById('source-content').value = '';
    document.getElementById('modal-source').style.display = 'flex';
  });

  document.getElementById('btn-close-source-modal').addEventListener('click', () => {
    document.getElementById('modal-source').style.display = 'none';
  });
  document.getElementById('btn-cancel-source').addEventListener('click', () => {
    document.getElementById('modal-source').style.display = 'none';
  });

  document.getElementById('btn-save-source').addEventListener('click', () => {
    if (!currentProject) return;
    const content = document.getElementById('source-content').value.trim();
    if (!content) { toast('请输入原文内容', 'warning'); return; }
    apiFetch('/api/projects/' + currentProject.id + '/source-texts', {
      method: 'POST',
      body: JSON.stringify({
        volume: document.getElementById('source-volume').value.trim(),
        chapter_name: document.getElementById('source-chapter').value.trim(),
        content: content,
      }),
    }).then(() => {
      document.getElementById('modal-source').style.display = 'none';
      loadSourceTexts();
      toast('原文已导入', 'success');
    }).catch(() => toast('导入失败', 'error'));
  });

  document.getElementById('btn-analyze-events').addEventListener('click', () => {
    if (!sourceTexts.length) {
      toast('请先导入原文', 'warning');
      return;
    }
    runWorkflowViaWS(
      'analyze_events',
      '事件分析',
      document.getElementById('btn-analyze-events'),
      '分析中'
    );
  });

  document.getElementById('btn-split-episodes').addEventListener('click', () => {
    if (!sourceTexts.length) {
      toast('请先导入原文', 'warning');
      return;
    }
    runWorkflowViaWS(
      'split_episodes',
      'AI 分集',
      document.getElementById('btn-split-episodes'),
      '分集中'
    );
  });

  document.querySelectorAll('[data-quick]').forEach(btn => {
    btn.addEventListener('click', () => {
      const action = btn.dataset.quick;
      if (!currentEpisode) { toast('请先选择一集', 'warning'); return; }
      if (workflowUserLabels[action]) {
        runWorkflowViaWS(
          action,
          workflowUserLabels[action],
          btn,
          workflowLoadingLabels[action] || '处理中'
        );
        return;
      }
    });
  });

  document.getElementById('btn-extract-assets').addEventListener('click', () => {
    if (!currentEpisode) { toast('请先选择一集', 'warning'); return; }
    runWorkflowViaWS(
      'extract_assets',
      '从剧本提取资产',
      document.getElementById('btn-extract-assets'),
      '提取中'
    );
  });

  document.getElementById('btn-gen-storyboard').addEventListener('click', () => {
    if (!currentEpisode) { toast('请先选择一集', 'warning'); return; }
    runWorkflowViaWS(
      'generate_storyboard',
      '生成分镜',
      document.getElementById('btn-gen-storyboard'),
      '生成中'
    );
  });

  document.getElementById('btn-gen-images').addEventListener('click', () => {
    if (!currentEpisode) { toast('请先选择一集', 'warning'); return; }
    const shots = getSelectedShotNumbers();
    if (!shots.length) {
      toast('请勾选分镜，或点击卡片上的「🎨 生成图片」', 'warning');
      return;
    }
    submitShotImagesViaWS(shots, '批量生成图片（' + shots.length + ' 镜）');
  });

  if (els.btnSelectAllShots) {
    els.btnSelectAllShots.addEventListener('click', () => {
      const allSelected = storyboards.length > 0 && countSelectedStoryboards() === storyboards.length;
      setAllStoryboardsSelected(!allSelected);
    });
  }

  if (els.storyboardList) {
    els.storyboardList.addEventListener('change', (e) => {
      if (!e.target.classList.contains('sb-select-cb')) return;
      const idx = parseInt(e.target.dataset.index, 10);
      if (Number.isNaN(idx) || !storyboards[idx]) return;
      storyboards[idx].selected = e.target.checked;
      const card = e.target.closest('.storyboard-card');
      if (card) card.classList.toggle('is-selected', e.target.checked);
      updateStoryboardSelectionUI();
    });
    els.storyboardList.addEventListener('click', (e) => {
      const toggle = e.target.closest('.sb-version-toggle');
      if (toggle) {
        e.stopPropagation();
        if (toggle.disabled) return;
        toggleClipVersionDropdown(parseInt(toggle.dataset.shot, 10));
        return;
      }
      const delBtn = e.target.closest('.clip-version-del-btn');
      if (delBtn) {
        e.preventDefault();
        e.stopPropagation();
        deleteShotClip(delBtn.dataset.clipId);
        return;
      }
      const chip = e.target.closest('.clip-version-chip');
      if (chip) {
        e.preventDefault();
        e.stopPropagation();
        selectShotClip(chip.dataset.clipId);
        return;
      }
      const versionPreview = e.target.closest('.clip-version-preview-btn');
      if (versionPreview) {
        e.preventDefault();
        e.stopPropagation();
        openMediaPreview('video', versionPreview.dataset.url, versionPreview.dataset.title);
        return;
      }
      const btn = e.target.closest('.sb-media-preview-btn');
      if (!btn) return;
      e.stopPropagation();
      openMediaPreview(btn.dataset.previewType, btn.dataset.url, btn.dataset.title);
    });
    els.storyboardList.addEventListener('mousedown', (e) => {
      if (e.target.closest('.sb-version-menu') || e.target.closest('.sb-version-toggle')) {
        e.stopPropagation();
      }
    });
  }

  document.addEventListener('click', (e) => {
    if (clipVersionDropdownShot == null) return;
    if (e.target.closest('.sb-version-dropdown')) return;
    closeClipVersionDropdown();
  }, false);

  document.getElementById('btn-close-media-preview')?.addEventListener('click', closeMediaPreview);
  els.modalMediaPreview?.addEventListener('click', (e) => {
    if (e.target === els.modalMediaPreview) closeMediaPreview();
  });
  els.mediaPreviewPanel?.addEventListener('click', (e) => e.stopPropagation());

  (function initMediaPreviewResize() {
    const panel = els.mediaPreviewPanel;
    const grip = els.mediaPreviewResizeGrip;
    if (!panel || !grip) return;

    let startX = 0;
    let startY = 0;
    let startW = 0;
    let startH = 0;

    grip.addEventListener('mousedown', (e) => {
      e.preventDefault();
      e.stopPropagation();
      startX = e.clientX;
      startY = e.clientY;
      startW = panel.offsetWidth;
      startH = panel.offsetHeight;
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'nwse-resize';

      function onMove(ev) {
        const maxW = window.innerWidth * 0.96;
        const maxH = window.innerHeight * 0.92;
        const w = Math.min(maxW, Math.max(320, startW + ev.clientX - startX));
        const h = Math.min(maxH, Math.max(220, startH + ev.clientY - startY));
        panel.style.width = w + 'px';
        panel.style.height = h + 'px';
      }

      function onUp() {
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      }

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
    });
  })();

  document.getElementById('btn-batch-gen-video')?.addEventListener('click', () => {
    batchGenerateShotVideos();
  });

  document.getElementById('btn-timeline-reload')?.addEventListener('click', reloadTimelineFromSelected);
  document.getElementById('btn-timeline-save')?.addEventListener('click', saveTimeline);
  document.getElementById('btn-timeline-export')?.addEventListener('click', exportTimeline);
  document.getElementById('btn-timeline-add-audio')?.addEventListener('click', addTimelineAudio);
  document.getElementById('btn-narration-plan')?.addEventListener('click', planNarration);
  document.getElementById('btn-narration-synthesize')?.addEventListener('click', synthesizeNarration);

  // 设置加载与保存（浏览器 localStorage 优先，服务端为备份）
  function readGeneralSettings() {
    try {
      return JSON.parse(localStorage.getItem(GENERAL_SETTINGS_KEY) || '{}');
    } catch (_) {
      return {};
    }
  }

  function writeGeneralSettings(data) {
    localStorage.setItem(GENERAL_SETTINGS_KEY, JSON.stringify(data));
  }

  function collectGeneralSettings() {
    return {
      output_dir: document.getElementById('set-output-dir').value,
      default_fps: document.getElementById('set-fps').value,
      default_resolution: document.getElementById('set-resolution').value,
      max_concurrent_tasks: document.getElementById('set-max-tasks').value,
      ffmpeg_path: document.getElementById('set-ffmpeg').value,
    };
  }

  function applyGeneralSettings(s) {
    if (!s) return;
    if (s.output_dir != null) document.getElementById('set-output-dir').value = s.output_dir;
    if (s.default_fps != null) document.getElementById('set-fps').value = s.default_fps;
    if (s.default_resolution != null) document.getElementById('set-resolution').value = s.default_resolution;
    if (s.max_concurrent_tasks != null) document.getElementById('set-max-tasks').value = s.max_concurrent_tasks;
    if (s.ffmpeg_path != null) document.getElementById('set-ffmpeg').value = s.ffmpeg_path;
  }

  function loadSettings() {
    const local = readGeneralSettings();
    if (Object.keys(local).length) {
      applyGeneralSettings(local);
    }
    apiFetch('/api/settings').then(r => r.json()).then(s => {
      // 本地无缓存时用服务端默认值填充
      if (!local.output_dir && s.output_dir) document.getElementById('set-output-dir').value = s.output_dir;
      if (!local.default_fps && s.default_fps) document.getElementById('set-fps').value = s.default_fps;
      if (!local.default_resolution && s.default_resolution) document.getElementById('set-resolution').value = s.default_resolution;
      if (!local.max_concurrent_tasks && s.max_concurrent_tasks) document.getElementById('set-max-tasks').value = s.max_concurrent_tasks;
      if (!local.ffmpeg_path && s.ffmpeg_path) document.getElementById('set-ffmpeg').value = s.ffmpeg_path;
      // 首次无本地缓存时，把服务端配置写入 localStorage
      if (!Object.keys(local).length) {
        writeGeneralSettings(collectGeneralSettings());
      }
    }).catch(() => {});
  }

  function saveGeneralSettings(showToast) {
    const data = collectGeneralSettings();
    writeGeneralSettings(data);
    return apiFetch('/api/settings', {
      method: 'PUT',
      body: JSON.stringify(data),
    }).then(() => {
      if (showToast) toast('设置已保存', 'success');
    }).catch(() => {
      if (showToast) toast('已保存到浏览器，同步服务端失败', 'warning');
    });
  }

  document.getElementById('btn-save-settings').addEventListener('click', () => {
    saveGeneralSettings(true);
  });

  // 修改常规设置时自动写入 localStorage
  ['set-output-dir', 'set-fps', 'set-resolution', 'set-max-tasks', 'set-ffmpeg'].forEach(id => {
    const el = document.getElementById(id);
    if (!el) return;
    el.addEventListener('change', () => writeGeneralSettings(collectGeneralSettings()));
    if (el.tagName === 'INPUT' && el.type === 'text') {
      el.addEventListener('blur', () => writeGeneralSettings(collectGeneralSettings()));
    }
  });

  function getGeneralSetting(key, fallback) {
    const s = readGeneralSettings();
    return s[key] != null && s[key] !== '' ? s[key] : fallback;
  }

  // 模型测试
  function runModelTest(type, resultEl) {
    resultEl.textContent = '测试中...';
    apiFetch('/api/models/test/' + type, { method: 'POST', body: '{}' })
      .then(r => r.json())
      .then(res => {
        if (!res.ok) {
          const msg = '失败: ' + (res.error || '未知错误') + (res.hint ? '\n' + res.hint : '');
          resultEl.textContent = msg;
          toast(type + ' 模型测试失败', 'error');
          return;
        }
        if (type === 'text') {
          resultEl.textContent = '成功: ' + (res.content || '');
        } else if (type === 'image' && res.data_url) {
          resultEl.innerHTML = '成功<br><img src="' + escapeHtml(res.data_url) + '" alt="test">';
        } else if (type === 'video') {
          resultEl.textContent = '成功: 视频已生成';
          if (res.video_url) {
            if (res.video_url.startsWith('data:')) {
              resultEl.innerHTML = '成功<br><video src="' + escapeHtml(res.video_url) + '" controls style="max-width:240px;margin-top:8px;"></video>';
            } else {
              resultEl.innerHTML = '成功<br><a href="' + escapeHtml(res.video_url) + '" target="_blank">查看视频</a>';
            }
          }
        } else {
          resultEl.textContent = '成功';
        }
        toast(type + ' 模型测试通过', 'success');
      })
      .catch(() => {
        resultEl.textContent = '请求失败';
        toast(type + ' 模型测试失败', 'error');
      });
  }

  document.getElementById('btn-test-text').addEventListener('click', () => {
    runModelTest('text', document.getElementById('test-text-result'));
  });
  document.getElementById('btn-test-image').addEventListener('click', () => {
    runModelTest('image', document.getElementById('test-image-result'));
  });
  document.getElementById('btn-test-video').addEventListener('click', () => {
    runModelTest('video', document.getElementById('test-video-result'));
  });

  // ======================== 工具函数 ========================
  function setStatus(text) {
    els.statusLeft.textContent = text;
  }

  function toast(msg, type) {
    const container = document.querySelector('.toast-container') || createToastContainer();
    const t = document.createElement('div');
    t.className = 'toast toast-' + (type || 'info');
    t.textContent = msg;
    container.appendChild(t);
    setTimeout(() => t.remove(), 3000);
  }

  function createToastContainer() {
    const c = document.createElement('div');
    c.className = 'toast-container';
    document.body.appendChild(c);
    return c;
  }

  function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function bootApp() {
    connectWS();
    loadProjects();
    loadStyles();
    loadVendors();
    loadTasks();
    loadSettings();
  }

  // ======================== 初始化 ========================
  function init() {
    checkSession();

    // 定时刷新任务列表（当前用户）
    setInterval(() => {
      if (authToken) loadTasks();
    }, 4000);

    // 定时刷新项目列表
    setInterval(() => {
      if (document.getElementById('page-projects').classList.contains('active')) {
        loadProjects();
      }
    }, 10000);
  }

  init();
})();
