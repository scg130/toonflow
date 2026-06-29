/**
 * ToonFlow — 短剧 AI 创作平台
 * 对标 ToonFlow 产品逻辑：项目制工作流、资产管理、分镜编辑、视频轨道
 */
(function () {
  'use strict';

  // ======================== 状态 ========================
  let authToken = localStorage.getItem('toonflow_token') || '';
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
  let storyboards = [];
  let assets = [];           // 当前项目的资产列表
  let videoTracks = [];      // 视频轨道
  let isGenerating = false;

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
    // 视频
    videoTracks: document.getElementById('video-tracks'),
    videoPreview: document.getElementById('video-preview-area'),
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
    // 弹窗
    modalNewProject: document.getElementById('modal-new-project'),
    modalAsset: document.getElementById('modal-asset'),
    modalVendor: document.getElementById('modal-vendor'),
    loginOverlay: document.getElementById('login-overlay'),
    userBadge: document.getElementById('user-badge'),
    btnLogout: document.getElementById('btn-logout'),
  };

  // ======================== 鉴权 ========================
  function unwrapApiBody(body) {
    if (!body || typeof body !== 'object' || Array.isArray(body)) return body;
    if (body.log_id && 'data' in body && Object.keys(body).length === 2) {
      return body.data;
    }
    return body;
  }

  function apiFetch(url, options) {
    options = options || {};
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
    localStorage.removeItem('toonflow_token');
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
      localStorage.setItem('toonflow_token', authToken);
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
      document.querySelectorAll('.planning-tab').forEach(t => t.classList.remove('active'));
      tab.classList.add('active');
      planningType = tab.dataset.plan;
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
    if (msg.step === 'chat_progress') {
      if (msg.data && msg.data.project_id && currentProject && msg.data.project_id !== currentProject.id) {
        return;
      }
      updateChatProgress(msg.msg || '处理中...', msg.progress);
      setStatus(msg.msg || '处理中...');
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
          if (idx >= 0) storyboards[idx] = Object.assign({}, storyboards[idx], shot);
          else storyboards.push(shot);
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
        toast('错误: ' + (msg.msg || '未知错误'), 'error');
        break;
      default:
        if (msg.progress > 0) updateProgress(msg.progress);
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
    if (panel === 'planning') loadPlanningContent();
    if (panel === 'storyboard' && currentProject) {
      loadProjectStoryboards(currentProject.id, currentEpisode?.id);
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

  function loadPlanningContent() {
    if (!currentProject || !currentEpisode) return;
    const el = document.getElementById('planning-content');
    if (!el) return;
    apiFetch('/api/projects/' + currentProject.id + '/agent-work?type=' + planningType + '&episode_id=' + currentEpisode.id)
      .then(r => r.json())
      .then(data => {
        el.textContent = data.content || '暂无内容，请让 AI 生成或点击下方按钮';
      }).catch(() => {});
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
          box.innerHTML = '<div class="wb-chat-msg assistant">你好！我是 ToonFlow 创作助手。\n\n建议流程：\n1. 导入原文\n2. 事件分析 + AI 分集\n3. 选择一集，生成故事骨架 → 改编策略 → 剧本\n4. 生成分镜 → 图片 → 视频\n\n直接告诉我你想做什么即可。</div>';
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

  function sendChat(message, silent) {
    if (!currentProject) { toast('请先选择项目', 'warning'); return Promise.reject(); }
    const box = document.getElementById('wb-chat-messages');
    if (!silent && box) {
      box.innerHTML += `<div class="wb-chat-msg user">${escapeHtml(message)}</div>`;
      box.scrollTop = box.scrollHeight;
    }
    updateChatProgress('等待 AI 响应...', 5);
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
      if (box) {
        box.innerHTML += `<div class="wb-chat-msg assistant">${escapeHtml(res.reply || '')}</div>`;
        box.scrollTop = box.scrollHeight;
      }
      if (res.action && res.action.type) {
        handleChatAction(res);
      }
      return res;
    }).catch(err => {
      updateChatProgress('', 0);
      throw err;
    });
  }

  function handleChatAction(res) {
    const t = res.action.type;
    if (t === 'analyze_events' || t === 'split_episodes') {
      loadSourceTexts();
      loadEpisodes();
    }
    if (t === 'generate_skeleton' || t === 'generate_strategy' || t === 'generate_script') {
      loadPlanningContent();
      loadEpisodes();
    }
    if (t === 'generate_storyboard') {
      switchWorkbenchPanel('storyboard');
      const items = normalizeStoryboards(res.work);
      if (items.length > 0) {
        storyboards = items;
        renderStoryboards();
        updateVideoTracksFromStoryboards();
        toast('已生成 ' + items.length + ' 个分镜', 'success');
      } else if (currentProject) {
        loadProjectStoryboards(currentProject.id, currentEpisode?.id);
      }
      return;
    }
    if (t === 'extract_assets') {
      loadProjectAssets(currentProject.id);
      switchWorkbenchPanel('assets');
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
    }));
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

  function updateVideoTracksFromStoryboards() {
    videoTracks = (storyboards || [])
      .filter(sb => sb.image_url)
      .map(sb => ({
        shot_number: sb.shot_number,
        thumbnail: sb.image_url,
        duration: sb.duration || 3,
        selected: false,
      }));
    renderVideoTracks();
  }

  function startGeneration(mode) {
    if (!currentProject) { toast('请先选择或创建项目', 'warning'); return; }
    const script = getEpisodeScript();
    if ((mode === 'full' || mode === 'parse') && !script) {
      toast('请先在 AI策划 中为当前集生成剧本', 'warning');
      switchWorkbenchPanel('planning');
      return;
    }
    if (mode === 'images' && storyboards.length === 0) {
      toast('请先生成分镜', 'warning');
      return;
    }
    if (mode === 'video' && !storyboards.some(sb => sb.image_url)) {
      toast('请先生成图片', 'warning');
      return;
    }

    sendWS('start_generate', {
      action: 'start_generate',
      mode: mode,
      project_id: currentProject.id,
      script: script,
      style: currentProject.art_style || '',
      frame_duration: 3,
      resolution: currentProject.video_ratio === '9:16'
        ? '720x1280'
        : (getGeneralSetting('default_resolution', '1280x720')),
      fps: parseInt(getGeneralSetting('default_fps', '24'), 10) || 24,
    });
    isGenerating = true;
    setStatus('发送生成任务...');
  }

  // ======================== 资产 CRUD ========================
  function loadProjectAssets(projectId) {
    apiFetch('/api/assets?project_id=' + projectId).then(r => r.json()).then(list => {
      assets = list || [];
      renderAssets();
    }).catch(() => {});
  }

  function renderAssets() {
    const icons = { role: '👤', scene: '🏞️', prop: '📦' };
    els.assetList.innerHTML = assets.map(a => `
      <div class="asset-item" data-id="${a.id}">
        <div class="asset-thumb">${icons[a.type] || '📋'}</div>
        <div class="asset-info">
          <div class="asset-name">${escapeHtml(a.name)}</div>
          <div class="asset-type-label">${a.type} ${escapeHtml(a.desc ? '— ' + a.desc.substring(0, 30) : '')}</div>
        </div>
        <div class="asset-actions">
          <button class="btn btn-sm btn-outline" onclick="window._app.deleteAsset(${a.id})">×</button>
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
      return;
    }
    els.storyboardEmpty.style.display = 'none';
    els.storyboardCount.textContent = storyboards.length + ' 个分镜';

    els.storyboardList.innerHTML = storyboards.map((sb, i) => `
      <div class="storyboard-card" data-index="${i}">
        <div class="storyboard-card-header">
          <span class="storyboard-card-title">🎬 第 ${sb.shot_number || i + 1} 镜 — ${escapeHtml(sb.scene || '未命名场景')}</span>
          <span style="font-size:12px;color:var(--text-secondary)">${sb.duration || 3}s</span>
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
          <div class="storyboard-card-preview">
            ${sb.image_url ?
              `<img src="${escapeHtml(sb.image_url)}" alt="Shot ${sb.shot_number}">` :
              `<div class="storyboard-card-placeholder">等待生成</div>`
            }
          </div>
        </div>
      </div>
    `).join('');
  }

  // ======================== 视频轨道 ========================
  function renderVideoTracks() {
    if (videoTracks.length === 0) {
      els.videoTracks.innerHTML = '<div class="empty-state-sm"><p>生成图片后此处显示视频轨道</p></div>';
      return;
    }
    els.videoTracks.innerHTML = videoTracks.map((vt, i) => `
      <div class="video-track-item ${vt.selected ? 'selected' : ''}" data-index="${i}">
        <div class="video-track-thumb">
          ${vt.thumbnail ? `<img src="${vt.thumbnail}">` : ''}
        </div>
        <div class="video-track-info">
          <div>Shot ${vt.shot_number || i + 1}</div>
          <div class="video-track-duration">${vt.duration || 3}s</div>
        </div>
      </div>
    `).join('');

    els.videoTracks.querySelectorAll('.video-track-item').forEach(item => {
      item.addEventListener('click', () => {
        els.videoTracks.querySelectorAll('.video-track-item').forEach(t => t.classList.remove('selected'));
        item.classList.add('selected');
      });
    });
  }

  function showVideoResult(url) {
    els.videoPreview.style.display = 'block';
    els.outputVideo.src = url;
    els.downloadLink.href = url;
  }

  // ======================== 任务列表 ========================
  function loadTasks() {
    apiFetch('/api/tasks').then(r => r.json()).then(list => {
      renderTasks(list || []);
    }).catch(() => {});
  }

  function renderTasks(tasks) {
    if (!tasks || tasks.length === 0) {
      els.taskList.innerHTML = '<div class="empty-state"><div class="empty-icon">📋</div><p>暂无任务</p></div>';
      els.taskStats.textContent = '';
      return;
    }

    const counts = { waiting: 0, parsing: 0, drawing: 0, merging: 0, done: 0, error: 0 };
    tasks.forEach(t => { if (counts[t.state] !== undefined) counts[t.state]++; });
    els.taskStats.textContent = `等待:${counts.waiting} 解析:${counts.parsing} 绘图:${counts.drawing} 合成:${counts.merging} 完成:${counts.done} 失败:${counts.error}`;

    els.taskList.innerHTML = tasks.map(t => `
      <div class="task-item">
        <span class="task-state-badge task-state-${t.state}">${stateLabel(t.state)}</span>
        <div class="task-info">
          <div class="task-title">${escapeHtml(t.step || t.id)}</div>
          <div class="task-step">${escapeHtml(t.error_message || '')}</div>
        </div>
        <div class="task-progress-bar">
          <div class="task-progress-fill" style="width:${(t.progress || 0)}%"></div>
        </div>
        <span class="task-time">${t.created_at ? new Date(t.created_at).toLocaleTimeString() : ''}</span>
      </div>
    `).join('');
  }

  function stateLabel(s) {
    const map = { waiting: '等待中', parsing: '解析中', storyboarding: '分镜中', drawing: '绘图中', merging: '合成中', done: '已完成', error: '失败' };
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

  function openProjectModal(projectId) {
    editingProjectId = projectId;
    document.getElementById('project-modal-title').textContent = projectId ? '编辑项目' : '新建项目';
    document.getElementById('btn-save-project').textContent = projectId ? '保存修改' : '创建项目';
    loadStylesForSelect().then(() => {
      if (!projectId) {
        document.getElementById('proj-name').value = '';
        document.getElementById('proj-intro').value = '';
        document.getElementById('proj-type').value = '';
        document.getElementById('proj-artstyle').value = '';
        document.getElementById('proj-ratio').value = '16:9';
        document.getElementById('proj-image-model').value = '';
        return;
      }
      return apiFetch('/api/projects/' + projectId).then(r => r.json()).then(proj => {
        document.getElementById('proj-name').value = proj.name || '';
        document.getElementById('proj-intro').value = proj.intro || '';
        document.getElementById('proj-type').value = proj.type || '';
        document.getElementById('proj-artstyle').value = proj.art_style || '';
        document.getElementById('proj-ratio').value = proj.video_ratio || '16:9';
        document.getElementById('proj-image-model').value = proj.image_model || '';
      });
    });
    els.modalNewProject.style.display = 'flex';
  }

  function loadStylesForSelect() {
    return apiFetch('/api/styles').then(r => r.json()).then(list => {
      const sel = document.getElementById('proj-artstyle');
      sel.innerHTML = '<option value="">默认画风</option>' + (list || []).map(s =>
        `<option value="${s.name}">${s.label || s.name}</option>`
      ).join('');
    }).catch(() => {});
  }

  document.getElementById('btn-save-project').addEventListener('click', () => {
    const name = document.getElementById('proj-name').value.trim();
    if (!name) { toast('请输入项目名称', 'warning'); return; }

    const data = {
      name: name,
      intro: document.getElementById('proj-intro').value,
      type: document.getElementById('proj-type').value,
      art_style: document.getElementById('proj-artstyle').value,
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

  function openAssetModal(type) {
    document.getElementById('asset-modal-title').textContent = '添加' + (type === 'role' ? '角色' : type === 'scene' ? '场景' : '道具');
    document.getElementById('asset-type').value = type;
    document.getElementById('asset-name').value = '';
    document.getElementById('asset-desc').value = '';
    els.modalAsset.style.display = 'flex';
  }

  document.getElementById('btn-close-asset-modal').addEventListener('click', () => els.modalAsset.style.display = 'none');
  document.getElementById('btn-cancel-asset').addEventListener('click', () => els.modalAsset.style.display = 'none');

  document.getElementById('btn-save-asset').addEventListener('click', () => {
    const data = {
      project_id: currentProject ? currentProject.id : '',
      name: document.getElementById('asset-name').value.trim(),
      desc: document.getElementById('asset-desc').value,
      type: document.getElementById('asset-type').value,
    };
    if (!data.name) { toast('请输入名称', 'warning'); return; }

    apiFetch('/api/assets', {
      method: 'POST',
      body: JSON.stringify(data),
    }).then(() => {
      els.modalAsset.style.display = 'none';
      if (currentProject) loadProjectAssets(currentProject.id);
      toast('资产已添加', 'success');
    }).catch(() => toast('添加失败', 'error'));
  });

  // 工作台事件
  document.getElementById('wb-episode-select').addEventListener('change', (e) => {
    currentEpisode = episodes.find(ep => ep.id === e.target.value) || null;
    renderEpisodeList();
    loadPlanningContent();
    loadChatMessages();
    if (currentProject) {
      loadProjectStoryboards(currentProject.id, currentEpisode?.id);
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

  function runProjectAction(path, btn, loadingLabel, onSuccess) {
    if (!currentProject) {
      toast('请先选择项目', 'warning');
      return;
    }
    const origLabel = btn ? btn.textContent : '';
    if (btn) {
      btn.disabled = true;
      btn.textContent = loadingLabel;
    }
    setStatus(loadingLabel + '...');

    apiFetch('/api/projects/' + currentProject.id + path, {
      method: 'POST',
      signal: AbortSignal.timeout(10 * 60 * 1000),
    })
      .then(async (r) => {
        const data = await r.json().catch(() => ({}));
        if (!r.ok) {
          const lid = data.log_id || r.logId || '';
          throw new Error((data.error || ('HTTP ' + r.status)) + (lid ? ' [log_id: ' + lid + ']' : ''));
        }
        return data;
      })
      .then(onSuccess)
      .catch((e) => toast(e.message || '操作失败', 'error'))
      .finally(() => {
        if (btn) {
          btn.disabled = false;
          btn.textContent = origLabel;
        }
        updateChatProgress('', 0);
        setStatus('就绪');
      });
  }

  document.getElementById('btn-analyze-events').addEventListener('click', () => {
    if (!sourceTexts.length) {
      toast('请先导入原文', 'warning');
      return;
    }
    const btn = document.getElementById('btn-analyze-events');
    runProjectAction('/source-texts/analyze', btn, '分析中', (res) => {
      if (res.items && res.items.length) {
        res.items.forEach(item => {
          const row = sourceTexts.find(s => s.id === item.id);
          if (row) row.events = item.events;
        });
        renderSourceTexts();
      } else {
        loadSourceTexts();
      }
      updateChatProgress('', 0);
      toast('事件分析完成，共 ' + (res.analyzed || 0) + ' 章', 'success');
    });
  });

  document.getElementById('btn-split-episodes').addEventListener('click', () => {
    if (!sourceTexts.length) {
      toast('请先导入原文', 'warning');
      return;
    }
    const btn = document.getElementById('btn-split-episodes');
    runProjectAction('/episodes/split', btn, '分集中', (res) => {
      episodes = res.episodes || [];
      if (episodes.length) currentEpisode = episodes[0];
      loadEpisodes();
      switchWorkbenchPanel('script');
      toast('已分 ' + episodes.length + ' 集', 'success');
    });
  });

  document.querySelectorAll('[data-quick]').forEach(btn => {
    btn.addEventListener('click', () => {
      const action = btn.dataset.quick;
      const labels = {
        generate_skeleton: '请为当前集生成故事骨架',
        generate_strategy: '请为当前集生成改编策略',
        generate_script: '请为当前集生成完整剧本',
      };
      if (!currentEpisode) { toast('请先选择一集', 'warning'); return; }
      sendChat(labels[action] || action).catch(() => toast('执行失败', 'error'));
    });
  });

  document.getElementById('btn-extract-assets').addEventListener('click', () => {
    if (!currentEpisode) { toast('请先选择一集', 'warning'); return; }
    sendChat('请从当前集剧本提取角色、场景和道具资产').catch(() => toast('提取失败', 'error'));
  });

  // AI 生成分镜
  document.getElementById('btn-gen-storyboard').addEventListener('click', () => {
    if (!currentEpisode) { toast('请先选择一集', 'warning'); return; }
    sendChat('请为当前集剧本生成分镜').catch(() => toast('生成分镜失败', 'error'));
  });

  // 批量生成图片
  document.getElementById('btn-gen-images').addEventListener('click', () => {
    startGeneration('images');
  });

  // 生成视频
  document.getElementById('btn-gen-video').addEventListener('click', () => {
    startGeneration('video');
  });

  // 设置加载与保存（浏览器 localStorage 优先，服务端为备份）
  const GENERAL_SETTINGS_KEY = 'toonflow_general_settings';

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

    // 定时刷新任务列表
    setInterval(() => {
      if (document.getElementById('page-tasks').classList.contains('active')) {
        loadTasks();
      }
    }, 5000);

    // 定时刷新项目列表
    setInterval(() => {
      if (document.getElementById('page-projects').classList.contains('active')) {
        loadProjects();
      }
    }, 10000);
  }

  init();
})();
