'use strict';

const state = {
  frame: 'G',
  specimens: [],
  specimen: null,
  detail: null,
  analyses: [],
  selectedAnalysisId: null,
  explicitKeys: null,
  explicitMode: false,
};

const $ = (id) => document.getElementById(id);

async function api(method, path, body) {
  const opt = { method, headers: {} };
  if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  const res = await fetch(path, opt);
  const text = await res.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch { data = text; }
  }
  if (!res.ok) {
    const msg = (data && data.error) ? data.error : `${res.status} ${res.statusText}`;
    throw new Error(msg);
  }
  return data;
}

function toast(msg, ms = 2600) {
  const el = $('toast');
  el.textContent = msg;
  el.classList.remove('hidden');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.add('hidden'), ms);
}

function showModal(title, body) {
  $('modal-title').textContent = title;
  $('modal-body').textContent = typeof body === 'string' ? body : JSON.stringify(body, null, 2);
  $('modal').classList.remove('hidden');
}

const fmt = (x, d = 2) => (x === null || x === undefined || Number.isNaN(x)) ? '—' : Number(x).toFixed(d);

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

async function init() {
  const project = await api('GET', '/api/project');
  state.frame = project.frame;
  $('frame-select').value = state.frame;

  await loadSpecimens();

  $('frame-select').addEventListener('change', onFrameChange);
  $('btn-fit').addEventListener('click', onFit);
  $('btn-rotations').addEventListener('click', onExportRotations);
  $('btn-runlog').addEventListener('click', onRunLog);
  $('btn-reseed').addEventListener('click', onReseed);
  $('btn-reset').addEventListener('click', onReset);
  $('modal-close').addEventListener('click', () => $('modal').classList.add('hidden'));
  $('modal').addEventListener('click', (e) => {
    if (e.target === $('modal')) $('modal').classList.add('hidden');
  });
  $('inp-treatment').addEventListener('change', renderLevelPicker);
}

async function loadSpecimens() {
  state.specimens = await api('GET', '/api/specimens');
  const list = $('specimen-list');
  if (!state.specimens.length) {
    list.innerHTML = '<div class="tag">数据库为空，点击“重新导入 fixture”</div>';
    return;
  }
  list.innerHTML = state.specimens.map((s) => `
    <div class="specimen-item" data-id="${esc(s.id)}">
      <div class="name">${esc(s.name)} <span class="tag">${esc(s.id)}</span></div>
      <div class="desc">${esc(s.description)}</div>
      <div class="desc">${s.steps} 个测量级 · 帧 ${state.frame}</div>
    </div>`).join('');
  list.querySelectorAll('.specimen-item').forEach((el) => {
    el.addEventListener('click', () => selectSpecimen(el.dataset.id));
  });
  if (!state.specimen || !state.specimens.find((s) => s.id === state.specimen.id)) {
    await selectSpecimen(state.specimens[0].id);
  }
}

async function selectSpecimen(id) {
  document.querySelectorAll('.specimen-item').forEach((el) =>
    el.classList.toggle('active', el.dataset.id === id));
  state.specimen = state.specimens.find((s) => s.id === id);
  state.explicitKeys = null;
  state.explicitMode = false;
  await refreshDetail();
}

async function refreshDetail() {
  if (!state.specimen) return;
  state.detail = await api('GET', `/api/specimens/${encodeURIComponent(state.specimen.id)}?frame=${state.frame}`);
  state.analyses = await api('GET', `/api/analyses?specimen_id=${encodeURIComponent(state.specimen.id)}`);
  // analyses were stored in their own frame; tag that explicitly.
  renderMeta();
  renderTreatmentOptions();
  renderLevelPicker();
  renderPlots();
  renderCandidates();
}

function renderMeta() {
  const d = state.detail;
  $('specimen-meta').innerHTML = `
    <div><b>${esc(d.name)}</b> · ${esc(d.id)}</div>
    <div style="margin:6px 0">${esc(d.description)}</div>
    <div>装样：走向 <b>${fmt(d.mount.trend_deg, 1)}°</b>，倾伏 <b>${fmt(d.mount.plunge_deg, 1)}°</b></div>
    <div>层面：倾向 <b>${fmt(d.bedding.dip_azimuth_deg, 1)}°</b>，倾角 <b>${fmt(d.bedding.dip_angle_deg, 1)}°</b></div>
    <div>当前帧：<b>${d.steps[0] ? d.steps[0].frame : state.frame}</b> ·
      往返误差最大 <b>${fmt(Math.max(0, ...d.steps.map((s) => s.roundtrip_max_abs_err)), 1)}</b></div>
  `;
}

function renderTreatmentOptions() {
  const treatments = [...new Set(state.detail.steps.map((s) => s.treatment))];
  const sel = $('inp-treatment');
  const cur = sel.value;
  sel.innerHTML = treatments.map((t) => `<option value="${t}">${t}${t === 'AF' ? ' 交流' : t === 'TH' ? ' 热' : ''}</option>`).join('');
  if (treatments.includes(cur)) sel.value = cur;
}

function orderedSteps() {
  const t = $('inp-treatment').value;
  return state.detail.steps
    .filter((s) => s.treatment === t)
    .sort((a, b) => a.level - b.level || a.rep - b.rep);
}

function renderLevelPicker() {
  if (!state.detail) return;
  const steps = orderedSteps();
  const levelGroups = new Map();
  steps.forEach((s) => {
    if (!levelGroups.has(s.level)) levelGroups.set(s.level, []);
    levelGroups.get(s.level).push(s);
  });
  if (state.explicitKeys === null) {
    state.explicitKeys = steps.map((s) => s.key);
  } else {
    const valid = new Set(steps.map((s) => s.key));
    state.explicitKeys = state.explicitKeys.filter((k) => valid.has(k));
  }
  const box = $('level-picker');
  box.innerHTML = [...levelGroups.entries()].map(([level, reps]) => reps.map((s) => `
    <label class="level-chip ${reps.length > 1 ? 'rep' : ''}">
      <input type="checkbox" data-key="${esc(s.key)}" ${state.explicitKeys.includes(s.key) ? 'checked' : ''}>
      ${esc(s.treatment)} ${fmt(level, 0)}${reps.length > 1 ? ` · R${s.rep}` : ''}
    </label>`).join('')).join('');
  box.querySelectorAll('input').forEach((cb) => {
    cb.addEventListener('change', () => {
      state.explicitMode = true;
      const k = cb.dataset.key;
      if (cb.checked && !state.explicitKeys.includes(k)) state.explicitKeys.push(k);
      if (!cb.checked) state.explicitKeys = state.explicitKeys.filter((x) => x !== k);
    });
  });
}

function buildWindow() {
  const fromVal = $('inp-from').value;
  const toVal = $('inp-to').value;
  const checked = state.explicitKeys || [];
  if (state.explicitMode) {
    return { keys: [...checked].sort() };
  }
  const w = { treatment: $('inp-treatment').value };
  if (fromVal !== '') w.level_from = Number(fromVal);
  if (toVal !== '') w.level_to = Number(toVal);
  return w;
}

async function onFit() {
  if (!state.specimen) return;
  const body = {
    specimen_id: state.specimen.id,
    frame: state.frame,
    window: buildWindow(),
    origin_anchored: $('inp-anchor').checked,
    weighting: $('inp-weight').value,
    bootstrap_seed: Number($('inp-seed').value || 42),
    note: $('inp-note').value.trim(),
    parent_id: $('inp-parent').value,
  };
  try {
    const view = await api('POST', '/api/analyses', body);
    toast(`已保留候选 v${view.version}（MAD ${fmt(view.result.mad_deg, 2)}°）`);
    await refreshDetail();
    state.selectedAnalysisId = view.id;
    highlightFit(view);
  } catch (e) {
    toast('拟合失败：' + e.message, 4200);
  }
}

function activeStepsFor(view) {
  // Points used by a candidate, resolved in the frame the candidate was made.
  return null;
}

// ---------- SVG plotting ----------

const SVG = 'http://www.w3.org/2000/svg';
function el(tag, attrs = {}, text) {
  const n = document.createElementNS(SVG, tag);
  Object.entries(attrs).forEach(([k, v]) => n.setAttribute(k, v));
  if (text !== undefined) n.textContent = text;
  return n;
}

function clear(node) { while (node.firstChild) node.removeChild(node.firstChild); }

function selectedAnalysisInCurrentFrame() {
  // Pick the most recent candidate computed in the active frame, else null.
  const sameFrame = state.analyses.filter((a) => a.frame === state.frame);
  if (state.selectedAnalysisId) {
    const hit = state.analyses.find((a) => a.id === state.selectedAnalysisId);
    if (hit && hit.frame === state.frame) return hit;
  }
  return sameFrame.length ? sameFrame[sameFrame.length - 1] : null;
}

function renderPlots() {
  renderZijderveld();
  renderStereo();
}

function renderZijderveld() {
  const host = $('zij-plot');
  clear(host);
  const steps = state.detail ? state.detail.steps.slice().sort((a, b) =>
    a.treatment.localeCompare(b.treatment) || a.level - b.level || a.rep - b.rep) : [];
  if (!steps.length) return;

  const W = 520, H = 420, pad = 46;
  const svg = el('svg', { viewBox: `0 0 ${W} ${H}` });
  // Zijderveld uses raw component space; square aspect.
  const pts = steps.flatMap((s) => [
    { x: s.v[0], y: s.v[1], kind: 'h', key: s.key },
    { x: s.v[0], y: s.v[2], kind: 'v', key: s.key },
  ]);
  let minX = Math.min(...pts.map((p) => p.x)), maxX = Math.max(...pts.map((p) => p.x));
  let minY = Math.min(...pts.map((p) => p.y)), maxY = Math.max(...pts.map((p) => p.y));
  const maxR = Math.max(Math.abs(minX), Math.abs(maxX), Math.abs(minY), Math.abs(maxY)) * 1.12;
  minX = minY = -maxR; maxX = maxY = maxR;
  const scaleX = (x) => pad + ((x - minX) / (maxX - minX)) * (W - 2 * pad);
  const scaleY = (y) => H - pad - ((y - minY) / (maxY - minY)) * (H - 2 * pad);

  svg.appendChild(el('rect', { x: pad, y: pad, width: W - 2 * pad, height: H - 2 * pad,
    fill: '#121a24', stroke: '#2b3848' }));
  const axX = scaleX(0), axY = scaleY(0);
  svg.appendChild(el('line', { x1: pad, y1: axY, x2: W - pad, y2: axY, stroke: '#344457', 'stroke-dasharray': '3 4' }));
  svg.appendChild(el('line', { x1: axX, y1: pad, x2: axX, y2: H - pad, stroke: '#344457', 'stroke-dasharray': '3 4' }));
  svg.appendChild(el('text', { x: W - pad + 4, y: axY - 4, class: 'axis-label' }, 'N (X)'));
  svg.appendChild(el('text', { x: axX + 4, y: pad - 6, class: 'axis-label' }, 'E (Y) / 下 (Z)'));

  const fit = selectedAnalysisInCurrentFrame();
  const excluded = new Set((fit ? fit.excluded : []).map((e) => e.key));
  const selected = new Set(fit ? fit.selected : []);

  drawZijPath(svg, steps, scaleX, scaleY, (s) => s.v[0], (s) => s.v[1], '#4ea1ff', excluded, selected, fit, false);
  drawZijPath(svg, steps, scaleX, scaleY, (s) => s.v[0], (s) => s.v[2], '#57d0a6', excluded, selected, fit, true);

  host.appendChild(svg);
}

function drawZijPath(svg, steps, sx, sy, fx, fy, color, excluded, selected, fit, vertical) {
  const groups = new Map();
  steps.forEach((s) => {
    if (!groups.has(s.treatment)) groups.set(s.treatment, []);
    groups.get(s.treatment).push(s);
  });
  groups.forEach((list) => {
    list.sort((a, b) => a.level - b.level || a.rep - b.rep);
    const d = list.map((s, i) => `${i ? 'L' : 'M'}${sx(fx(s)).toFixed(1)},${sy(fy(s)).toFixed(1)}`).join(' ');
    svg.appendChild(el('path', { d, class: 'path-line', stroke: '#3c4c60' }));
    list.forEach((s) => {
      const isOut = fit && excluded.has(s.key);
      const c = el('circle', {
        cx: sx(fx(s)), cy: sy(fy(s)), r: isOut ? 3 : 4.2,
        class: 'pt' + (isOut ? ' excluded' : ''),
        fill: isOut ? '#6b7787' : color,
      });
      const title = `${s.key} (${s.treatment} ${s.level}${s.rep > 1 ? ' R' + s.rep : ''}) |M|=${s.norm_ma_m.toFixed(2)}`;
      c.appendChild(el('title', {}, title));
      svg.appendChild(c);
      if (isOut) {
        svg.appendChild(el('line', { x1: sx(fx(s)) - 4, y1: sy(fy(s)) - 4, x2: sx(fx(s)) + 4, y2: sy(fy(s)) + 4, stroke: '#ef6f6f', 'stroke-width': 1.3 }));
      }
    });
  });

  if (fit && fit.selected.length) {
    const used = steps.filter((s) => selected.has(s.key)).sort((a, b) => a.level - b.level);
    if (!used.length) return;
    const dir = fit.result.direction;
    // Direction components for the chosen projection plane.
    const dx = dir.x, dy = vertical ? dir.z : dir.y;
    // Anchor offset: reconstruct from centroid / anchor residual.
    const c = fit.result.centroid;
    const cx = c.x, cy = vertical ? c.z : c.y;
    // Determine t-range covering selected points in this plane.
    const ts = used.map((s) => {
      const px = fx(s) - cx, py = fy(s) - cy;
      return (px * dx + py * dy) / (dx * dx + dy * dy);
    });
    const t0 = fit.anchored ? -Math.min(0, ...ts) - 0 : Math.min(...ts);
    const t1 = Math.max(...ts);
    const p0 = fit.anchored ? { x: 0, y: 0 } : { x: cx, y: cy };
    const A = { x: p0.x + dx * t0, y: p0.y + dy * t0 };
    const B = { x: p0.x + dx * t1, y: p0.y + dy * t1 };
    svg.appendChild(el('line', {
      x1: sx(A.x), y1: sy(A.y), x2: sx(B.x), y2: sy(B.y), class: 'fit-line',
    }));
  }
}

function renderStereo() {
  const host = $('stereo-plot');
  clear(host);
  const steps = state.detail ? state.detail.steps.slice().sort((a, b) =>
    a.treatment.localeCompare(b.treatment) || a.level - b.level || a.rep - b.rep) : [];
  if (!steps.length) return;

  const W = 440, H = 420, cx = W / 2, cy = H / 2 + 6, R = 165;
  const svg = el('svg', { viewBox: `0 0 ${W} ${H}` });

  // equal-area (Schmidt) lower hemisphere; flip upper hemisphere through origin
  const project = (v) => {
    const n = Math.hypot(v[0], v[1], v[2]);
    let x = v[0] / n, y = v[1] / n, z = v[2] / n;
    let upper = false;
    if (z < 0) { x = -x; y = -y; z = -z; upper = true; }
    const r = R * Math.sqrt(1 - z); // Schmidt equal-area lower hemisphere
    const ang = Math.atan2(y, x);
    return { px: cx + r * Math.cos(ang), py: cy - r * Math.sin(ang), upper };
  };

  [R, R * 0.7071].forEach((rr) => svg.appendChild(el('circle', { cx, cy, r: rr, class: 'grid-ring' })));
  svg.appendChild(el('line', { x1: cx - R, y1: cy, x2: cx + R, y2: cy, stroke: '#263240' }));
  svg.appendChild(el('line', { x1: cx, y1: cy - R, x2: cx, y2: cy + R, stroke: '#263240' }));
  [['N', cx, cy - R - 6], ['E', cx + R + 10, cy + 4], ['S', cx, cy + R + 16], ['W', cx - R - 14, cy + 4]]
    .forEach(([t, x, y]) => svg.appendChild(el('text', { x, y, class: 'tick-label', 'text-anchor': 'middle' }, t)));

  const fit = selectedAnalysisInCurrentFrame();
  const excluded = new Set((fit ? fit.excluded : []).map((e) => e.key));

  const groups = new Map();
  steps.forEach((s) => {
    if (!groups.has(s.treatment)) groups.set(s.treatment, []);
    groups.get(s.treatment).push(s);
  });
  groups.forEach((list, treatment) => {
    list.sort((a, b) => a.level - b.level || a.rep - b.rep);
    for (let i = 1; i < list.length; i++) {
      const a = project(list[i - 1].v), b = project(list[i].v);
      svg.appendChild(el('line', { x1: a.px, y1: a.py, x2: b.px, y2: b.py,
    stroke: '#3c4c60', 'stroke-width': 1.2 }));
    }
    list.forEach((s, i) => {
      const p = project(s.v);
      const isOut = fit && excluded.has(s.key);
      const hue = treatment === 'AF' ? '#7db6ff' : '#7fe0bd';
      svg.appendChild(el('circle', {
        cx: p.px, cy: p.py, r: isOut ? 3 : 4.5,
        fill: isOut ? '#6b7787' : hue, class: 'pt' + (isOut ? ' excluded' : ''),
        stroke: p.upper ? '#ef6f6f' : '#0f141b',
      })).appendChild(el('title', {}, `${s.key} ${isOut ? '(排除)' : ''}`));
      if (i === 0 || s.rep > 1) {
        svg.appendChild(el('text', { x: p.px + 6, y: p.py - 5, class: 'pt-label' },
          `${treatment}${s.level}${s.rep > 1 ? 'R' + s.rep : ''}`));
      }
    });
  });

  if (fit && fit.result.direction) {
    const dp = project([fit.result.direction.x, fit.result.direction.y, fit.result.direction.z]);
    // alpha95 cone in equal-area: approximate as screen circle of angular radius.
    const a95 = fit.result.alpha95_deg || 0;
    const rPix = R * Math.sqrt(1 - Math.cos(a95 * Math.PI / 180));
    svg.appendChild(el('circle', { cx: dp.px, cy: dp.py, r: Math.max(2, rPix),
      fill: 'rgba(242,178,78,.10)', stroke: '#f2b24e', 'stroke-width': 1.4 }));
    svg.appendChild(el('circle', { cx: dp.px, cy: dp.py, r: 5, fill: '#f2b24e', stroke: '#0f141b' }));
    const alt = project([fit.result.alternate_pole.x, fit.result.alternate_pole.y, fit.result.alternate_pole.z]);
    svg.appendChild(el('circle', { cx: alt.px, cy: alt.py, r: 3.2, fill: 'none', stroke: '#f2b24e', 'stroke-dasharray': '2 2' }));
    svg.appendChild(el('text', { x: dp.px + 8, y: dp.py - 8, class: 'pt-label', fill: '#f2b24e' },
      `v${fit.version} ${fit.result.declination_deg.toFixed(1)}/${fit.result.inclination_deg.toFixed(1)}`));
  }

  host.appendChild(svg);
}

function renderCandidates() {
  $('cand-frame').textContent = `当前坐标帧 ${state.frame}；候选按各自创建帧显示`;
  const parentSel = $('inp-parent');
  parentSel.innerHTML = '<option value="">（无）</option>' + state.analyses.map((a) =>
    `<option value="${a.id}">v${a.version} · ${a.frame} · ${a.treatment}${a.level_from ?? '∗'}–${a.level_to ?? '∗'} · MAD ${fmt(a.result.mad_deg, 1)}°</option>`).join('');

  const grid = $('candidate-grid');
  if (!state.analyses.length) {
    grid.innerHTML = '<div class="tag">尚无候选窗。选择连续处理窗或手动勾选级次后点击“计算候选窗”。</div>';
    return;
  }
  grid.innerHTML = state.analyses.slice().reverse().map((a) => {
    const r = a.result;
    const decision = a.decision ? a.decision.action : 'candidate';
    const diags = (r.diagnostics || []).map((d) =>
      `<div class="diag ${d.severity}">${diagLabel(d.code)} ${esc(d.step_key ? '· ' + d.step_key : '')} — ${esc(d.message)}</div>`).join('');
    const repCount = r.replicates ? Object.values(r.replicates).reduce((n, arr) => n + arr[0].count, 0) : 0;
    return `
    <div class="cand ${decision}" data-id="${a.id}">
      <h3>候选 v${a.version} <span class="tag">${a.frame} 帧</span>
        <span class="tag">${a.anchored ? '原点约束' : '自由拟合'}</span>
        <span class="tag">${a.weighting === 'inverse_variance' ? '逆方差' : '等权'}</span></h3>
      <div class="metric"><span>处理方式 / 窗</span><b>${esc(a.treatment)} ${a.level_from === null ? '∗' : fmt(a.level_from, 0)}–${a.level_to === null ? '∗' : fmt(a.level_to, 0)}（${r.n} 点）</b></div>
      <div class="metric"><span>方向 D / I</span><b>${fmt(r.declination_deg, 2)}° / ${fmt(r.inclination_deg, 2)}°</b></div>
      <div class="metric"><span>MAD</span><b>${fmt(r.mad_deg, 3)}°</b></div>
      <div class="metric"><span>α95（bootstrap）</span><b>${fmt(r.alpha95_deg, 3)}° · n=${r.bootstrap_count}</b></div>
      <div class="metric"><span>端点残差 (首/末)</span><b>${fmt(r.endpoints[0].perp_distance_ma_m, 3)} / ${fmt(r.endpoints[1].perp_distance_ma_m, 3)}</b></div>
      <div class="metric"><span>原点偏离</span><b>${fmt(r.anchor_residual_ma_m, 3)} mA/m</b></div>
      <div class="metric"><span>被排除级次</span><b>${a.excluded.length} 个</b></div>
      <div class="metric"><span>重复复测</span><b>${repCount > 0 ? '保留 ' + repCount + ' 行（未合并）' : '无'}</b></div>
      ${diags ? `<div class="diags">${diags}</div>` : ''}
      <div class="metric"><span>人工取舍</span><b>${decisionLabel(decision)}${a.decision && a.decision.rationale ? '：' + esc(a.decision.rationale) : ''}</b></div>
      <input class="rationale" data-id="${a.id}" placeholder="取舍理由（可选）" value="${a.decision ? esc(a.decision.rationale || '') : ''}">
      <div class="cand-actions">
        <button class="ok" data-action="accepted" data-id="${a.id}">采纳</button>
        <button class="no" data-action="rejected" data-id="${a.id}">拒绝</button>
        <button data-action="candidate" data-id="${a.id}">保留候选</button>
        <button data-action="excluded" data-id="${a.id}">排除原因(${a.excluded.length})</button>
        <button data-action="trace" data-id="${a.id}">追溯谱系</button>
        <button data-action="discard" data-id="${a.id}">删除候选</button>
      </div>
      ${a.note ? `<div class="tag">备注：${esc(a.note)}</div>` : ''}
    </div>`;
  }).join('');

  grid.querySelectorAll('button[data-action]').forEach((btn) => {
    btn.addEventListener('click', () => onCandidateAction(btn.dataset.action, btn.dataset.id));
  });
  grid.querySelectorAll('.cand').forEach((card) => {
    card.addEventListener('click', (e) => {
      if (e.target.closest('button, input')) return;
      state.selectedAnalysisId = card.dataset.id;
      const a = state.analyses.find((x) => x.id === card.dataset.id);
      if (a && a.frame === state.frame) { renderPlots(); highlightFit(a); }
    });
  });
}

function highlightFit() { /* plots already read selection */ }

function decisionLabel(a) {
  return { accepted: '已采纳', rejected: '已拒绝', candidate: '候选中' }[a] || a;
}
function diagLabel(c) {
  return {
    WINDOW_TOO_SHORT: '窗长不足',
    DEGENERATE_VARIANCE: '方差退化',
    NEAR_ZERO_MOMENT: '近零磁矩',
    POLARITY_FLIP_NEARBY: '极性翻转附近',
    REPEATED_LOAD_MISMATCH: '重复级异载荷',
    BOOTSTRAP_UNSTABLE: 'bootstrap 不稳定',
  }[c] || c;
}

async function onCandidateAction(action, id) {
  if (action === 'trace') { showLineage(id); return; }
  if (action === 'excluded') {
    const a = state.analyses.find((x) => x.id === id);
    const text = a.excluded.length
      ? a.excluded.map((e) => `${e.key} — ${e.reason}`).join('\n')
      : '该候选未排除任何级次（全部复测均保留）';
    showModal('被排除级次及原因', text);
    return;
  }
  if (action === 'discard') {
    if (!confirm('删除该候选？版本号保持占用（仅从候选列表移除，运行日志保留）')) return;
    try {
      await api('DELETE', `/api/analyses/${encodeURIComponent(id)}`);
      toast('候选已删除');
      if (state.selectedAnalysisId === id) state.selectedAnalysisId = null;
      await refreshDetail();
    } catch (e) { toast(e.message, 3500); }
    return;
  }
  const rationale = document.querySelector(`input.rationale[data-id="${id}"]`).value.trim();
  try {
    await api('PUT', '/api/decisions', {
      specimen_id: state.specimen.id, analysis_id: id, action, rationale,
    });
    toast(`已标记为「${decisionLabel(action)}」`);
    await refreshDetail();
  } catch (e) { toast(e.message, 3500); }
}

async function showLineage(id) {
  const view = await api('GET', `/api/analyses/${encodeURIComponent(id)}`);
  const lines = [`候选 ${view.id}（标本 ${view.specimen_id}，帧 ${view.frame}）`,
    `计算版本 v${view.version}${view.parent_id ? '，父候选 ' + view.parent_id : '，无父候选'}`,
    `权重 ${view.weighting} · ${view.anchored ? '原点约束' : '自由拟合'} · bootstrap 种子 ${view.bootstrap_seed}`,
    '', '纳入级次（原始 key 与变换链可在级次列表/旋转矩阵导出中逐条追溯）：',
    ...view.selected.map((k) => '  + ' + k),
    '', '被排除级次：',
    ...(view.excluded.length ? view.excluded.map((e) => `  - ${e.key}: ${e.reason}`) : ['  （无）']),
    '', '计算谱系：',
    ...view.lineage.map((n) => `  v${n.version} ${n.frame} ${n.analysis_id}${n.parent_id ? ' <- ' + n.parent_id : ''} ${n.note || ''}`),
    '', `请求哈希 ${view.req_hash}；创建于 ${view.created_at}`];
  showModal('方向追溯 · 原始级次与坐标链', lines.join('\n'));
}

async function onFrameChange() {
  state.frame = $('frame-select').value;
  await api('PUT', '/api/project/frame', { frame: state.frame });
  state.selectedAnalysisId = null;
  toast(`坐标帧切换为 ${state.frame}；候选与拒绝级次按原帧保留，重开项目后原样恢复`);
  await loadSpecimens();
}

async function onExportRotations() {
  if (!state.specimen) return;
  const data = await api('GET', `/api/specimens/${encodeURIComponent(state.specimen.id)}/rotations?frame=${state.frame}`);
  const stepsText = data.steps.map((s) => {
    const rows = [];
    for (let r = 0; r < 3; r++) rows.push('    [' + s.matrix.A.slice(r * 3, r * 3 + 3).map((x) => x.toFixed(9)).join(', ') + ']');
    const ang = Object.entries(s.angle_deg || {}).map(([k, v]) => `${k}=${v}°`).join(', ');
    return `  ${s.name}: ${s.frame_in} -> ${s.frame_out}${ang ? ' (' + ang + ')' : ''}\n  matrix = [\n${rows.join('\n')}\n  ]`;
  }).join('\n\n');
  const comp = [];
  for (let r = 0; r < 3; r++) comp.push('  [' + data.composed.A.slice(r * 3, r * 3 + 3).map((x) => x.toFixed(9)).join(', ') + ']');
  download(`${data.specimen_id}_rotations_${data.frame}.txt`,
    `旋转链导出 · 标本 ${data.specimen_id} → 帧 ${data.frame}\n生成 ${data.generated_at}\n约定：${data.convention}\n\n${stepsText}\n\n合成矩阵 M->${data.frame} =\n${comp.join('\n')}\n`);
}

function download(name, text) {
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

async function onRunLog() {
  const logs = await api('GET', '/api/runlog');
  const text = logs.map((l) =>
    `${l.ts}  ${l.method.padEnd(6)} ${l.status}  ${l.path}${l.request_hash && l.request_hash.Valid ? '  req=' + l.request_hash.String : ''}  ${l.summary || ''}`).join('\n');
  showModal('运行记录（可导出复核）', text || '（暂无记录）');
}

async function onReseed() {
  const r = await api('POST', '/api/admin/reseed');
  toast(`已重新导入 ${r.imported} 个固定 fixture 标本`);
  state.specimen = null;
  await loadSpecimens();
}

async function onReset() {
  if (!confirm('将清空所有标本、候选与决策（保留库表）。可随后重新导入 fixture 复核。继续？')) return;
  await api('POST', '/api/admin/reset');
  toast('数据库已清空');
  state.specimen = null;
  state.analyses = [];
  await loadSpecimens();
}

init().catch((e) => {
  toast('初始化失败：' + e.message, 6000);
  console.error(e);
});
