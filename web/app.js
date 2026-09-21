'use strict';

const state = {
  project: null,
  specimens: [],
  currentId: null,
  data: null,
};

const $ = (sel) => document.querySelector(sel);
const statusEl = $('#status');

function setStatus(msg, isErr) {
  statusEl.textContent = msg || '';
  statusEl.style.color = isErr ? '#b91c1c' : '';
}

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error || msg; } catch (_) {}
    throw new Error(msg);
  }
  return res.json();
}

function frameLabel(f) {
  return { spec: '标本坐标', geo: '地理坐标 NED', tilt: '倾斜校正' }[f] || f;
}

function fmt(x, n = 3) {
  if (x === null || x === undefined || Number.isNaN(x)) return '—';
  return Number(x).toFixed(n);
}

function levelLabel(step) {
  if (step.treatment) return step.treatment_label_fallback || '';
  return '';
}

async function loadProject() {
  const j = await api('/api/project');
  state.project = j.project;
  state.specimens = j.specimens || [];
  $('#frame').value = state.project.frame || 'spec';
  renderSpecimenList();
  if (state.specimens.length) {
    await selectSpecimen(state.specimens[0].id);
    $('#empty').classList.add('hidden');
  } else {
    $('#detail').classList.add('hidden');
    $('#empty').classList.remove('hidden');
  }
}

function renderSpecimenList() {
  const ul = $('#specimen-list');
  ul.innerHTML = '';
  for (const sp of state.specimens) {
    const li = document.createElement('li');
    if (sp.id === state.currentId) li.classList.add('active');
    li.innerHTML = `<div class="code">${sp.code}</div><div class="name">${sp.name}</div>`;
    li.addEventListener('click', () => selectSpecimen(sp.id));
    ul.appendChild(li);
  }
}

async function selectSpecimen(id) {
  state.currentId = id;
  renderSpecimenList();
  const data = await api(`/api/specimen/${id}`);
  state.data = data;
  $('#empty').classList.add('hidden');
  $('#detail').classList.remove('hidden');
  renderDetail();
}

function treatmentText(t) {
  if (t.kind === 'AF') return `AF ${fmt(t.level, 0)} mT`;
  if (t.kind === 'TH') return `TH ${fmt(t.level, 0)}°C`;
  return `${t.kind} ${t.level}`;
}

function renderDetail() {
  const d = state.data;
  const sp = d.specimen;
  $('#spec-title').textContent = `${sp.code} · ${sp.name}`;
  const bed = sp.strike !== null && sp.strike !== undefined
    ? `｜产状 走向 ${fmt(sp.strike, 0)}° 倾角 ${fmt(sp.dip, 0)}°` : '';
  $('#spec-meta').textContent =
    `装样 方位 ${fmt(sp.azimuth, 1)}° / 倾伏 ${fmt(sp.plunge, 1)}° / 滚转 ${fmt(sp.roll, 1)}°${bed} ｜ 当前：${frameLabel(d.frame)}`;
  const bust = `?t=${Date.now()}`;
  $('#img-zij').src = `/api/specimen/${sp.id}/plot/zijderveld.svg${bust}`;
  $('#img-stereo').src = `/api/specimen/${sp.id}/plot/stereonet.svg${bust}`;
  $('#steps-frame').textContent = `（${frameLabel(d.frame)}）`;

  // Steps table.
  const tb = $('#steps-table tbody');
  tb.innerHTML = '';
  const fromSel = $('#w-from');
  const toSel = $('#w-to');
  fromSel.innerHTML = '';
  toSel.innerHTML = '';
  d.views.forEach((v, idx) => {
    const tr = document.createElement('tr');
    tr.dataset.seq = v.seq;
    const az = v.moment > 0 ? azimuthInclination(v.v) : null;
    tr.innerHTML = `
      <td>${v.seq}</td>
      <td>${treatmentText(v.treatment)}${v.note ? ` <em>(${escapeHtml(v.note)})</em>` : ''}</td>
      <td>${fmt(v.v.x)}</td><td>${fmt(v.v.y)}</td><td>${fmt(v.v.z)}</td>
      <td>${fmt(v.moment)}</td>
      <td>${az ? `${fmt(az[0], 1)}° / ${fmt(az[1], 1)}°` : '—'}</td>
      <td>${v.note ? escapeHtml(v.note) : ''}</td>
      <td><input type="checkbox" class="excl" data-seq="${v.seq}"/></td>`;
    tb.appendChild(tr);
    for (const sel of [fromSel, toSel]) {
      const o = document.createElement('option');
      o.value = v.seq;
      o.textContent = `${v.seq} · ${treatmentText(v.treatment)}`;
      sel.appendChild(o);
    }
  });
  const seqs = d.views.map((v) => v.seq);
  fromSel.value = seqs[0];
  toSel.value = seqs[seqs.length - 1];

  renderCandidates();
}

function azimuthInclination(v) {
  const n = Math.hypot(v.x, v.y, v.z);
  const inc = Math.asin(Math.max(-1, Math.min(1, v.z / n))) * 180 / Math.PI;
  let az = Math.atan2(v.y, v.x) * 180 / Math.PI;
  if (az < 0) az += 360;
  return [az, inc];
}

function selectedExcludes() {
  return [...document.querySelectorAll('#steps-table .excl:checked')]
    .map((c) => Number(c.dataset.seq));
}

async function addCandidate() {
  const d = state.data;
  const body = {
    label: $('#w-label').value || `候选窗 ${d.candidates.length + 1}`,
    frame: d.frame,
    from_seq: Number($('#w-from').value),
    to_seq: Number($('#w-to').value),
    origin: $('#w-origin').value,
    weighting: $('#w-weight').value,
    manual_exclude: selectedExcludes(),
    bootstrap_seed: Number($('#w-seed').value) || 1164,
    bootstrap_repeats: 2000,
  };
  if (body.to_seq < body.from_seq) {
    setStatus('「至级次」不能早于「自级次」', true);
    return;
  }
  setStatus('正在对窗内向量做特征分解…');
  try {
    await api(`/api/specimen/${d.specimen.id}/candidates`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    setStatus('候选窗已保留');
    await selectSpecimen(d.specimen.id);
  } catch (e) {
    setStatus(e.message, true);
  }
}

function sevClass(sev) { return sev; }

function renderCandidates() {
  const d = state.data;
  const box = $('#candidates');
  box.innerHTML = '';
  if (!d.candidates.length) {
    box.innerHTML = '<p class="hint">尚无候选窗。选择连续级次范围、原点约束与加权方式后加入一个。</p>';
    return;
  }
  for (const cv of d.candidates) {
    const c = cv.candidate;
    const r = cv.result;
    const card = document.createElement('div');
    card.className = 'candidate' + (c.accepted ? ' accepted' : '');
    let html = `
      <h4>${escapeHtml(c.label)}
        <span class="badge ${c.accepted ? 'accepted' : ''}">${c.accepted ? '人工采纳' : '待取舍'}</span>
      </h4>
      <div class="params">
        级次 ${c.from_seq}–${c.to_seq} · ${originText(c.origin)} · ${weightText(c.weighting)}
        · ${frameLabel(c.frame)} · v${cv.version}
        ${(c.manual_exclude && c.manual_exclude.length) ? '· 排除 ' + c.manual_exclude.join(',') : ''}
      </div>`;
    if (r) {
      html += `
        <div class="direction-line">方向 D = (${fmt(r.direction.x, 4)}, ${fmt(r.direction.y, 4)}, ${fmt(r.direction.z, 4)})</div>
        <div class="metric-grid">
          <span class="k">方位角 / 倾角</span><span class="v">${fmt(r.azimuth, 2)}° / ${fmt(r.inclination, 2)}°</span>
          <span class="k">MAD</span><span class="v">${fmt(r.mad, 2)}°</span>
          <span class="k">95% 角置信半角</span><span class="v">${r.angular_gap95 ? fmt(r.angular_gap95, 2) + '°' : '—'}</span>
          <span class="k">端点残差</span><span class="v">${fmt(r.endpoint_residual[0], 3)} / ${fmt(r.endpoint_residual[1], 3)}</span>
          <span class="k">特征值 λ1,λ2,λ3</span><span class="v">${r.eigenvalues.map((x) => fmt(x, 3)).join(', ')}</span>
          <span class="k">纳入 / 总级次</span><span class="v">${r.n} / ${d.views.length}</span>
        </div>`;
      if (r.diagnostics && r.diagnostics.length) {
        html += '<ul class="diags">' + r.diagnostics.map((g) =>
          `<li class="${sevClass(g.severity)}"><strong>${diagCode(g.code)}</strong> · ${escapeHtml(g.message)}</li>`
        ).join('') + '</ul>';
      }
      if (r.excluded_seq && r.excluded_seq.length) {
        html += `<div class="excluded-list">被排除级次：` +
          r.excluded_seq.map((e) => `#${e.seq} ${escapeHtml(e.level)}（${escapeHtml(e.reason)}）`).join('；') +
          `</div>`;
      }
    } else {
      html += '<p class="hint">尚无计算结果。</p>';
    }
    html += `
      <div class="links">
        <a href="/api/candidate/${c.id}/chain?download=1" target="_blank">导出旋转矩阵</a>
        <a href="/api/candidate/${c.id}/versions" target="_blank">查看版本</a>
        <a href="/api/candidate/${c.id}/plot/zijderveld.svg" target="_blank">窗口高亮图</a>
      </div>
      <div class="decision">
        <label><input type="checkbox" class="accept" ${c.accepted ? 'checked' : ''}/> 人工采纳</label>
        <input type="text" class="rationale" placeholder="取舍依据（与计算分层保存）" value="${escapeHtml(c.rationale || '')}"/>
        <button type="button" class="save-decision">保存取舍</button>
      </div>
      <div class="versions">计算版本 v${cv.version} 为不可变记录；bootstrap 种子 ${c.bootstrap_seed}，重放确定性一致。</div>`;
    card.innerHTML = html;
    card.querySelector('.save-decision').addEventListener('click', async () => {
      const accepted = card.querySelector('.accept').checked;
      const rationale = card.querySelector('.rationale').value;
      try {
        await api(`/api/candidate/${c.id}/decision`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ accepted, rationale, reviewer: 'researcher' }),
        });
        setStatus('取舍已分层保存');
        await selectSpecimen(d.specimen.id);
      } catch (e) { setStatus(e.message, true); }
    });
    box.appendChild(card);
  }
}

function originText(o) { return o === 'anchored' ? '过原点' : '自由直线'; }
function weightText(w) {
  return { none: '等权', isotropic: '协方差迹倒数', mahalanobis: '方向方差倒数' }[w] || w;
}
function diagCode(code) {
  return {
    WINDOW_TOO_SHORT: '窗长不足',
    VARIANCE_DEGENERATE: '方差退化',
    NEAR_ZERO_MOMENT: '近零磁矩',
    POLARITY_FLIP: '极性翻转',
    DUPLICATE_LEVEL: '重复级次',
    DUPLICATE_ANOMALOUS_LOAD: '异载荷',
    CI_INSUFFICIENT: '置信区间不可用',
    CI_UNSTABLE: '置信区间不稳',
  }[code] || code;
}
function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, (ch) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]
  ));
}

async function switchFrame() {
  const frame = $('#frame').value;
  await api('/api/frame', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ frame }),
  });
  setStatus(`坐标系切换为 ${frameLabel(frame)}；候选窗在各自坐标帧中原样保留`);
  if (state.currentId) await selectSpecimen(state.currentId);
  else await loadProject();
}

async function resetAndReimport() {
  if (!confirm('将清空全部数据（含候选与取舍），随后重新导入 fixture。继续？')) return;
  setStatus('清空数据库…');
  await api('/api/reset', { method: 'POST' });
  setStatus('重新导入…');
  const r = await api('/api/import', { method: 'POST' });
  setStatus(`重新导入完成：${r.imported_levels} 个级次`);
  await loadProject();
}

$('#btn-import').addEventListener('click', async () => {
  const r = await api('/api/import', { method: 'POST' });
  setStatus(`fixture 导入完成：${r.imported_levels} 个级次`);
  await loadProject();
});
$('#btn-reset').addEventListener('click', resetAndReimport);
$('#frame').addEventListener('change', switchFrame);
$('#btn-add-candidate').addEventListener('click', addCandidate);

(async function init() {
  try {
    await loadProject();
  } catch (e) {
    setStatus(e.message, true);
  }
})();
