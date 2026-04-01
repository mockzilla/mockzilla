import * as config from './config.js';
import * as commons from './commons.js';
import * as navi from './navi.js';
import * as services from './services.js';

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

// Go's time.Duration JSON-marshals as nanoseconds.
const formatDuration = (ns) => {
    const us = ns / 1e3;
    if (us < 1000) return `${Math.round(us)}µs`;
    const ms = us / 1e3;
    if (ms < 1000) return `${ms.toFixed(2)}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
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
    config.tabHistory.style.display = '';
    config.tabConfiguration.href = `#/configuration/${service}`;
    config.tabResources.classList.remove('active');
    config.tabHistory.classList.add('active');
    config.tabConfiguration.classList.remove('active');
};

const showDetail = (entry) => {
    const detail = document.getElementById('history-detail');
    detail.style.display = 'block';

    const req = entry.request;
    const resp = entry.response;

    // Update panel header with the URL
    const panelTitle = document.getElementById('history-detail-title');
    panelTitle.textContent = req ? decodeURIComponent(req.url) : 'Select an entry';

    // Details accordion
    const summaryHeader = document.getElementById('history-summary-header');
    summaryHeader.textContent = 'Details';

    const summaryContent = document.getElementById('history-summary-content');
    summaryContent.classList.remove('active');
    summaryHeader.classList.remove('expanded');

    if (!summaryHeader.hasAttribute('data-wired')) {
        summaryHeader.setAttribute('data-wired', '1');
        summaryHeader.addEventListener('click', () => {
            summaryContent.classList.toggle('active');
            summaryHeader.classList.toggle('expanded');
        });
    }

    const tbody = document.createElement('tbody');
    if (resp) {
        if (resp.statusCode) addMetaRow(tbody, 'Status', `${resp.statusCode}`);
        if (resp.duration) addMetaRow(tbody, 'Duration', formatDuration(resp.duration));
        if (resp.contentType) addMetaRow(tbody, 'Content-Type', resp.contentType);

        // Derive source from X-Cxs-Source header or isFromUpstream flag
        const sourceHeader = (resp.headers || [])
            .find(h => h.toLowerCase().startsWith('x-cxs-source:'));
        if (sourceHeader) {
            addMetaRow(tbody, 'Source', sourceHeader.split(': ')[1]);
        } else {
            addMetaRow(tbody, 'Source', resp.isFromUpstream ? 'upstream' : 'generated');
        }

        if (resp.upstreamURL) addMetaRow(tbody, 'Upstream URL', decodeURIComponent(resp.upstreamURL));
        if (resp.upstreamError) addMetaRow(tbody, 'Upstream Error', resp.upstreamError);
    }
    if (req && req.requestId) addMetaRow(tbody, 'Request ID', req.requestId);
    if (req && req.remoteAddr) addMetaRow(tbody, 'Remote', req.remoteAddr);
    if (entry.createdAt) {
        const d = new Date(entry.createdAt);
        const pad = (n) => String(n).padStart(2, '0');
        const ts = `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
        addMetaRow(tbody, 'Time', ts);
    }

    const table = document.createElement('table');
    table.className = 'response-headers-table';
    table.appendChild(tbody);
    summaryContent.innerHTML = '';
    summaryContent.appendChild(table);

    // Request headers
    const reqHeadersBody = document.getElementById('history-req-headers-body');
    reqHeadersBody.innerHTML = '';
    if (req && req.headers) {
        for (const h of req.headers) {
            const row = document.createElement('tr');
            const colonIdx = h.indexOf(': ');
            const name = colonIdx >= 0 ? h.substring(0, colonIdx) : h;
            const value = colonIdx >= 0 ? h.substring(colonIdx + 2) : '';
            const nameCell = document.createElement('td');
            nameCell.textContent = name;
            const valueCell = document.createElement('td');
            valueCell.textContent = value;
            row.append(nameCell, valueCell);
            reqHeadersBody.appendChild(row);
        }
    }

    // Request body
    const reqBodyContainer = document.getElementById('history-req-body-container');
    if (req && req.body && req.body.length > 0) {
        reqBodyContainer.style.display = 'block';
        const {text, mode} = decodeBody(req.body);
        const editor = commons.getCodeEditor('history-req-body', mode, {maxLines: Infinity});
        editor.setValue(text);
        editor.clearSelection();
        editor.setReadOnly(true);
    } else {
        reqBodyContainer.style.display = 'none';
    }

    // Response headers
    const respHeadersBody = document.getElementById('history-resp-headers-body');
    respHeadersBody.innerHTML = '';
    if (resp && resp.headers) {
        for (const h of resp.headers) {
            const row = document.createElement('tr');
            const colonIdx = h.indexOf(': ');
            const name = colonIdx >= 0 ? h.substring(0, colonIdx) : h;
            const value = colonIdx >= 0 ? h.substring(colonIdx + 2) : '';
            const nameCell = document.createElement('td');
            nameCell.textContent = name;
            const valueCell = document.createElement('td');
            valueCell.textContent = value;
            row.append(nameCell, valueCell);
            respHeadersBody.appendChild(row);
        }
    }

    // Response body
    const respBodyContainer = document.getElementById('history-resp-body-container');
    if (resp && resp.body && resp.body.length > 0) {
        respBodyContainer.style.display = 'block';
        const {text, mode} = decodeBody(resp.body);
        const editor = commons.getCodeEditor('history-resp-body', mode, {maxLines: Infinity});
        editor.setValue(text);
        editor.clearSelection();
        editor.setReadOnly(true);
    } else {
        respBodyContainer.style.display = 'none';
    }
};

const renderEntries = (items, service) => {
    const tbody = document.getElementById('history-table-body');
    tbody.innerHTML = '';

    if (!items || items.length === 0) {
        const row = document.createElement('tr');
        const cell = document.createElement('td');
        cell.colSpan = 6;
        cell.textContent = 'No history entries';
        cell.style.textAlign = 'center';
        cell.style.color = 'var(--text-muted)';
        row.appendChild(cell);
        tbody.appendChild(row);
        return;
    }

    // Show newest first
    const sorted = [...items].reverse();

    sorted.forEach((entry, i) => {
        const row = document.createElement('tr');
        row.id = `history-${entry.id}`;
        row.style.cursor = 'pointer';
        row.onclick = () => {
            navi.applySelection(`history-${entry.id}`, 'selected-resource');
            const detailUrl = `${config.historyUrl}?service=${encodeURIComponent(service)}&id=${encodeURIComponent(entry.id)}`;
            fetch(detailUrl)
                .then(res => res.json())
                .then(full => showDetail(full))
                .catch(err => console.error('Failed to fetch entry:', err));
        };

        const numCell = document.createElement('td');
        numCell.textContent = `${i + 1}`;
        row.appendChild(numCell);

        const methodCell = document.createElement('td');
        const method = entry.request ? entry.request.method : '';
        methodCell.textContent = method;
        methodCell.className = `fixed-resource-method ${method.toLowerCase()}`;
        row.appendChild(methodCell);

        const pathCell = document.createElement('td');
        pathCell.className = 'fixed-resource-path';
        const pathSpan = document.createElement('span');
        const rawPath = entry.resource || (entry.request ? entry.request.url : '');
        pathSpan.textContent = decodeURIComponent(rawPath);
        pathCell.appendChild(pathSpan);
        pathCell.title = pathSpan.textContent;
        row.appendChild(pathCell);

        const statusCell = document.createElement('td');
        if (entry.response) {
            statusCell.textContent = entry.response.statusCode;
            statusCell.className = `history-status ${statusClass(entry.response.statusCode)}`;
        }
        row.appendChild(statusCell);

        const durationCell = document.createElement('td');
        durationCell.className = 'history-duration';
        if (entry.response && entry.response.duration) {
            durationCell.textContent = formatDuration(entry.response.duration);
        }
        row.appendChild(durationCell);

        const timeCell = document.createElement('td');
        timeCell.textContent = formatTime(entry.createdAt);
        timeCell.className = 'history-time';
        row.appendChild(timeCell);

        tbody.appendChild(row);
    });
};

const fetchAndRender = (service) => {
    const historyApiUrl = `${config.historyUrl}?service=${encodeURIComponent(service)}`;
    return fetch(historyApiUrl)
        .then(res => res.json())
        .then(data => {
            renderEntries(data.items, service);
        })
        .catch(err => {
            console.error('Failed to fetch history:', err);
        });
};

export const show = (match) => {
    const {name} = match.params;
    const service = name;

    navi.resetContents();
    services.show();

    navi.applySelection(`service-${service}`, 'selected-service');

    let displayName = service;
    if (displayName === '.root') {
        displayName = 'Root level';
    } else {
        displayName = `/${displayName}`;
    }
    config.contentTitleEl.innerHTML = `${displayName} history`;

    showTabs(service);
    config.historyContainer.style.display = 'block';

    fetchAndRender(service);

    document.getElementById('history-refresh').onclick = () => fetchAndRender(service);
    document.getElementById('history-clear').onclick = () => {
        if (!confirm(`Clear history for ${displayName}?`)) return;
        const historyApiUrl = `${config.historyUrl}?service=${encodeURIComponent(service)}`;
        fetch(historyApiUrl, {method: 'DELETE'})
            .then(() => fetchAndRender(service))
            .catch(err => console.error('Failed to clear history:', err));
    };
};
