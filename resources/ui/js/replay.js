import * as config from './config.js';
import * as commons from './commons.js';
import * as navi from './navi.js';
import * as services from './services.js';

let selectedKey = null;

// Go's []byte JSON-marshals as a base64 string. Decode it back to text,
// then detect whether the result is valid JSON for syntax highlighting.
const decodeBody = (raw) => {
    let text;
    try {
        text = atob(raw);
    } catch {
        text = String(raw);
    }
    let mode = 'text';
    try {
        text = JSON.stringify(JSON.parse(text), null, 2);
        mode = 'json';
    } catch {}
    return {text, mode};
};

const formatTime = (dateStr) => {
    const now = new Date();
    const date = new Date(dateStr);
    const diffSec = Math.floor((now - date) / 1000);
    if (diffSec < 60) return `${diffSec}s ago`;
    if (diffSec < 300) return `${Math.floor(diffSec / 60)}m ago`;
    return date.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit', second: '2-digit'});
};

const statusClass = (code) => {
    if (code >= 200 && code < 300) return 'status-2xx';
    if (code >= 400 && code < 500) return 'status-4xx';
    if (code >= 500) return 'status-5xx';
    return '';
};

// matchValues is the field=value map that forms the recording's key,
// e.g. {"body:reference": "abc123"}. Render it compactly for the list.
const formatMatch = (mv) => {
    if (!mv) return '';
    return Object.entries(mv).map(([k, v]) => `${k}=${v}`).join(', ');
};

const addMetaRow = (tbody, label, value) => {
    const row = document.createElement('tr');
    const nameCell = document.createElement('td');
    nameCell.textContent = label;
    const valueCell = document.createElement('td');
    valueCell.textContent = value;
    row.append(nameCell, valueCell);
    tbody.appendChild(row);
};

const showTabs = (service) => {
    config.serviceTabs.style.display = 'flex';
    config.tabResources.href = `#/services/${service}`;
    config.tabHistory.href = `#/history/${service}`;
    config.tabHistory.style.display = config.historyEnabled ? '' : 'none';
    config.tabReplay.href = `#/replay/${service}`;
    config.tabReplay.style.display = config.replayEnabled ? '' : 'none';
    config.tabConfiguration.href = `#/configuration/${service}`;
    config.tabConfiguration.style.display = config.configEnabled ? '' : 'none';
    config.tabResources.classList.remove('active');
    config.tabHistory.classList.remove('active');
    config.tabReplay.classList.add('active');
    config.tabConfiguration.classList.remove('active');
};

const showDetail = (rec, key) => {
    selectedKey = key;

    const detail = document.getElementById('replay-detail');
    detail.style.display = 'block';
    const hint = document.getElementById('right-pane-hint');
    if (hint) hint.style.display = 'none';

    const panelTitle = document.getElementById('replay-detail-title');
    panelTitle.textContent = decodeURIComponent(rec.resource || rec.path || 'Recording');

    document.getElementById('replay-delete').style.display = '';

    const summaryContent = document.getElementById('replay-summary-content');
    summaryContent.innerHTML = '';

    const tbody = document.createElement('tbody');
    if (rec.method) addMetaRow(tbody, 'Method', rec.method);
    if (rec.path) addMetaRow(tbody, 'Path', decodeURIComponent(rec.path));
    if (rec.resource) addMetaRow(tbody, 'Resource', decodeURIComponent(rec.resource));
    if (rec.statusCode) addMetaRow(tbody, 'Status', `${rec.statusCode}`);
    addMetaRow(tbody, 'Recorded from', rec.isFromUpstream ? 'upstream' : 'generated');
    if (rec.contentType) addMetaRow(tbody, 'Content-Type', rec.contentType);
    if (rec.createdAt) {
        const d = new Date(rec.createdAt);
        const pad = (n) => String(n).padStart(2, '0');
        const ts = `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
        addMetaRow(tbody, 'Time', ts);
    }
    const table = document.createElement('table');
    table.className = 'history-summary-table';
    table.appendChild(tbody);
    summaryContent.appendChild(table);

    // Match values: the fields that form this recording's key.
    const mv = rec.matchValues || {};
    const mvKeys = Object.keys(mv);
    if (mvKeys.length) {
        const heading = document.createElement('div');
        heading.className = 'replay-match-heading';
        heading.textContent = 'Match values';
        summaryContent.appendChild(heading);

        const mvBody = document.createElement('tbody');
        for (const k of mvKeys) addMetaRow(mvBody, k, `${mv[k]}`);
        const mvTable = document.createElement('table');
        mvTable.className = 'history-summary-table';
        mvTable.appendChild(mvBody);
        summaryContent.appendChild(mvTable);
    }

    // Request body
    if (rec.requestBody && rec.requestBody.length > 0) {
        const {text, mode} = decodeBody(rec.requestBody);
        const editor = commons.getCodeEditor('replay-req-body', mode);
        editor.setValue(text);
        editor.clearSelection();
        editor.setReadOnly(true);
    } else {
        const editor = commons.getCodeEditor('replay-req-body', 'text');
        editor.setValue('(no request body)');
        editor.clearSelection();
        editor.setReadOnly(true);
    }

    // Response headers (stored as an object map)
    const respHeadersBody = document.getElementById('replay-resp-headers-body');
    respHeadersBody.innerHTML = '';
    const headers = rec.headers || {};
    for (const name of Object.keys(headers)) {
        const row = document.createElement('tr');
        const nameCell = document.createElement('td');
        nameCell.textContent = name;
        const valueCell = document.createElement('td');
        valueCell.textContent = headers[name];
        row.append(nameCell, valueCell);
        respHeadersBody.appendChild(row);
    }

    // Response body
    if (rec.data && rec.data.length > 0) {
        const {text, mode} = decodeBody(rec.data);
        const editor = commons.getCodeEditor('replay-resp-body', mode);
        editor.setValue(text);
        editor.clearSelection();
        editor.setReadOnly(true);
    } else {
        const editor = commons.getCodeEditor('replay-resp-body', 'text');
        editor.setValue('(no response body)');
        editor.clearSelection();
        editor.setReadOnly(true);
    }
};

const renderEntries = (items, service, truncated) => {
    const tbody = document.getElementById('replay-table-body');
    tbody.innerHTML = '';

    if (!items || items.length === 0) {
        const row = document.createElement('tr');
        const cell = document.createElement('td');
        cell.colSpan = 6;
        cell.textContent = 'No replay recordings';
        cell.style.textAlign = 'center';
        cell.style.color = 'var(--text-muted)';
        row.appendChild(cell);
        tbody.appendChild(row);
        return;
    }

    // Server already returns newest-first.
    items.forEach((entry, i) => {
        const row = document.createElement('tr');
        row.id = `replay-${entry.key}`;
        row.style.cursor = 'pointer';
        row.onclick = () => {
            navi.applySelection(`replay-${entry.key}`, 'selected-resource');
            const detailUrl = `${config.replayUrl}?service=${encodeURIComponent(service)}&key=${encodeURIComponent(entry.key)}`;
            fetch(detailUrl)
                .then(res => res.json())
                .then(full => showDetail(full, entry.key))
                .catch(err => console.error('Failed to fetch recording:', err));
        };

        const numCell = document.createElement('td');
        numCell.textContent = `${i + 1}`;
        row.appendChild(numCell);

        const methodCell = document.createElement('td');
        const method = entry.method || '';
        methodCell.textContent = method;
        methodCell.className = `fixed-resource-method ${method.toLowerCase()}`;
        row.appendChild(methodCell);

        const pathCell = document.createElement('td');
        pathCell.className = 'fixed-resource-path';
        const pathSpan = document.createElement('span');
        const rawPath = entry.resource || entry.path || '';
        pathSpan.textContent = decodeURIComponent(rawPath);
        pathCell.appendChild(pathSpan);
        pathCell.title = pathSpan.textContent;
        row.appendChild(pathCell);

        const statusCell = document.createElement('td');
        if (entry.statusCode) {
            statusCell.textContent = entry.statusCode;
            statusCell.className = `history-status ${statusClass(entry.statusCode)}`;
        }
        row.appendChild(statusCell);

        const matchCell = document.createElement('td');
        matchCell.className = 'replay-match';
        const matchText = formatMatch(entry.matchValues);
        matchCell.textContent = matchText;
        matchCell.title = matchText;
        row.appendChild(matchCell);

        const timeCell = document.createElement('td');
        timeCell.textContent = formatTime(entry.createdAt);
        timeCell.className = 'history-time';
        row.appendChild(timeCell);

        tbody.appendChild(row);
    });

    if (truncated) {
        const note = document.createElement('tr');
        const cell = document.createElement('td');
        cell.colSpan = 6;
        cell.textContent = `Showing newest ${items.length} recordings`;
        cell.style.textAlign = 'center';
        cell.style.color = 'var(--text-muted)';
        cell.style.fontSize = '12px';
        note.appendChild(cell);
        tbody.appendChild(note);
    }
};

const fetchAndRender = (service) => {
    const url = `${config.replayUrl}?service=${encodeURIComponent(service)}`;
    return fetch(url)
        .then(res => res.json())
        .then(data => renderEntries(data.items, service, data.truncated))
        .catch(err => console.error('Failed to fetch replay recordings:', err));
};

export const show = (match) => {
    const {name} = match.params;
    const service = name;

    selectedKey = null;

    navi.resetContents();
    navi.setActiveView('replay');
    services.show(service);

    let displayName = service;
    if (displayName === '.root') {
        displayName = 'Root level';
    } else {
        displayName = `/${displayName}`;
    }
    config.contentTitleEl.innerHTML = `${displayName} replay`;

    showTabs(service);
    config.fixedServiceContainer.style.display = 'block';
    document.getElementById('replay-table-list').style.display = '';
    document.getElementById('replay-actions').style.display = 'flex';
    document.getElementById('resource-panel-title').style.display = 'none';
    document.getElementById('replay-detail-title').style.display = '';
    document.getElementById('replay-tabs').style.display = 'flex';

    fetchAndRender(service);

    document.getElementById('replay-refresh').onclick = () => fetchAndRender(service);
    document.getElementById('replay-clear').onclick = () => {
        if (!confirm(`Clear all replay recordings for ${displayName}?`)) return;
        const url = `${config.replayUrl}?service=${encodeURIComponent(service)}`;
        fetch(url, {method: 'DELETE'})
            .then(() => fetchAndRender(service))
            .catch(err => console.error('Failed to clear replay recordings:', err));
    };
    document.getElementById('replay-delete').onclick = () => {
        if (!selectedKey) return;
        if (!confirm('Delete this replay recording?')) return;
        const url = `${config.replayUrl}?service=${encodeURIComponent(service)}&key=${encodeURIComponent(selectedKey)}`;
        fetch(url, {method: 'DELETE'})
            .then(() => {
                selectedKey = null;
                document.getElementById('replay-detail').style.display = 'none';
                document.getElementById('replay-delete').style.display = 'none';
                fetchAndRender(service);
            })
            .catch(err => console.error('Failed to delete recording:', err));
    };
};
