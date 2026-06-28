/**
 * ToonFlow — 短剧 AI 创作平台
 * 对标 ToonFlow 产品逻辑：项目制工作流、资产管理、分镜编辑、视频轨道
 */
(function () {
  'use strict';

  // ======================== 状态 ========================
  let ws = null;
  let reconnectTimer = null;
  let currentProject = null; // { id, name, art_style, ... }
  let storyboards = [];      // 当前项目的分镜列表
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
    // 侧边栏
    sidebarTabs: document.querySelectorAll('.sidebar-tab'),
    sidebarPanels: document.querySelectorAll('.sidebar-panel'),
    assetList: document.getElementById('asset-list'),
    styleList: document.getElementById('style-list'),
    scriptInput: document.getElementById('sidebar-script'),
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
  };

  // ======================== 页面切换 ========================
  function showPage(pageId) {
    els.navBtns.forEach(b => b.classList.toggle('active', b.dataset.page === pageId));
    els.pages.forEach(p => p.classList.toggle('active', p.id === 'page-' + pageId));
  }

  // 侧边栏 tab 切换
  document.querySelectorAll('.sidebar-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      document.querySelectorAll('.sidebar-tab').forEach(t => t.classList.remove('active'));
      document.querySelectorAll('.sidebar-panel').forEach(p => p.classList.remove('active'));
      tab.classList.add('active');
      document.getElementById('panel-' + tab.dataset.tab).classList.add('active');
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
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(proto + '//' + location.host + '/ws');

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
          storyboards = msg.data.storyboard;
          renderStoryboards();
        }
        break;
      case 'gen_image':
        setStatus('🎨 AI 绘图中 (' + (msg.data?.current_shot || '?') + '/' + (msg.data?.total_shots || '?') + ')');
        updateProgress(msg.progress);
        break;
      case 'merge_video':
        setStatus('🎬 视频合成中...');
        updateProgress(msg.progress);
        break;
      case 'finish':
        setStatus('🎉 生成完成！');
        updateProgress(100);
        isGenerating = false;
        if (msg.data && msg.data.video_url) {
          showVideoResult(msg.data.video_url);
        }
        toast('短剧生成完成！', 'success');
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
    fetch('/api/projects').then(r => r.json()).then(list => {
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
      </div>
    `).join('');

    els.projectList.querySelectorAll('.project-card').forEach(card => {
      card.addEventListener('click', () => selectProject(card.dataset.id));
    });
  }

  function statusLabel(s) {
    const map = { draft: '草稿', processing: '进行中', done: '已完成', error: '失败' };
    return map[s] || s;
  }

  function selectProject(id) {
    fetch('/api/projects/' + id).then(r => r.json()).then(proj => {
      currentProject = proj;
      els.projectName.textContent = proj.name;
      els.projectName.style.display = 'inline';
      els.projectDisplay.textContent = '当前项目: ' + proj.name;
      loadProjectAssets(proj.id);
      loadProjectStoryboards(proj.id);
      toast('已切换到: ' + proj.name, 'info');
    }).catch(() => toast('加载项目失败', 'error'));
  }

  // ======================== 资产 CRUD ========================
  function loadProjectAssets(projectId) {
    fetch('/api/assets?project_id=' + projectId).then(r => r.json()).then(list => {
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
    deleteAsset: function(id) {
      fetch('/api/assets/' + id, { method: 'DELETE' }).then(() => {
        assets = assets.filter(a => a.id !== id);
        renderAssets();
        toast('资产已删除', 'info');
      }).catch(() => toast('删除失败', 'error'));
    }
  };

  // ======================== 画风列表 ========================
  function loadStyles() {
    fetch('/api/styles').then(r => r.json()).then(list => {
      const icons = ['🎭', '🖌️', '📐', '💕', '🎪', '🏯', '🧸', '🌃', '👘', '🏙️', '🌆'];
      els.styleList.innerHTML = (list || []).map((s, i) => `
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
    fetch('/api/tasks').then(r => r.json()).then(list => {
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
  function loadVendors() {
    fetch('/api/vendors').then(r => r.json()).then(list => {
      els.vendorList.innerHTML = (list || []).map(v => `
        <div class="vendor-card">
          <div class="vendor-card-info">
            <div class="vendor-card-name">${escapeHtml(v)}</div>
            <div class="vendor-card-models">已注册适配器</div>
          </div>
          <div class="toggle-switch on" onclick="this.classList.toggle('on')"></div>
        </div>
      `).join('');
    }).catch(() => {});
  }

  // ======================== 事件绑定 ========================
  // 新建项目
  document.getElementById('btn-new-project').addEventListener('click', () => {
    els.modalNewProject.style.display = 'flex';
    loadStylesForSelect();
  });

  document.getElementById('btn-close-modal').addEventListener('click', closeModal);
  document.getElementById('btn-cancel-project').addEventListener('click', closeModal);
  els.modalNewProject.addEventListener('click', (e) => { if (e.target === els.modalNewProject) closeModal(); });

  function closeModal() {
    els.modalNewProject.style.display = 'none';
  }

  function loadStylesForSelect() {
    fetch('/api/styles').then(r => r.json()).then(list => {
      const sel = document.getElementById('proj-artstyle');
      sel.innerHTML = '<option value="">默认画风</option>' + (list || []).map(s =>
        `<option value="${s.name}">${s.label || s.name}</option>`
      ).join('');
    }).catch(() => {});
  }

  document.getElementById('btn-create-project').addEventListener('click', () => {
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

    fetch('/api/projects', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    }).then(r => r.json()).then(proj => {
      currentProject = proj;
      closeModal();
      loadProjects();
      toast('项目 "' + name + '" 已创建', 'success');
    }).catch(() => toast('创建项目失败', 'error'));
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

    fetch('/api/assets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    }).then(() => {
      els.modalAsset.style.display = 'none';
      if (currentProject) loadProjectAssets(currentProject.id);
      toast('资产已添加', 'success');
    }).catch(() => toast('添加失败', 'error'));
  });

  // 剧本解析
  document.getElementById('btn-parse-script').addEventListener('click', () => {
    const script = els.scriptInput.value.trim();
    if (!script) { toast('请输入剧本', 'warning'); return; }
    if (!currentProject) { toast('请先选择或创建项目', 'warning'); return; }

    sendWS('start_generate', {
      action: 'start_generate',
      script: script,
      style: currentProject.art_style || '',
      frame_duration: 3,
      resolution: '1280x720',
      fps: 24,
    });
    isGenerating = true;
    setStatus('发送生成任务...');
  });

  // AI 生成分镜
  document.getElementById('btn-gen-storyboard').addEventListener('click', () => {
    if (!currentProject) { toast('请先选择项目', 'warning'); return; }
    const script = els.scriptInput.value.trim();
    if (!script) { toast('请先在剧本面板输入剧本', 'warning'); return; }
    sendWS('start_generate', {
      action: 'start_generate',
      script: script,
      style: currentProject.art_style || '',
      frame_duration: 3,
      resolution: '1280x720',
      fps: 24,
    });
  });

  // 批量生成图片
  document.getElementById('btn-gen-images').addEventListener('click', () => {
    if (!currentProject) { toast('请先选择项目', 'warning'); return; }
    toast('批量生成图片功能开发中...', 'info');
  });

  // 生成视频
  document.getElementById('btn-gen-video').addEventListener('click', () => {
    if (!currentProject) { toast('请先选择项目', 'warning'); return; }
    toast('视频合成功能开发中...', 'info');
  });

  // 设置保存
  document.getElementById('btn-save-settings').addEventListener('click', () => {
    const data = {
      output_dir: document.getElementById('set-output-dir').value,
      fps: document.getElementById('set-fps').value,
      resolution: document.getElementById('set-resolution').value,
      max_tasks: document.getElementById('set-max-tasks').value,
      ffmpeg: document.getElementById('set-ffmpeg').value,
    };
    fetch('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    }).then(() => toast('设置已保存', 'success')).catch(() => toast('保存失败', 'error'));
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

  // ======================== 初始化 ========================
  function init() {
    connectWS();
    loadProjects();
    loadStyles();
    loadVendors();
    loadTasks();

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
