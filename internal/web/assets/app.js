(() => {
  'use strict';

  const KiB = 1024;
  const MiB = 1024 * 1024;
  const GiB = 1024 * 1024 * 1024;
  const TiB = 1024 * 1024 * 1024 * 1024;

  const state = {
    view: 'dashboard',
    chartDays: 14,
    dashOnlyLimited: false,
    histMonth: '',
    histMonths: [],
    histOnlyLimited: false,
    eventsLevel: 'ALL',
    authEnabled: false,
    authenticated: true,
    lastDashboard: null,
    settingsLoaded: false,
    pendingAction: null,
  };

  // --- Utilities ---
  function fmtBytes(b) {
    if (!b || b <= 0) return '0 B';
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
    let v = b;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    return i === 0 ? `${Math.round(v)} B` : `${v.toFixed(2)} ${units[i]}`;
  }

  function fmtSpeed(bps) {
    if (!bps || bps <= 0) return '0 B/s';
    return `${fmtBytes(bps)}/s`;
  }

  function fmtLimitSpeed(bps) {
    if (!bps || bps <= 0) return '不限速';
    return fmtSpeed(bps);
  }

  function escapeHTML(str) {
    return String(str ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function toast(msg, type = 'info') {
    const c = document.getElementById('toast-container');
    const el = document.createElement('div');
    el.className = `toast ${type}`;
    el.textContent = msg;
    c.appendChild(el);
    setTimeout(() => {
      el.style.opacity = '0';
      setTimeout(() => el.remove(), 220);
    }, 3200);
  }

  // --- Theme Switcher (Light / Dark) ---
  const SUN_SVG = `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>`;
  const MOON_SVG = `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>`;

  function applyTheme(theme) {
    const t = theme === 'dark' ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', t);
    localStorage.setItem('qbl_theme', t);
    const deskIcon = document.getElementById('theme-icon-desktop');
    const deskLabel = document.getElementById('theme-label-desktop');
    const mobBtn = document.getElementById('theme-toggle-mobile');
    if (deskIcon) deskIcon.innerHTML = t === 'dark' ? SUN_SVG : MOON_SVG;
    if (deskLabel) deskLabel.textContent = t === 'dark' ? '浅色模式' : '深色模式';
    if (mobBtn) mobBtn.innerHTML = t === 'dark' ? SUN_SVG : MOON_SVG;
  }

  function initTheme() {
    const saved = localStorage.getItem('qbl_theme');
    if (saved) {
      applyTheme(saved);
    } else if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
      applyTheme('dark');
    } else {
      applyTheme('light');
    }
    const toggle = () => {
      const cur = document.documentElement.getAttribute('data-theme');
      applyTheme(cur === 'dark' ? 'light' : 'dark');
    };
    document.getElementById('theme-toggle-desktop')?.addEventListener('click', toggle);
    document.getElementById('theme-toggle-mobile')?.addEventListener('click', toggle);
  }

  // --- API Helper ---
  async function apiFetch(url, opts = {}) {
    const res = await fetch(url, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
      ...opts,
    });
    const data = await res.json().catch(() => ({}));
    if (res.status === 401 && data.auth_required) {
      state.authenticated = false;
      updateAuthUI();
      throw Object.assign(new Error(data.error || '需要登录'), { authRequired: true });
    }
    if (!res.ok) {
      throw new Error(data.error || `请求失败 (${res.status})`);
    }
    return data;
  }

  async function refreshAuthStatus() {
    try {
      const data = await apiFetch('/api/auth/status');
      state.authEnabled = !!data.auth_enabled;
      state.authenticated = !!data.authenticated;
      updateAuthUI();
    } catch (_) {}
  }

  function updateAuthUI() {
    const btn = document.getElementById('btn-logout');
    if (btn) {
      btn.classList.toggle('hidden', !(state.authEnabled && state.authenticated));
    }
  }

  function openAuthModal(onSuccess) {
    state.pendingAction = onSuccess || null;
    const modal = document.getElementById('auth-modal');
    const inp = document.getElementById('auth-modal-pass');
    const err = document.getElementById('auth-modal-err');
    inp.value = '';
    err.classList.add('hidden');
    modal.classList.remove('hidden');
    setTimeout(() => inp.focus(), 50);
  }

  function closeAuthModal() {
    document.getElementById('auth-modal').classList.add('hidden');
    state.pendingAction = null;
  }

  // --- Navigation ---
  function switchView(view) {
    state.view = view;
    document.querySelectorAll('.view-section').forEach((el) => {
      el.classList.toggle('hidden', el.id !== `view-${view}`);
    });
    document.querySelectorAll('.nav-item, .m-tab').forEach((el) => {
      el.classList.toggle('active', el.dataset.view === view);
    });
    if (view === 'dashboard') loadDashboard();
    if (view === 'history') loadHistory();
    if (view === 'logs') loadEvents();
    if (view === 'settings') loadSettings();
  }

  // --- Dashboard ---
  async function loadDashboard() {
    try {
      const q = new URLSearchParams({
        days: String(state.chartDays),
        only_limited: state.dashOnlyLimited ? '1' : '0',
      });
      const data = await apiFetch(`/api/dashboard?${q}`);
      state.lastDashboard = data;
      renderDashboard(data);
    } catch (e) {
      toast('加载监控数据失败: ' + e.message, 'err');
    }
  }

  function renderDashboard(data) {
    const st = data.status || {};
    const today = data.today || {};
    const month = data.month || {};
    const policy = data.policy || {};

    // Sidebar & Mobile connection status
    const sbDot = document.getElementById('sb-dot');
    const sbText = document.getElementById('sb-conn-text');
    const sbLat = document.getElementById('sb-latency');
    const sbHost = document.getElementById('sb-host');
    const sbPoll = document.getElementById('sb-poll');
    const mConn = document.getElementById('m-conn-text');
    const mSpeed = document.getElementById('m-speed-pill');

    if (st.connected) {
      sbDot.className = 'status-dot online';
      sbText.textContent = 'qBittorrent 在线';
      sbLat.textContent = `${st.latency_ms}ms`;
      sbHost.textContent = `${(policy.qb_host || '').replace(/^https?:\/\//, '')}${st.app_version ? ' · ' + st.app_version : ''}`;
      mConn.textContent = `在线 · ${st.latency_ms}ms`;
      mConn.classList.remove('offline');
    } else {
      sbDot.className = 'status-dot offline';
      sbText.textContent = 'qBittorrent 离线';
      sbLat.textContent = '--';
      sbHost.textContent = policy.qb_host || '未配置服务地址';
      mConn.textContent = '未连接';
      mConn.classList.add('offline');
    }
    sbPoll.textContent = `轮询 ${policy.poll_interval_sec || 10}s`;
    mSpeed.textContent = fmtLimitSpeed(st.current_up_limit);

    // Error banner
    const errBanner = document.getElementById('global-error-banner');
    if (!st.connected && st.last_error) {
      document.getElementById('global-error-text').textContent = st.last_error;
      errBanner.classList.remove('hidden');
    } else {
      errBanner.classList.add('hidden');
    }

    // Subtitle
    document.getElementById('dash-subtitle').textContent =
      `实时监控 qBittorrent 日/月上行配额与限速策略${st.last_sync_at ? ' · 上次同步 ' + st.last_sync_at : ''}`;

    // Mode pill & manual override button
    const modePill = document.getElementById('dash-mode-pill');
    const modeText = document.getElementById('dash-mode-text');
    const modeMap = {
      normal: ['pill-emerald', `正常限速 · ${fmtLimitSpeed(st.current_up_limit)}`],
      daily_over: ['pill-rose', `日流量超限 · 熔断 ${fmtSpeed(st.current_up_limit)}`],
      monthly_over: ['pill-rose', `月流量超限 · 熔断 ${fmtSpeed(st.current_up_limit)}`],
      manual_deny: ['pill-amber', `手动熔断中 · ${fmtSpeed(st.current_up_limit)}`],
      paused: ['pill-neutral', '自动限速已暂停'],
    };
    const [pillCls, pillLbl] = modeMap[st.limit_mode] || modeMap.normal;
    modePill.className = `pill ${pillCls}`;
    modeText.textContent = pillLbl;

    const denyBtn = document.getElementById('btn-manual-deny');
    const denyTxt = document.getElementById('btn-manual-deny-text');
    if (st.manual_deny) {
      denyBtn.className = 'btn btn-emerald-soft';
      denyTxt.textContent = '解除手动熔断';
    } else {
      denyBtn.className = 'btn btn-danger-soft';
      denyTxt.textContent = '手动熔断限速';
    }

    // KPI 1: Today Upload
    const dayPct = today.limit_bytes > 0 ? Math.min(100, (today.upload_bytes / today.limit_bytes) * 100) : 0;
    document.getElementById('kpi-day-up').textContent = fmtBytes(today.upload_bytes);
    document.getElementById('kpi-day-limit').textContent = today.limit_bytes > 0 ? `/ ${fmtBytes(today.limit_bytes)}` : '/ 不限额';
    document.getElementById('kpi-day-pct').textContent = today.limit_bytes > 0 ? `${dayPct.toFixed(1)}%` : '不限';
    const dayBar = document.getElementById('kpi-day-bar');
    dayBar.style.width = `${dayPct}%`;
    dayBar.className = `progress-fill ${dayPct >= 100 ? 'rose' : dayPct >= 85 ? 'amber' : 'primary'}`;
    document.getElementById('kpi-day-rem').textContent =
      today.limit_bytes > 0 ? fmtBytes(Math.max(0, today.limit_bytes - today.upload_bytes)) : '无限制';

    // KPI 2: Month Upload
    const monthPct = month.limit_bytes > 0 ? Math.min(100, (month.upload_bytes / month.limit_bytes) * 100) : 0;
    document.getElementById('kpi-month-up').textContent = fmtBytes(month.upload_bytes);
    document.getElementById('kpi-month-limit').textContent = month.limit_bytes > 0 ? `/ ${fmtBytes(month.limit_bytes)}` : '/ 不限额';
    document.getElementById('kpi-month-pct').textContent = month.limit_bytes > 0 ? `${monthPct.toFixed(1)}%` : '不限';
    const monthBar = document.getElementById('kpi-month-bar');
    monthBar.style.width = `${monthPct}%`;
    monthBar.className = `progress-fill ${monthPct >= 100 ? 'rose' : monthPct >= 85 ? 'amber' : 'indigo'}`;
    document.getElementById('kpi-month-days').textContent = `距周期重置 ${month.days_remaining ?? '--'} 天`;
    document.getElementById('kpi-month-rem').textContent =
      month.limit_bytes > 0 ? `余 ${fmtBytes(Math.max(0, month.limit_bytes - month.upload_bytes))}` : '无限制';

    // KPI 3: Live Speed
    document.getElementById('kpi-up-speed').textContent = `↑ ${fmtSpeed(st.up_speed)}`;
    document.getElementById('kpi-down-speed').textContent = `↓ ${fmtSpeed(st.down_speed)}`;
    const spBadge = document.getElementById('kpi-speed-badge');
    spBadge.className = `pill ${st.connected ? (st.is_limited ? 'pill-rose' : 'pill-emerald') : 'pill-neutral'}`;
    spBadge.textContent = st.connected ? (st.is_limited ? '限速中' : '正常运行') : '离线';
    document.getElementById('kpi-cur-limit').textContent = fmtLimitSpeed(st.current_up_limit);

    // KPI 4: Month Download
    document.getElementById('kpi-month-down').textContent = fmtBytes(month.download_bytes);
    const ratio = month.download_bytes > 0 ? (month.upload_bytes / month.download_bytes).toFixed(2) : '∞';
    document.getElementById('kpi-share-ratio').textContent = `分享率 ${month.upload_bytes === 0 && month.download_bytes === 0 ? '0.00' : ratio}`;
    document.getElementById('kpi-limited-days').textContent = `限速 ${month.limited_days || 0} 天`;
    document.getElementById('kpi-day-down').textContent = fmtBytes(today.download_bytes);

    // Chart
    document.getElementById('chart-title').textContent = `近 ${state.chartDays} 天流量趋势`;
    renderTrendChart(data.chart || [], policy.daily_upload_limit || 0);

    // Recent table
    document.getElementById('recent-total-count').textContent = String(data.total_count || 0);
    renderRecentTable(data.recent_logs || [], policy.daily_upload_limit || 0, today.date);
  }

  function renderTrendChart(points, dailyLimit) {
    const container = document.getElementById('trend-chart');
    if (!points.length) {
      container.innerHTML = '<div class="empty-cell">暂无趋势数据</div>';
      return;
    }

    const W = 960;
    const H = 210;
    const padL = 52;
    const padR = 14;
    const padT = 16;
    const padB = 28;
    const innerW = W - padL - padR;
    const innerH = H - padT - padB;

    let maxVal = dailyLimit > 0 ? dailyLimit * 1.15 : 10 * GiB;
    points.forEach((p) => {
      if (p.upload_bytes > maxVal) maxVal = p.upload_bytes * 1.12;
      if (p.download_bytes > maxVal) maxVal = p.download_bytes * 1.12;
    });
    if (maxVal <= 0) maxVal = GiB;

    const yOf = (v) => padT + innerH - (v / maxVal) * innerH;
    const slotW = innerW / points.length;
    const barGap = points.length > 20 ? 2 : 4;
    const upW = Math.max(4, Math.min(20, slotW * 0.42));
    const dnW = Math.max(3, Math.min(14, slotW * 0.30));

    let gridSVG = '';
    for (let i = 0; i <= 3; i++) {
      const v = (maxVal * i) / 3;
      const y = yOf(v);
      gridSVG += `<line x1="${padL}" y1="${y.toFixed(1)}" x2="${W - padR}" y2="${y.toFixed(1)}" stroke="var(--chart-grid)" stroke-width="1"/>`;
      gridSVG += `<text x="${padL - 8}" y="${(y + 4).toFixed(1)}" text-anchor="end" fill="var(--ink-faint)" font-size="11">${fmtBytes(v).replace('.00', '')}</text>`;
    }

    let limitLineSVG = '';
    if (dailyLimit > 0 && dailyLimit <= maxVal) {
      const ly = yOf(dailyLimit);
      limitLineSVG = `<line x1="${padL}" y1="${ly.toFixed(1)}" x2="${W - padR}" y2="${ly.toFixed(1)}" stroke="var(--amber)" stroke-width="1.5" stroke-dasharray="5 4"/>`;
    }

    let barsSVG = '';
    const labelStep = points.length > 16 ? 3 : points.length > 10 ? 2 : 1;

    points.forEach((p, idx) => {
      const cx = padL + idx * slotW + slotW / 2;
      const upH = Math.max(p.upload_bytes > 0 ? 3 : 0, (p.upload_bytes / maxVal) * innerH);
      const dnH = Math.max(p.download_bytes > 0 ? 3 : 0, (p.download_bytes / maxVal) * innerH);
      const totalPairW = upW + barGap + dnW;
      const upX = cx - totalPairW / 2;
      const dnX = upX + upW + barGap;
      const upColor = p.limited ? 'var(--bar-over)' : 'var(--bar-up)';

      barsSVG += `<g class="chart-col" data-idx="${idx}" style="cursor:pointer">
        <rect x="${(padL + idx * slotW).toFixed(1)}" y="${padT}" width="${slotW.toFixed(1)}" height="${innerH}" fill="transparent"/>
        <rect x="${upX.toFixed(1)}" y="${(padT + innerH - upH).toFixed(1)}" width="${upW.toFixed(1)}" height="${upH.toFixed(1)}" rx="3" fill="${upColor}"/>
        <rect x="${dnX.toFixed(1)}" y="${(padT + innerH - dnH).toFixed(1)}" width="${dnW.toFixed(1)}" height="${dnH.toFixed(1)}" rx="3" fill="var(--bar-down)"/>
      </g>`;

      if (idx % labelStep === 0 || idx === points.length - 1) {
        const isLast = idx === points.length - 1;
        barsSVG += `<text x="${cx.toFixed(1)}" y="${H - 6}" text-anchor="middle" fill="${isLast ? 'var(--ink-primary)' : 'var(--ink-muted)'}" font-weight="${isLast ? '600' : '400'}" font-size="11">${escapeHTML(p.label)}</text>`;
      }
    });

    container.innerHTML = `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">${gridSVG}${barsSVG}${limitLineSVG}</svg>`;

    const tooltip = document.getElementById('chart-tooltip');
    const wrap = container.parentElement;
    container.querySelectorAll('.chart-col').forEach((g) => {
      g.addEventListener('mouseenter', (ev) => {
        const p = points[Number(g.dataset.idx)];
        tooltip.innerHTML = `
          <div class="tt-date">${escapeHTML(p.date)} ${p.limited ? '<span style="color:var(--rose)">· 已限速</span>' : ''}</div>
          <div class="tt-row"><span>上行流量</span><strong>${fmtBytes(p.upload_bytes)}</strong></div>
          <div class="tt-row"><span>下行流量</span><strong>${fmtBytes(p.download_bytes)}</strong></div>
        `;
        tooltip.classList.remove('hidden');
        positionTooltip(ev, wrap, tooltip);
      });
      g.addEventListener('mousemove', (ev) => positionTooltip(ev, wrap, tooltip));
      g.addEventListener('mouseleave', () => tooltip.classList.add('hidden'));
    });
  }

  function positionTooltip(ev, wrap, tooltip) {
    const rect = wrap.getBoundingClientRect();
    let x = ev.clientX - rect.left + 12;
    let y = ev.clientY - rect.top - 10;
    if (x + 170 > rect.width) x = ev.clientX - rect.left - 175;
    if (y < 6) y = 6;
    tooltip.style.left = `${x}px`;
    tooltip.style.top = `${y}px`;
  }

  function renderRecentTable(logs, dailyLimit, todayStr) {
    const tbody = document.getElementById('recent-tbody');
    if (!logs.length) {
      tbody.innerHTML = '<tr><td colspan="6" class="empty-cell">暂无流量记录</td></tr>';
      return;
    }
    tbody.innerHTML = logs.slice(0, 12).map((r) => {
      const isToday = r.date === todayStr;
      const pct = dailyLimit > 0 ? Math.min(100, (r.upload_bytes / dailyLimit) * 100) : 0;
      const limited = r.deny_limit === 1;
      const barCls = limited ? 'rose' : pct >= 90 ? 'amber' : 'primary';
      const statusHtml = limited
        ? `<span class="pill pill-rose"><span class="dot"></span>${escapeHTML(r.limit_reason || '已触发限速')}</span>`
        : `<span class="pill pill-emerald"><span class="dot"></span>正常</span>`;
      return `
        <tr class="${isToday ? 'today-row' : ''}">
          <td class="cell-strong">${escapeHTML(r.date)}${isToday ? ' <span class="muted-text">(今日)</span>' : ''}</td>
          <td class="${limited ? 'cell-rose' : 'cell-strong'}">${fmtBytes(r.upload_bytes)}</td>
          <td>
            <div class="mini-progress-cell">
              <div class="progress-track"><div class="progress-fill ${barCls}" style="width:${pct.toFixed(1)}%"></div></div>
              <span>${dailyLimit > 0 ? Math.round(pct) + '%' : '-'}</span>
            </div>
          </td>
          <td>${fmtBytes(r.download_bytes)}</td>
          <td>${statusHtml}</td>
          <td class="cell-muted">${escapeHTML(r.updated_at || '-')}</td>
        </tr>`;
    }).join('');
  }

  // --- History View ---
  async function loadHistory() {
    try {
      const q = new URLSearchParams();
      if (state.histMonth) q.set('month', state.histMonth);
      if (state.histOnlyLimited) q.set('only_limited', '1');
      const data = await apiFetch(`/api/history?${q}`);
      state.histMonth = data.month || '';
      state.histMonths = data.months || [];
      renderHistory(data);
    } catch (e) {
      toast('加载历史明细失败: ' + e.message, 'err');
    }
  }

  function renderHistory(data) {
    const sel = document.getElementById('history-month-select');
    const months = data.months && data.months.length ? data.months : [data.month];
    sel.innerHTML = months.map((m) => `<option value="${escapeHTML(m)}" ${m === data.month ? 'selected' : ''}>${escapeHTML(m)}</option>`).join('');

    const sum = data.summary || {};
    document.getElementById('hist-sum-up').textContent = fmtBytes(sum.total_upload);
    document.getElementById('hist-sum-down').textContent = fmtBytes(sum.total_download);
    const avg = sum.record_days > 0 ? Math.round(sum.total_upload / sum.record_days) : 0;
    document.getElementById('hist-sum-avg').textContent = fmtBytes(avg);
    document.getElementById('hist-sum-limited').textContent = `${sum.limited_days || 0} 天 / ${sum.record_days || 0} 天`;

    const logs = data.logs || [];
    document.getElementById('hist-count-label').textContent = `共 ${logs.length} 条`;

    const tbody = document.getElementById('history-tbody');
    if (!logs.length) {
      tbody.innerHTML = '<tr><td colspan="6" class="empty-cell">该月份暂无流量记录</td></tr>';
      return;
    }
    tbody.innerHTML = logs.map((r) => {
      const limited = r.deny_limit === 1;
      const badge = limited
        ? `<span class="pill pill-rose"><span class="dot"></span>${escapeHTML(r.limit_reason || '已触发限速')}</span>`
        : `<span class="pill pill-emerald"><span class="dot"></span>正常</span>`;
      return `
        <tr>
          <td class="cell-strong">${escapeHTML(r.date)}</td>
          <td class="${limited ? 'cell-rose' : 'cell-strong'}">${fmtBytes(r.upload_bytes)}</td>
          <td>${fmtBytes(r.download_bytes)}</td>
          <td>${badge}</td>
          <td class="cell-muted">${escapeHTML(r.created_at || '-')}</td>
          <td class="cell-muted">${escapeHTML(r.updated_at || '-')}</td>
        </tr>`;
    }).join('');
  }

  // --- Events View ---
  async function loadEvents() {
    try {
      const data = await apiFetch(`/api/events?limit=100&level=${encodeURIComponent(state.eventsLevel)}`);
      const list = document.getElementById('events-list');
      const events = data.events || [];
      if (!events.length) {
        list.innerHTML = '<div class="empty-cell">暂无运行日志</div>';
        return;
      }
      list.innerHTML = events.map((ev) => {
        const cls = ev.level === 'ERROR' ? 'pill-rose' : ev.level === 'WARN' ? 'pill-amber' : 'pill-primary';
        return `
          <div class="event-row">
            <span class="pill ${cls}">${escapeHTML(ev.level)}</span>
            <span class="event-time">${escapeHTML(ev.created_at)}</span>
            <span class="event-msg">${escapeHTML(ev.message)}</span>
          </div>`;
      }).join('');
    } catch (e) {
      toast('加载日志失败: ' + e.message, 'err');
    }
  }

  // --- Settings View ---
  function splitQuotaBytes(bytes, allowMiB) {
    if (!bytes || bytes <= 0) return { val: 0, unit: 'GiB' };
    if (bytes >= TiB && bytes % TiB === 0) return { val: bytes / TiB, unit: 'TiB' };
    if (bytes >= GiB || !allowMiB) return { val: +(bytes / GiB).toFixed(2), unit: 'GiB' };
    return { val: +(bytes / MiB).toFixed(2), unit: 'MiB' };
  }

  function splitSpeedBytes(bps, defaultUnit) {
    if (!bps || bps <= 0) return { val: 0, unit: defaultUnit };
    if (bps >= MiB && (bps % (MiB / 10) === 0 || defaultUnit === 'MiB')) {
      return { val: +(bps / MiB).toFixed(2), unit: 'MiB' };
    }
    return { val: +(bps / KiB).toFixed(1), unit: 'KiB' };
  }

  function toBytes(val, unit) {
    const n = Number(val);
    if (!Number.isFinite(n) || n <= 0) return 0;
    const mult = { KiB, MiB, GiB, TiB }[unit] || 1;
    return Math.round(n * mult);
  }

  function initResetDaySelect() {
    const sel = document.getElementById('cfg-reset-day');
    let html = '';
    for (let d = 1; d <= 28; d++) {
      html += `<option value="${d}">每月 ${d} 日</option>`;
    }
    sel.innerHTML = html;
  }

  function updatePolicyPreview() {
    const daily = toBytes(document.getElementById('cfg-daily-val').value, document.getElementById('cfg-daily-unit').value);
    const monthly = toBytes(document.getElementById('cfg-monthly-val').value, document.getElementById('cfg-monthly-unit').value);
    const normal = toBytes(document.getElementById('cfg-normal-val').value, document.getElementById('cfg-normal-unit').value);
    const deny = toBytes(document.getElementById('cfg-deny-val').value, document.getElementById('cfg-deny-unit').value);
    const resetDay = document.getElementById('cfg-reset-day').value || '1';
    const poll = document.getElementById('cfg-poll-sec').value || '10';
    const enabled = document.getElementById('cfg-monitor-enabled').checked;

    document.getElementById('policy-preview-list').innerHTML = `
      <li>• 日上限 <strong>${daily > 0 ? fmtBytes(daily) : '不限制'}</strong> / 月上限 <strong>${monthly > 0 ? fmtBytes(monthly) : '不限制'}</strong>（每月 ${resetDay} 日重置）</li>
      <li>• 正常速率 <strong>${fmtLimitSpeed(normal)}</strong> → 超限熔断降至 <strong>${fmtSpeed(deny)}</strong></li>
      <li>• 自动监控：<strong>${enabled ? '已开启' : '已暂停'}</strong>，每 <strong>${poll} 秒</strong> 同步一次</li>
    `;
  }

  async function loadSettings() {
    try {
      const cfg = await apiFetch('/api/settings');
      state.settingsLoaded = true;
      fillSettingsForm(cfg);
    } catch (e) {
      if (e.authRequired) {
        openAuthModal(() => loadSettings());
      } else {
        toast('读取配置失败: ' + e.message, 'err');
      }
    }
  }

  function fillSettingsForm(cfg) {
    document.getElementById('cfg-qb-host').value = cfg.qb_host || '';
    document.getElementById('cfg-qb-user').value = cfg.qb_username || '';
    const qbPass = document.getElementById('cfg-qb-pass');
    qbPass.value = '';
    qbPass.placeholder = cfg.qb_password_set ? '已设置密码（留空表示保持不变）' : '请输入 qBittorrent WebUI 密码';

    const d = splitQuotaBytes(cfg.daily_upload_limit, true);
    document.getElementById('cfg-daily-val').value = d.val;
    document.getElementById('cfg-daily-unit').value = d.unit;

    const m = splitQuotaBytes(cfg.monthly_upload_limit, false);
    document.getElementById('cfg-monthly-val').value = m.val;
    document.getElementById('cfg-monthly-unit').value = m.unit;

    const ns = splitSpeedBytes(cfg.normal_upload_speed, 'MiB');
    document.getElementById('cfg-normal-val').value = ns.val;
    document.getElementById('cfg-normal-unit').value = ns.unit;

    const ds = splitSpeedBytes(cfg.deny_upload_speed, 'KiB');
    document.getElementById('cfg-deny-val').value = ds.val;
    document.getElementById('cfg-deny-unit').value = ds.unit;

    document.getElementById('cfg-reset-day').value = String(cfg.billing_reset_day || 1);
    document.getElementById('cfg-monitor-enabled').checked = !!cfg.monitor_enabled;
    document.getElementById('cfg-poll-sec').value = cfg.poll_interval_sec || 10;
    document.getElementById('cfg-retention-days').value = cfg.log_retention_days || 90;
    document.getElementById('cfg-restart-protect').checked = !!cfg.restart_protect;

    document.getElementById('cfg-auth-enabled').checked = !!cfg.auth_enabled;
    const ap = document.getElementById('cfg-auth-pass');
    ap.value = '';
    ap.placeholder = cfg.auth_password_set ? '已设置管理密码（留空保持不变）' : '开启密码保护时请设置密码';
    document.getElementById('auth-pass-field').classList.toggle('hidden', !cfg.auth_enabled);
    document.getElementById('qb-test-result').classList.add('hidden');
    updatePolicyPreview();
  }

  async function saveSettings() {
    const btn = document.getElementById('btn-save-settings');
    btn.disabled = true;
    try {
      const payload = {
        qb_host: document.getElementById('cfg-qb-host').value.trim(),
        qb_username: document.getElementById('cfg-qb-user').value.trim(),
        daily_upload_limit: toBytes(document.getElementById('cfg-daily-val').value, document.getElementById('cfg-daily-unit').value),
        monthly_upload_limit: toBytes(document.getElementById('cfg-monthly-val').value, document.getElementById('cfg-monthly-unit').value),
        normal_upload_speed: toBytes(document.getElementById('cfg-normal-val').value, document.getElementById('cfg-normal-unit').value),
        deny_upload_speed: toBytes(document.getElementById('cfg-deny-val').value, document.getElementById('cfg-deny-unit').value),
        billing_reset_day: Number(document.getElementById('cfg-reset-day').value) || 1,
        monitor_enabled: document.getElementById('cfg-monitor-enabled').checked,
        poll_interval_sec: Number(document.getElementById('cfg-poll-sec').value) || 10,
        log_retention_days: Number(document.getElementById('cfg-retention-days').value) || 90,
        restart_protect: document.getElementById('cfg-restart-protect').checked,
        auth_enabled: document.getElementById('cfg-auth-enabled').checked,
      };
      const qbPass = document.getElementById('cfg-qb-pass').value;
      if (qbPass) payload.qb_password = qbPass;
      const authPass = document.getElementById('cfg-auth-pass').value;
      if (authPass) payload.auth_password = authPass;

      const res = await apiFetch('/api/settings', {
        method: 'PUT',
        body: JSON.stringify(payload),
      });
      fillSettingsForm(res.settings);
      await refreshAuthStatus();
      toast('配置已保存并即时生效', 'ok');
    } catch (e) {
      if (e.authRequired) {
        openAuthModal(() => saveSettings());
      } else {
        toast('保存失败: ' + e.message, 'err');
      }
    } finally {
      btn.disabled = false;
    }
  }

  async function testQBConnection() {
    const btn = document.getElementById('btn-test-qb');
    const banner = document.getElementById('qb-test-result');
    btn.disabled = true;
    btn.textContent = '检测中...';
    banner.classList.add('hidden');
    try {
      const res = await apiFetch('/api/qbit/test', {
        method: 'POST',
        body: JSON.stringify({
          qb_host: document.getElementById('cfg-qb-host').value.trim(),
          qb_username: document.getElementById('cfg-qb-user').value.trim(),
          qb_password: document.getElementById('cfg-qb-pass').value,
        }),
      });
      banner.classList.remove('hidden', 'ok', 'err');
      if (res.ok) {
        banner.classList.add('ok');
        banner.innerHTML = `<span>✓ 连接验证通过 · qBittorrent ${escapeHTML(res.app_version)} ${res.api_version ? `(WebAPI ${escapeHTML(res.api_version)})` : ''}</span><strong>${res.latency_ms}ms</strong>`;
      } else {
        banner.classList.add('err');
        banner.textContent = `✕ 连接失败：${res.error || '未知错误'}`;
      }
    } catch (e) {
      if (e.authRequired) {
        openAuthModal(() => testQBConnection());
      } else {
        banner.classList.remove('hidden', 'ok');
        banner.classList.add('err');
        banner.textContent = `✕ 检测失败：${e.message}`;
      }
    } finally {
      btn.disabled = false;
      btn.textContent = '测试连接';
    }
  }

  async function triggerSync() {
    try {
      const res = await apiFetch('/api/monitor/sync', { method: 'POST' });
      if (res.ok) {
        toast('同步成功', 'ok');
      } else {
        toast('同步异常: ' + (res.error || '请检查配置'), 'err');
      }
      await loadDashboard();
    } catch (e) {
      if (e.authRequired) openAuthModal(() => triggerSync());
      else toast('同步失败: ' + e.message, 'err');
    }
  }

  async function toggleManualDeny() {
    const cur = !!(state.lastDashboard && state.lastDashboard.status && state.lastDashboard.status.manual_deny);
    try {
      const res = await apiFetch('/api/monitor/override', {
        method: 'POST',
        body: JSON.stringify({ manual_deny: !cur }),
      });
      if (res.ok) {
        toast(!cur ? '已触发手动熔断限速' : '已解除手动熔断，恢复自动策略', 'ok');
      } else {
        toast('操作失败: ' + (res.error || ''), 'err');
      }
      await loadDashboard();
    } catch (e) {
      if (e.authRequired) openAuthModal(() => toggleManualDeny());
      else toast('操作失败: ' + e.message, 'err');
    }
  }

  // --- Wire Events ---
  function bindEvents() {
    document.querySelectorAll('.nav-item, .m-tab').forEach((btn) => {
      btn.addEventListener('click', () => switchView(btn.dataset.view));
    });
    document.querySelectorAll('[data-jump]').forEach((btn) => {
      btn.addEventListener('click', () => switchView(btn.dataset.jump));
    });

    // Dashboard controls
    document.getElementById('btn-sync').addEventListener('click', triggerSync);
    document.getElementById('btn-sync-mobile').addEventListener('click', triggerSync);
    document.getElementById('btn-manual-deny').addEventListener('click', toggleManualDeny);

    document.querySelectorAll('#chart-range-tabs .seg-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('#chart-range-tabs .seg-btn').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        state.chartDays = Number(btn.dataset.days);
        loadDashboard();
      });
    });

    document.querySelectorAll('#dash-filter-pills .filter-pill').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('#dash-filter-pills .filter-pill').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        state.dashOnlyLimited = btn.dataset.limited === '1';
        loadDashboard();
      });
    });

    // History controls
    document.getElementById('history-month-select').addEventListener('change', (e) => {
      state.histMonth = e.target.value;
      loadHistory();
    });
    document.getElementById('btn-month-prev').addEventListener('click', () => {
      const idx = state.histMonths.indexOf(state.histMonth);
      if (idx >= 0 && idx < state.histMonths.length - 1) {
        state.histMonth = state.histMonths[idx + 1];
        loadHistory();
      }
    });
    document.getElementById('btn-month-next').addEventListener('click', () => {
      const idx = state.histMonths.indexOf(state.histMonth);
      if (idx > 0) {
        state.histMonth = state.histMonths[idx - 1];
        loadHistory();
      }
    });
    document.querySelectorAll('#hist-filter-pills .filter-pill').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('#hist-filter-pills .filter-pill').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        state.histOnlyLimited = btn.dataset.limited === '1';
        loadHistory();
      });
    });
    document.getElementById('btn-export-csv').addEventListener('click', () => {
      const q = new URLSearchParams();
      if (state.histMonth) q.set('month', state.histMonth);
      if (state.histOnlyLimited) q.set('only_limited', '1');
      window.location.href = `/api/history/export?${q}`;
    });

    // Events controls
    document.querySelectorAll('#events-level-pills .filter-pill').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('#events-level-pills .filter-pill').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        state.eventsLevel = btn.dataset.level;
        loadEvents();
      });
    });
    document.getElementById('btn-refresh-events').addEventListener('click', loadEvents);

    // Settings controls
    document.getElementById('btn-save-settings').addEventListener('click', saveSettings);
    document.getElementById('btn-reset-settings').addEventListener('click', loadSettings);
    document.getElementById('btn-test-qb').addEventListener('click', testQBConnection);
    document.getElementById('cfg-auth-enabled').addEventListener('change', (e) => {
      document.getElementById('auth-pass-field').classList.toggle('hidden', !e.target.checked);
    });
    ['cfg-daily-val', 'cfg-daily-unit', 'cfg-monthly-val', 'cfg-monthly-unit', 'cfg-normal-val', 'cfg-normal-unit', 'cfg-deny-val', 'cfg-deny-unit', 'cfg-reset-day', 'cfg-poll-sec', 'cfg-monitor-enabled'].forEach((id) => {
      document.getElementById(id).addEventListener('input', updatePolicyPreview);
      document.getElementById(id).addEventListener('change', updatePolicyPreview);
    });

    document.querySelectorAll('[data-toggle-pass]').forEach((btn) => {
      btn.addEventListener('click', () => {
        const inp = document.getElementById(btn.dataset.togglePass);
        inp.type = inp.type === 'password' ? 'text' : 'password';
      });
    });

    // Auth modal & logout
    document.getElementById('auth-modal-cancel').addEventListener('click', closeAuthModal);
    document.getElementById('auth-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const pass = document.getElementById('auth-modal-pass').value;
      const errEl = document.getElementById('auth-modal-err');
      errEl.classList.add('hidden');
      try {
        await apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({ password: pass }) });
        state.authenticated = true;
        updateAuthUI();
        const cb = state.pendingAction;
        closeAuthModal();
        toast('登录成功', 'ok');
        if (cb) cb();
      } catch (err) {
        errEl.textContent = err.message;
        errEl.classList.remove('hidden');
      }
    });
    document.getElementById('btn-logout').addEventListener('click', async () => {
      await apiFetch('/api/auth/logout', { method: 'POST' }).catch(() => {});
      state.authenticated = false;
      updateAuthUI();
      toast('已退出后台登录');
      if (state.view === 'settings') switchView('dashboard');
    });
  }

  async function boot() {
    initTheme();
    initResetDaySelect();
    bindEvents();
    await refreshAuthStatus();
    await loadDashboard();

    // Auto refresh dashboard every 15s
    setInterval(() => {
      if (state.view === 'dashboard') loadDashboard();
    }, 15000);
  }

  boot();
})();
