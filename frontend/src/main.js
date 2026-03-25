import './style.css';
import { ChooseMediaFile, GetOverview, StartLogin, GetLoginStatus, GetLoginQRCode, SaveSettings, ListEvents, ListDeadLetters, SendText, SendMedia, RetryDeadLetter, Logout } from '../wailsjs/go/main/AppBridge';
import { ClipboardSetText } from '../wailsjs/runtime/runtime';

const state = {
  sessionId: '',
  eventAfterId: 0,
  polling: null,
  connected: null,
  events: [],
  deadLetters: [],
  mediaFilePath: '',
  recentInboundUserId: '',
};

const app = document.querySelector('#app');

app.innerHTML = `
  <main class="shell">
    <section class="layout">
      <div class="left-col">
        <div class="panel">
          <div class="panel-head">
            <div>
              <h2>本地服务</h2>
            </div>
          </div>
          <label class="field">
            <span>监听地址</span>
            <input id="listenAddr" placeholder="127.0.0.1:17890" />
          </label>
          <label class="field">
            <span>回调地址</span>
            <input id="webhookUrl" placeholder="https://example.com/webhook" />
          </label>
          <button id="saveSettingsBtn" class="primary wide">保存设置</button>
          <p class="hint">监听地址保存后需要重启生效；回调地址保存后立即用于新消息。</p>
        </div>

        <div class="panel account-panel">
          <div class="panel-head">
            <button id="accountActionBtn" class="primary">扫码登录</button>
          </div>
          <div id="accountState" class="account-state"></div>
          <div id="qrBlock" class="qr-block hidden">
            <img id="qrImage" alt="登录二维码" />
            <p id="qrHint" class="hint"></p>
          </div>
        </div>
      </div>

      <div class="right-col">
        <div class="panel send-panel">
          <div class="panel-head">
            <div>
              <h2>文本消息</h2>
            </div>
          </div>
          <div class="summary-grid">
            <div class="summary-item inline-summary">
              <span class="inline-label">账号 ID</span>
              <div id="sendAccountId" class="static-value">未登录</div>
            </div>
            <div class="summary-item inline-summary">
              <span class="inline-label">目标用户 ID</span>
              <div id="sendToUserId" class="static-value">等待收到一条消息</div>
            </div>
          </div>
          <p id="sendHint" class="hint">当前文本发送仅支持回复已经给你发过消息的用户。</p>
          <label class="field">
            <textarea id="sendText" rows="4" placeholder="输入一段文本，用于测试消息发送"></textarea>
          </label>
          <button id="sendBtn" class="primary wide">发送文本</button>
          <div class="media-composer">
            <div id="mediaFilePath" class="static-value">未选择文件</div>
            <div class="media-actions">
              <button id="chooseMediaBtn" class="ghost wide">选择媒体文件</button>
              <button id="sendMediaBtn" class="primary wide">发送媒体</button>
            </div>
            <label class="field">
              <textarea id="mediaCaption" rows="3" placeholder="可选：给媒体附带一段说明文字"></textarea>
            </label>
          </div>
        </div>

        <div class="panel dead-panel">
          <div class="panel-head">
            <div>
              <h2>Webhook 死信</h2>
            </div>
          </div>
          <div id="deadEmpty" class="empty">当前没有死信。</div>
          <div id="deadList" class="dead-list"></div>
        </div>

        <div class="panel events-panel">
          <div class="panel-head">
            <div>
              <h2>收发记录</h2>
            </div>
          </div>
          <div id="eventsEmpty" class="empty">暂无事件。完成收发后会显示在这里。</div>
          <div id="eventsList" class="events-list"></div>
        </div>
      </div>
    </section>
    <div id="toast" class="toast hidden"></div>
  </main>
`;

const els = {
  accountActionBtn: document.getElementById('accountActionBtn'),
  accountState: document.getElementById('accountState'),
  qrBlock: document.getElementById('qrBlock'),
  qrImage: document.getElementById('qrImage'),
  qrHint: document.getElementById('qrHint'),
  listenAddr: document.getElementById('listenAddr'),
  webhookUrl: document.getElementById('webhookUrl'),
  saveSettingsBtn: document.getElementById('saveSettingsBtn'),
  sendAccountId: document.getElementById('sendAccountId'),
  sendToUserId: document.getElementById('sendToUserId'),
  sendHint: document.getElementById('sendHint'),
  sendText: document.getElementById('sendText'),
  sendBtn: document.getElementById('sendBtn'),
  mediaFilePath: document.getElementById('mediaFilePath'),
  chooseMediaBtn: document.getElementById('chooseMediaBtn'),
  mediaCaption: document.getElementById('mediaCaption'),
  sendMediaBtn: document.getElementById('sendMediaBtn'),
  deadEmpty: document.getElementById('deadEmpty'),
  deadList: document.getElementById('deadList'),
  eventsEmpty: document.getElementById('eventsEmpty'),
  eventsList: document.getElementById('eventsList'),
  toast: document.getElementById('toast'),
};

let toastTimer = null;

function escapeHTML(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function showToast(message) {
  const text = message instanceof Error ? message.message : String(message);
  els.toast.textContent = text;
  els.toast.classList.remove('hidden');
  if (toastTimer) {
    clearTimeout(toastTimer);
  }
  toastTimer = setTimeout(() => {
    els.toast.classList.add('hidden');
  }, 2600);
}

function bjTime(value) {
  if (!value) return '-';
  const date = new Date(value);
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date).replace(/\//g, '-');
}

function compactValue(value, head = 18) {
  const text = String(value || '');
  if (!text) return '';
  if (text.length <= head) return text;
  return `${text.slice(0, head)}…`;
}

function shouldRenderEventItems(event) {
  const items = event.items || [];
  return items.length > 0 && (items.length > 1 || items.some(item => item.kind !== 'text'));
}

function renderEventItemsHTML(items) {
  return `
    <div class="event-items">
      ${items.map(renderEventItemHTML).join('')}
    </div>
  `;
}

function renderEventItemHTML(item) {
  const kind = escapeHTML(item.kind || 'unknown');
  const meta = [];
  if (item.text) {
    meta.push(`<div class="event-item-line">${escapeHTML(item.text)}</div>`);
  }
  if (item.file_name) {
    meta.push(`<div class="event-item-line">文件名：${escapeHTML(item.file_name)}</div>`);
  }
  if (item.file_len) {
    meta.push(`<div class="event-item-line">大小：${escapeHTML(item.file_len)}</div>`);
  }
  if (item.encode_type) {
    meta.push(`<div class="event-item-line">编码：${escapeHTML(item.encode_type)}</div>`);
  }
  if (item.media_encrypt_query_param) {
    meta.push(`<div class="event-item-line">媒体参数：<span class="code-chip" title="${escapeHTML(item.media_encrypt_query_param)}">${escapeHTML(compactValue(item.media_encrypt_query_param))}</span></div>`);
  }
  if (item.media_aes_key) {
    meta.push(`<div class="event-item-line">AES Key：<span class="code-chip" title="${escapeHTML(item.media_aes_key)}">${escapeHTML(compactValue(item.media_aes_key))}</span></div>`);
  }
  if (item.local_path) {
    meta.push(`<div class="event-item-line">本地文件：<span class="code-chip" title="${escapeHTML(item.local_path)}">${escapeHTML(compactValue(item.local_path, 42))}</span></div>`);
  }
  if (!meta.length) {
    meta.push('<div class="event-item-line muted-text">已收到该消息类型的结构化元数据。</div>');
  }
  return `
    <div class="event-item">
      <div class="event-item-head">
        <span class="pill muted">${kind}</span>
      </div>
      ${meta.join('')}
    </div>
  `;
}

function renderOverview(overview) {
  state.connected = overview.connected || null;
  if (document.activeElement !== els.listenAddr) {
    els.listenAddr.value = overview.settings.listen_addr || '';
  }
  if (document.activeElement !== els.webhookUrl) {
    els.webhookUrl.value = overview.settings.webhook_url || '';
  }

  if (overview.connected) {
    els.accountActionBtn.textContent = '退出登录';
    els.accountActionBtn.className = 'ghost';
    els.accountActionBtn.disabled = false;
    els.accountState.innerHTML = `
      <div class="identity-card">
        <div class="avatar">${overview.connected.account_id.slice(0, 2).toUpperCase()}</div>
        <div class="identity-copy">
          <h3>${overview.connected.account_id}</h3>
          <div class="identity-line inline-summary">
            <span class="identity-label inline-label">用户 ID</span>
            <div class="identity-user">
              <span class="identity-user-text" title="${overview.connected.ilink_user_id || '-'}">${overview.connected.ilink_user_id || '-'}</span>
              <button id="copyUserIdBtn" class="icon-btn" title="复制用户 ID" aria-label="复制用户 ID">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M9 9h9v11H9z"></path>
                  <path d="M6 5h9v2H8v9H6z"></path>
                </svg>
              </button>
            </div>
          </div>
          <p>最近入站：${bjTime(overview.connected.last_inbound_at)}</p>
        </div>
      </div>
    `;
    const copyBtn = document.getElementById('copyUserIdBtn');
    if (copyBtn && overview.connected.ilink_user_id) {
      copyBtn.addEventListener('click', async () => {
        await ClipboardSetText(overview.connected.ilink_user_id);
        showToast('用户 ID 已复制');
      });
    }
    els.qrBlock.classList.add('hidden');
    els.sendAccountId.textContent = overview.connected.account_id;
    syncSuggestedTarget();
  } else {
    els.accountActionBtn.textContent = '扫码登录';
    els.accountActionBtn.className = 'primary';
    els.accountActionBtn.disabled = Boolean(state.sessionId);
    els.accountState.innerHTML = ``;
    els.sendAccountId.textContent = '未登录';
    els.sendToUserId.textContent = '等待收到一条消息';
    state.recentInboundUserId = '';
    state.mediaFilePath = '';
    els.mediaCaption.value = '';
    renderMediaSelection();
    syncSuggestedTarget();
  }
  updateSendState();
}

function renderEvents() {
  els.eventsList.innerHTML = '';
  if (!state.events.length) {
    els.eventsEmpty.classList.remove('hidden');
    return;
  }
  els.eventsEmpty.classList.add('hidden');
  state.recentInboundUserId = '';
  for (let i = state.events.length - 1; i >= 0; i -= 1) {
    const item = state.events[i];
    if (!state.recentInboundUserId && item.direction === 'inbound' && item.from_user_id) {
      state.recentInboundUserId = item.from_user_id;
      break;
    }
  }
  syncSuggestedTarget();
  for (const item of state.events) {
    const row = document.createElement('article');
    row.className = 'event-row';
    const itemDetails = shouldRenderEventItems(item) ? renderEventItemsHTML(item.items || []) : '';
    row.innerHTML = `
      <header>
        <strong>${bjTime(item.created_at)}</strong>
        <span class="pill ${item.direction}">${item.direction}</span>
        <span class="pill muted">${item.event_type}</span>
      </header>
      <div class="body">${escapeHTML(item.body_text || '(空文本)')}</div>
      ${itemDetails}
    `;
    els.eventsList.appendChild(row);
  }
}

function renderDeadLetters() {
  els.deadList.innerHTML = '';
  if (!state.deadLetters.length) {
    els.deadEmpty.classList.remove('hidden');
    return;
  }
  els.deadEmpty.classList.add('hidden');
  for (const item of state.deadLetters) {
    const row = document.createElement('article');
    row.className = 'dead-row';
    row.innerHTML = `
      <header>
        <strong>${bjTime(item.dead_letter_at || item.updated_at || item.created_at)}</strong>
        <span class="pill dead">dead</span>
        <span class="pill muted">${item.event_type}</span>
      </header>
      <div class="body">${escapeHTML(item.body_text || '(空文本)')}</div>
      <p class="dead-meta">账号：${escapeHTML(item.account_id || '-')} | 来源：${escapeHTML(item.from_user_id || '-')} | 尝试：${item.attempt_count}/${item.max_attempts}</p>
      <p class="dead-error">${escapeHTML(item.last_error || '未知错误')}</p>
      <button class="ghost retry-dead-btn" data-id="${item.id}">重新投递</button>
    `;
    els.deadList.appendChild(row);
  }
  for (const button of els.deadList.querySelectorAll('.retry-dead-btn')) {
    button.addEventListener('click', async (event) => {
      const id = Number(event.currentTarget.dataset.id || '0');
      if (!id) return;
      event.currentTarget.disabled = true;
      try {
        await RetryDeadLetter(id);
        showToast('死信已重新入队');
        await loadDeadLetters();
      } catch (err) {
        showToast(err);
      } finally {
        event.currentTarget.disabled = false;
      }
    });
  }
}

function syncSuggestedTarget() {
  if (!state.connected) {
    els.sendToUserId.textContent = '等待收到一条消息';
    els.sendHint.textContent = '当前文本发送仅支持回复已经给你发过消息的用户。';
    updateSendState();
    return;
  }
  if (state.recentInboundUserId) {
    els.sendToUserId.textContent = state.recentInboundUserId;
    els.sendHint.textContent = `将优先回复最近来信用户：${state.recentInboundUserId}`;
  } else {
    els.sendToUserId.textContent = '等待收到一条消息';
    els.sendHint.textContent = '还没有可回复的来信用户。请先让对方发一条消息过来。';
  }
  updateSendState();
}

function updateSendState() {
  const canReply = Boolean(state.connected && state.recentInboundUserId);
  els.sendText.disabled = !canReply;
  els.sendBtn.disabled = !canReply || !els.sendText.value.trim();
  els.chooseMediaBtn.disabled = !canReply;
  els.mediaCaption.disabled = !canReply;
  els.sendMediaBtn.disabled = !canReply || !state.mediaFilePath;
}

function renderMediaSelection() {
  els.mediaFilePath.textContent = state.mediaFilePath || '未选择文件';
}

async function loadOverview() {
  const overview = await GetOverview();
  renderOverview(overview);
}

async function pollEvents() {
  const items = await ListEvents(state.eventAfterId, 100);
  if (items.length) {
    state.eventAfterId = items[items.length - 1].id;
    state.events.push(...items);
    if (state.events.length > 300) {
      state.events = state.events.slice(-300);
    }
    renderEvents();
  }
}

async function loadDeadLetters() {
  state.deadLetters = await ListDeadLetters(20);
  renderDeadLetters();
}

async function beginLogin() {
  const session = await StartLogin();
  state.sessionId = session.session_id;
  els.qrImage.src = await GetLoginQRCode(session.session_id);
  els.qrHint.textContent = '请使用微信扫描二维码完成连接。';
  els.qrBlock.classList.remove('hidden');
  els.accountActionBtn.textContent = '等待扫码...';
  els.accountActionBtn.disabled = true;
  if (state.polling) clearInterval(state.polling);
  state.polling = setInterval(checkLoginStatus, 3000);
}

async function checkLoginStatus() {
  if (!state.sessionId) return;
  const session = await GetLoginStatus(state.sessionId);
  if (session.status === 'confirmed') {
    clearInterval(state.polling);
    state.polling = null;
    state.sessionId = '';
    await loadOverview();
    showToast('登录成功');
    return;
  }
  if (session.status === 'expired' || session.status === 'error') {
    clearInterval(state.polling);
    state.polling = null;
    state.sessionId = '';
    els.qrHint.textContent = session.status === 'expired' ? '二维码已过期，请重新开始扫码登录。' : (session.error || '登录失败，请重试。');
    els.accountActionBtn.textContent = '扫码登录';
    els.accountActionBtn.disabled = false;
  }
}

async function saveSettings() {
  await SaveSettings(els.listenAddr.value.trim(), els.webhookUrl.value.trim());
  showToast('设置已保存。监听地址需要重启后生效。');
}

async function sendTextMessage() {
  const accountID = state.connected?.account_id || '';
  const toUserID = state.recentInboundUserId || '';
  const text = els.sendText.value.trim();
  if (!accountID || !toUserID || !text) {
    showToast('当前没有可回复的目标用户，或消息内容为空。');
    return;
  }
  await SendText(accountID, toUserID, text);
  els.sendText.value = '';
  updateSendState();
  showToast('消息已发送');
}

async function chooseMediaFile() {
  const filePath = await ChooseMediaFile();
  state.mediaFilePath = filePath || '';
  renderMediaSelection();
  updateSendState();
}

async function sendMediaMessage() {
  const accountID = state.connected?.account_id || '';
  const toUserID = state.recentInboundUserId || '';
  const filePath = state.mediaFilePath || '';
  const text = els.mediaCaption.value.trim();
  if (!accountID || !toUserID || !filePath) {
    showToast('当前没有可回复的目标用户，或尚未选择文件。');
    return;
  }
  await SendMedia(accountID, toUserID, filePath, text);
  state.mediaFilePath = '';
  els.mediaCaption.value = '';
  renderMediaSelection();
  updateSendState();
  showToast('媒体已发送');
}

async function logout() {
  if (!state.connected) return;
  await Logout(state.connected.account_id);
  state.connected = null;
  state.sessionId = '';
  els.qrBlock.classList.add('hidden');
  await loadOverview();
  showToast('当前账号已在本地退出登录');
}

els.accountActionBtn.addEventListener('click', () => {
  const action = state.connected ? logout : beginLogin;
  action().catch(err => showToast(err));
});
els.saveSettingsBtn.addEventListener('click', () => saveSettings().catch(err => showToast(err)));
els.sendBtn.addEventListener('click', () => sendTextMessage().catch(err => showToast(err)));
els.chooseMediaBtn.addEventListener('click', () => chooseMediaFile().catch(err => showToast(err)));
els.sendMediaBtn.addEventListener('click', () => sendMediaMessage().catch(err => showToast(err)));
els.sendText.addEventListener('input', () => updateSendState());

async function bootstrap() {
  renderMediaSelection();
  await loadOverview();
  await pollEvents();
  await loadDeadLetters();
  setInterval(() => pollEvents().catch(console.error), 3000);
  setInterval(() => loadDeadLetters().catch(console.error), 5000);
  setInterval(() => loadOverview().catch(console.error), 5000);
}

bootstrap().catch(err => showToast(err));
