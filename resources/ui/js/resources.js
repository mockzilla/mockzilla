import * as config from './config.js';
import * as commons from './commons.js';
import * as validators from './validators.js';
import * as navi from "./navi.js";
import * as services from "./services.js";
import * as presets from "./presets.js";
import { joinServiceUrl } from "./url.js";

export const addCustomHeaderRow = (enabled = true, name = '', value = '', visible = true) => {
    const container = document.getElementById('custom-headers-rows');
    const row = document.createElement('div');
    row.className = 'config-override-row custom-header-row';

    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.checked = enabled;

    const nameInput = document.createElement('input');
    nameInput.type = 'text';
    nameInput.className = 'custom-header-name';
    nameInput.placeholder = 'Header name';
    nameInput.value = name;

    const valueInput = document.createElement('input');
    valueInput.type = visible ? 'text' : 'password';
    valueInput.className = 'custom-header-value';
    valueInput.placeholder = 'Header value';
    valueInput.value = value;

    const eyeBtn = document.createElement('button');
    eyeBtn.type = 'button';
    eyeBtn.className = 'btn-eye';
    eyeBtn.innerHTML = visible
        ? '<i class="fa-solid fa-eye"></i>'
        : '<i class="fa-solid fa-eye-slash"></i>';
    eyeBtn.title = 'Toggle visibility';
    eyeBtn.addEventListener('click', () => {
        const isVisible = valueInput.type === 'text';
        valueInput.type = isVisible ? 'password' : 'text';
        eyeBtn.innerHTML = isVisible
            ? '<i class="fa-solid fa-eye-slash"></i>'
            : '<i class="fa-solid fa-eye"></i>';
    });

    const removeBtn = document.createElement('button');
    removeBtn.type = 'button';
    removeBtn.className = 'btn-remove';
    removeBtn.innerHTML = '<i class="fa-solid fa-xmark"></i>';
    removeBtn.title = 'Remove header';
    removeBtn.addEventListener('click', () => row.remove());

    row.append(cb, nameInput, valueInput, eyeBtn, removeBtn);
    container.appendChild(row);
};

export const getCustomHeaders = () => {
    const headers = {};
    const rows = document.querySelectorAll('#custom-headers-rows .custom-header-row');
    for (const row of rows) {
        const cb = row.querySelector('input[type="checkbox"]');
        if (!cb || !cb.checked) continue;
        const name = row.querySelector('.custom-header-name').value.trim();
        const value = row.querySelector('.custom-header-value').value;
        if (name) headers[name] = value;
    }
    return headers;
};

// Collect enabled config override headers from the UI
const getConfigOverrideHeaders = () => {
    const headers = {};

    // Upstream URL override
    const upstreamEnabled = document.getElementById('override-upstream-enabled');
    if (upstreamEnabled && upstreamEnabled.checked) {
        const upstreamUrl = document.getElementById('override-upstream-url');
        // Always send the header when checked (empty string disables upstream)
        headers['X-Mockzilla-Upstream-Url'] = upstreamUrl ? upstreamUrl.value : '';
    }

    // Cache Requests override
    const cacheEnabled = document.getElementById('override-cache-enabled');
    if (cacheEnabled && cacheEnabled.checked) {
        const cacheValue = document.getElementById('override-cache-value');
        if (cacheValue) {
            headers['X-Mockzilla-Cache-Requests'] = cacheValue.value;
        }
    }

    // Latency override
    const latencyEnabled = document.getElementById('override-latency-enabled');
    if (latencyEnabled && latencyEnabled.checked) {
        const latencyValue = document.getElementById('override-latency-value');
        if (latencyValue && latencyValue.value) {
            headers['X-Mockzilla-Latency'] = latencyValue.value;
        }
    }

    // Replay override
    const replayEnabled = document.getElementById('override-replay-enabled');
    if (replayEnabled && replayEnabled.checked) {
        const replayValue = document.getElementById('override-replay-value');
        // Always send the header when checked (empty value uses match fields from config)
        headers['X-Mockzilla-Replay'] = replayValue ? replayValue.value : '';
    }

    // Validate Request override
    const validateReqEnabled = document.getElementById('override-validate-request-enabled');
    if (validateReqEnabled && validateReqEnabled.checked) {
        const validateReqValue = document.getElementById('override-validate-request-value');
        if (validateReqValue) {
            headers['X-Mockzilla-Validate-Request'] = validateReqValue.value;
        }
    }

    // Validate Response override
    const validateRespEnabled = document.getElementById('override-validate-response-enabled');
    if (validateRespEnabled && validateRespEnabled.checked) {
        const validateRespValue = document.getElementById('override-validate-response-value');
        if (validateRespValue) {
            headers['X-Mockzilla-Validate-Response'] = validateRespValue.value;
        }
    }

    // Validate Verbose override
    const validateVerboseEnabled = document.getElementById('override-validate-verbose-enabled');
    if (validateVerboseEnabled && validateVerboseEnabled.checked) {
        const validateVerboseValue = document.getElementById('override-validate-verbose-value');
        if (validateVerboseValue) {
            headers['X-Mockzilla-Validate-Verbose'] = validateVerboseValue.value;
        }
    }

    // Validate Timeout override (Go duration string, e.g. "5s", "500ms")
    const validateTimeoutEnabled = document.getElementById('override-validate-timeout-enabled');
    if (validateTimeoutEnabled && validateTimeoutEnabled.checked) {
        const validateTimeoutValue = document.getElementById('override-validate-timeout-value');
        if (validateTimeoutValue && validateTimeoutValue.value.trim() !== '') {
            headers['X-Mockzilla-Validate-Timeout'] = validateTimeoutValue.value.trim();
        }
    }

    return headers;
};

const buildStaticBadge = () => {
    const badge = document.createElement('span');
    badge.className = 'static-badge';
    badge.title = 'Response served from a static file overlay, not the OpenAPI generator';
    badge.innerHTML = `<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8.5 1.5a.5.5 0 0 0-1 0v.793L5.354 4.439a.5.5 0 0 0-.146.354v3.207l-2.061 2.06A.5.5 0 0 0 3 10.207V11h4.5v3.5a.5.5 0 0 0 1 0V11H13v-.793a.5.5 0 0 0-.146-.354L10.793 7.793V4.793a.5.5 0 0 0-.147-.354L8.5 2.293V1.5Z"/></svg><span>static</span>`;
    return badge;
};

export const show = match => {
    const {name} = match.params;
    const service = name;

    // Get ix from query parameter
    // Try both window.location.search (for ?ix=1 after hash) and hash query string (for #/path?ix=1)
    let ix = null;

    // First try from actual URL query string (http://...?ix=1)
    const urlParams = new URLSearchParams(window.location.search);
    ix = urlParams.get('ix');

    // If not found, try from hash query string (#/path?ix=1)
    if (!ix) {
        const hashParts = window.location.hash.split('?');
        if (hashParts.length > 1) {
            const hashParams = new URLSearchParams(hashParts[1]);
            ix = hashParams.get('ix');
        }
    }

    const serviceResourcesUrl = `${config.serviceUrl}/${service}`;

    // If ix is provided, skip fetching routes and go directly to generate
    if (ix !== null && ix !== undefined) {
        // Get endpoint info from the table (which should already be populated)
        const row = document.getElementById(`resource-${ix}`);
        if (row) {
            // Extract path and method from the table row
            const pathCell = row.querySelector('.fixed-resource-path span');
            const methodCell = row.querySelector('.fixed-resource-method');

            if (pathCell && methodCell) {
                const path = pathCell.textContent;
                const method = methodCell.textContent.toLowerCase();

                navi.applySelection(`resource-${ix}`, 'selected-resource');
                generateResult(service, ix - 1, path, method);
                return;
            }
        }
        // If row not found, fall through to fetch routes
    }

    navi.resetContents();
    navi.setActiveView('resources');
    services.show(service);

    let displayName = service;
    if (displayName === `.root`) {
        displayName = `Root level`
    } else {
        displayName = `/${displayName}`
    }
    config.contentTitleEl.innerHTML = `${displayName} resources`;

    config.serviceTabs.style.display = 'flex';
    config.tabResources.href = `#/services/${service}`;
    config.tabHistory.href = `#/history/${service}`;
    config.tabHistory.style.display = config.historyEnabled ? '' : 'none';
    config.tabReplay.href = `#/replay/${service}`;
    config.tabReplay.style.display = config.replayEnabled ? '' : 'none';
    config.tabConfiguration.href = `#/configuration/${service}`;
    config.tabConfiguration.style.display = config.configEnabled ? '' : 'none';
    config.tabResources.classList.add('active');
    config.tabHistory.classList.remove('active');
    config.tabReplay.classList.remove('active');
    config.tabConfiguration.classList.remove('active');

    fetch(serviceResourcesUrl)
        .then(res => res.json())
        .then(data => {
            if (data.success === false) {
                commons.showSuccessOrError(data.message, data.success);
                return;
            }
            navi.applySelection(`service-${service}`, 'selected-service');

            const endpoints = data['endpoints'];
            const table = document.getElementById('fixed-service-table-body');
            let i = 0;
            const mapped = {};

            for (const { method, path, contentType, isStatic } of endpoints) {
                const num = i + 1;
                const row = document.createElement('tr');
                row.id = `resource-${num}`;
                row.style.cursor = 'pointer';
                row.onclick = () => { window.location.hash = `#/services/${service}?ix=${num}`; };

                const cell1 = document.createElement('td');
                cell1.textContent = `${num}`;
                cell1.className = 'fixed-resource-num';
                row.appendChild(cell1);

                const methodCell = document.createElement('td');
                methodCell.innerHTML = `${method.toUpperCase()}`;
                methodCell.className = `fixed-resource-method ${method.toLowerCase()}`;
                row.appendChild(methodCell);

                const pathCell = document.createElement('td');
                pathCell.className = `fixed-resource-path`;
                pathCell.title = path;
                const pathSpan = document.createElement('span');
                pathSpan.textContent = path;
                pathCell.appendChild(pathSpan);
                if (isStatic) {
                    pathCell.appendChild(buildStaticBadge());
                }
                row.appendChild(pathCell);

                table.appendChild(row);
                i += 1;
            }
            config.fixedServiceContainer.style.display = 'block';
            document.getElementById('fixed-service-table-list').style.display = '';

            // If ix is present, generate the resource
            if (ix !== null && ix !== undefined) {
                navi.applySelection(`resource-${ix}`, 'selected-resource');
                generateResult(service, ix - 1, endpoints[ix - 1].path, endpoints[ix - 1].method);
            }
        });
}

export const generateResult = (service, ix, path, method) => {
    const onDone = () => {
        config.generatorCont.style.display = 'block';
        config.resourceRefreshBtn.onclick = () => generateResult(service, ix, path, method);
        config.resourceRefreshBtn.style.display = 'inline';
        const tabs = document.getElementById('resource-tabs');
        if (tabs) tabs.style.display = 'flex';
        const hint = document.getElementById('right-pane-hint');
        if (hint) hint.style.display = 'none';
    }
    commons.hideMessage();
    presets.initIfNeeded();

    let replacements = null;
    const replacementsEditor = commons.getCodeEditor(`context-replacements`, `yaml`);
    const yamlContent = replacementsEditor.getValue();
    if (yamlContent) {
        const yamlObject = jsyaml.load(yamlContent);
        const jsonContent = JSON.stringify(yamlObject, null, 2);
        replacements = validators.fixAndValidateJSON(jsonContent);
    }
    const customHeaders = getCustomHeaders();
    const hasCustomHeaders = Object.keys(customHeaders).length > 0;

    document.getElementById(`resource-edit-container`).style.display = 'none';

    // Use .root as-is for the generate endpoint (backend will convert it)
    const generateUrl = `${config.serviceUrl}/${service}`;
    const payload = {
        path: path,
        method: method,
        context: replacements,
    };

    fetch(generateUrl, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    })
        .then(async res => {
            if (res.status === 500) {
                commons.showError(await res.text() || `Internal server error`);
                return;
            }
            return res;
        })
        .then(res => res && res.json())
        .then(async res => {
            if (!res) {
                console.log(`No response`);
                return;
            }

            // Populate the name → prefix map before we build URLs. On a
            // deep link straight to #/services/<name>?ix=N we may reach
            // this point before services.show() has fetched the list,
            // in which case getServicePrefix returns undefined and the
            // call below would hit /<service> (the folder-snake-cased
            // name) instead of the actual mount.
            await services.ensureLoaded();

            // Clear previous data first
            document.getElementById('request-path').innerHTML = '';
            document.getElementById('request-path-container').style.display = 'none';
            document.getElementById('request-body-container').style.display = 'none';
            document.getElementById('response-body-container').style.display = 'none';
            document.getElementById('response-headers-container').style.display = 'none';

            let reqPath = res["path"];
            if (!reqPath) {
                commons.showError('No path returned from server. The request may have failed.');
                return;
            }

            // Update panel header with the generated URL
            document.getElementById('resource-panel-title').textContent = decodeURIComponent(reqPath);
            const reqContentType = res["contentType"];
            const reqBody = res["body"];
            const reqHeaders = res["headers"] || {};

            let formattedBody = ``;
            let reqBodyString = null;
            if (reqBody !== undefined && reqBody !== null) {
                if (reqContentType === `application/json`) {
                    formattedBody = JSON.stringify(reqBody, null, 2);
                    reqBodyString = JSON.stringify(reqBody);
                } else if (typeof reqBody === 'string') {
                    // Handle non-JSON body (e.g., form-urlencoded, plain text)
                    formattedBody = reqBody;
                    reqBodyString = reqBody;
                }
            }

            if (formattedBody.length) {
                document.getElementById('request-body-container').style.display = 'block';
                // Use 'text' mode for non-JSON content types
                const editorMode = reqContentType === 'application/json' ? 'json' : 'text';
                const reqView = commons.getCodeEditor(`request-body`, editorMode);
                reqView.setValue(formattedBody);
                reqView.clearSelection();
                reqView.setReadOnly(true);
            }

            // The server-reported prefix is the source of truth, including
            // when `--mount` overrides a root-named service. joinServiceUrl
            // collapses the "/" prefix (root mount) and any trailing slash
            // from a CLI mount value so the join with reqPath stays clean.
            const lookupName = service === '.root' ? '' : service;
            const rawPrefix = services.getServicePrefix(lookupName) ?? (service === '.root' ? '' : `/${service}`);
            const fullUrl = joinServiceUrl(config.baseUrl, rawPrefix, reqPath);

            // Stash everything renderCurl needs so override/custom-header
            // changes can re-render without re-fetching the payload.
            lastCurlState = {
                method,
                fullUrl,
                reqContentType,
                reqBodyString,
                generatedHeaders: reqHeaders,
                exampleCurl: res.request?.examples?.curl,
            };
            renderCurl(lastCurlState);

            // Make actual API call to get response
            if (reqPath) {
                // Reuse the same join so the live request and the displayed
                // curl command stay in lockstep.
                const apiUrl = fullUrl;
                const fetchOptions = {
                    method: method.toUpperCase(),
                    headers: { ...reqHeaders }
                };

                // Mark request as coming from the UI
                fetchOptions.headers['X-Mockzilla-Source'] = 'ui';

                // Apply config overrides from UI
                const overrideHeaders = getConfigOverrideHeaders();
                Object.assign(fetchOptions.headers, overrideHeaders);

                // Apply custom headers
                if (hasCustomHeaders) {
                    for (const [headerName, headerValue] of Object.entries(customHeaders)) {
                        fetchOptions.headers[headerName] = String(headerValue);
                    }
                }

                // Pass context replacements via header for response generation
                if (replacements) {
                    fetchOptions.headers['X-Mockzilla-Context'] = btoa(JSON.stringify(replacements));
                }

                // Tell upstream middleware which headers to forward (everything else is browser noise)
                const upstreamHeaders = Object.keys({...reqHeaders, ...customHeaders});
                if (upstreamHeaders.length > 0) {
                    fetchOptions.headers['X-Mockzilla-Upstream-Headers'] = upstreamHeaders.join(',');
                }

                if (reqContentType) {
                    fetchOptions.headers['Content-Type'] = reqContentType;
                }

                if (reqBodyString && method.toLowerCase() !== 'get') {
                    fetchOptions.body = reqBodyString;
                }

                fetch(apiUrl, fetchOptions)
                .then(response => {
                    const responseContentType = response.headers.get('Content-Type');
                    const headers = {};
                    response.headers.forEach((value, name) => {
                        headers[name] = value;
                    });
                    return response.text().then(text => ({
                        status: response.status,
                        contentType: responseContentType,
                        headers: headers,
                        body: text
                    }));
                })
                .then(responseData => {
                    console.log('API Response:', responseData);

                    // Display response in code editor
                    let formattedResponse = responseData.body;
                    if (responseData.contentType && responseData.contentType.includes('application/json')) {
                        try {
                            // Check if body is already an object or a string
                            const jsonObject = typeof responseData.body === 'string'
                                ? JSON.parse(responseData.body)
                                : responseData.body;
                            formattedResponse = JSON.stringify(jsonObject, null, 2);
                        } catch (e) {
                            console.error('Failed to parse JSON response:', e);
                        }
                    }

                    const statusBadge = document.getElementById('response-status-badge');
                    statusBadge.textContent = responseData.status;
                    statusBadge.className = 'response-status-badge status-' + Math.floor(responseData.status / 100) + 'xx';

                    document.getElementById('response-body-container').style.display = 'block';
                    const respContentType = responseData.contentType || '';
                    const respEditorMode = respContentType.includes('application/json') ? 'json' : 'text';
                    const responseView = commons.getCodeEditor(`response-body`, respEditorMode);
                    responseView.setValue(formattedResponse);
                    responseView.clearSelection();
                    responseView.setReadOnly(true);

                    const headerEntries = Object.entries(responseData.headers || {});
                    if (headerEntries.length > 0) {
                        const tbody = document.getElementById('response-headers-body');
                        tbody.innerHTML = '';
                        for (const [name, value] of headerEntries) {
                            const row = document.createElement('tr');
                            const nameCell = document.createElement('td');
                            nameCell.textContent = name;
                            const valueCell = document.createElement('td');
                            valueCell.textContent = value;
                            row.append(nameCell, valueCell);
                            tbody.appendChild(row);
                        }
                        document.getElementById('response-headers-container').style.display = 'block';
                    }
                })
                .catch(error => {
                    console.error('API call failed:', error);
                    const responseView = commons.getCodeEditor(`response-body`, `json`);
                    responseView.setValue(`Error: ${error.message}`);
                    responseView.clearSelection();
                    responseView.setReadOnly(true);
                });
            }

        }).then(onDone);
}

document.getElementById('custom-header-add')
    .addEventListener('click', () => addCustomHeaderRow(true, '', '', true));

// Last rendered cURL state. Stored so override/custom-header changes can
// re-render the block without re-fetching the generated payload. Cleared
// implicitly when a new endpoint is generated (state is overwritten).
let lastCurlState = null;

// renderCurl writes the cURL block from a base state (URL, method,
// content-type, generated body) plus the live values of custom and
// override headers. Called both from generateResult and from change
// listeners so the cURL stays in lockstep with override toggles.
const renderCurl = (state) => {
    if (!state) return;
    const block = document.getElementById('example-curl');
    if (!block) return;

    let text = `curl --request ${state.method.toUpperCase()} \\\n'${state.fullUrl}'`;
    if (state.reqContentType) {
        text += ` \\\n--header 'Content-Type: ${state.reqContentType}'`;
    }
    for (const [name, value] of Object.entries(state.generatedHeaders || {})) {
        if (name.toLowerCase() === 'content-type') continue;
        text += ` \\\n--header '${name}: ${value}'`;
    }
    for (const [name, value] of Object.entries(getCustomHeaders())) {
        text += ` \\\n--header '${name}: ${value}'`;
    }
    for (const [name, value] of Object.entries(getConfigOverrideHeaders())) {
        text += ` \\\n--header '${name}: ${value}'`;
    }
    if (state.reqBodyString && state.method.toLowerCase() !== 'get') {
        text += ` \\\n--data '${state.reqBodyString.replace(/'/g, "\\'")}'`;
    }
    if (state.exampleCurl) {
        text += ` \\\n${state.exampleCurl}`;
    }
    block.textContent = text;
};

// Wire live-update listeners. Config override controls are static IDs;
// custom headers are dynamic so we delegate to the rows container so new
// rows (added after page load) also trigger re-renders.
const wireCurlLiveUpdate = () => {
    const overrideIds = [
        'override-upstream-enabled', 'override-upstream-url',
        'override-cache-enabled', 'override-cache-value',
        'override-latency-enabled', 'override-latency-value',
        'override-replay-enabled', 'override-replay-value',
        'override-validate-request-enabled', 'override-validate-request-value',
        'override-validate-response-enabled', 'override-validate-response-value',
        'override-validate-verbose-enabled', 'override-validate-verbose-value',
        'override-validate-timeout-enabled', 'override-validate-timeout-value',
    ];
    const refresh = () => renderCurl(lastCurlState);
    for (const id of overrideIds) {
        const el = document.getElementById(id);
        if (el) el.addEventListener('input', refresh);
    }
    const customRows = document.getElementById('custom-headers-rows');
    if (customRows) {
        customRows.addEventListener('input', refresh);
        // catch row removal too: DOM mutation triggers a refresh
        new MutationObserver(refresh).observe(customRows, { childList: true });
    }
};
wireCurlLiveUpdate();

// Resource filter — hides table rows whose method or path doesn't match.
const resourceFilter = document.getElementById('resource-filter');
if (resourceFilter) {
    resourceFilter.addEventListener('input', () => {
        const q = resourceFilter.value.toLowerCase().trim();
        document.querySelectorAll('#fixed-service-table-body tr').forEach(row => {
            const method = row.querySelector('.fixed-resource-method')?.textContent.toLowerCase() || '';
            const path = row.querySelector('.fixed-resource-path span')?.textContent.toLowerCase() || '';
            row.style.display = !q || method.includes(q) || path.includes(q) ? '' : 'none';
        });
    });
}
