import * as config from './config.js';
import * as commons from './commons.js';
import * as validators from './validators.js';
import * as navi from "./navi.js";
import * as services from "./services.js";
import * as presets from "./presets.js";

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
        headers['X-Cxs-Upstream-Url'] = upstreamUrl ? upstreamUrl.value : '';
    }

    // Cache Requests override
    const cacheEnabled = document.getElementById('override-cache-enabled');
    if (cacheEnabled && cacheEnabled.checked) {
        const cacheValue = document.getElementById('override-cache-value');
        if (cacheValue) {
            headers['X-Cxs-Cache-Requests'] = cacheValue.value;
        }
    }

    // Latency override
    const latencyEnabled = document.getElementById('override-latency-enabled');
    if (latencyEnabled && latencyEnabled.checked) {
        const latencyValue = document.getElementById('override-latency-value');
        if (latencyValue && latencyValue.value) {
            headers['X-Cxs-Latency'] = latencyValue.value;
        }
    }

    // Replay override
    const replayEnabled = document.getElementById('override-replay-enabled');
    if (replayEnabled && replayEnabled.checked) {
        const replayValue = document.getElementById('override-replay-value');
        // Always send the header when checked (empty value uses match fields from config)
        headers['X-Cxs-Replay'] = replayValue ? replayValue.value : '';
    }

    return headers;
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
    services.show();

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
    config.tabConfiguration.href = `#/configuration/${service}`;
    config.tabResources.classList.add('active');
    config.tabHistory.classList.remove('active');
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

            for (const { method, path, contentType } of endpoints) {
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
                pathCell.innerHTML = `<span>${path}</span>`;
                pathCell.className = `fixed-resource-path`;
                pathCell.title = path;
                row.appendChild(pathCell);

                table.appendChild(row);
                i += 1;
            }
            config.fixedServiceContainer.style.display = 'block';

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
        .then(res => {
            if (!res) {
                console.log(`No response`);
                return;
            }

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
                const reqView = commons.getCodeEditor(`request-body`, editorMode, {maxLines: Infinity});
                reqView.setValue(formattedBody);
                reqView.clearSelection();
                reqView.setReadOnly(true);
            }

            const curlBlock = document.getElementById('example-curl');

            // Use service name for the URL prefix, converting .root back to empty string
            const servicePrefix = service === '.root' ? '' : `/${service}`;
            curlBlock.textContent = `curl --request ${method} \\\n'${config.baseUrl}${servicePrefix}${reqPath}'`;
            if (reqContentType) {
                curlBlock.textContent += ` \\\n--header 'Content-Type: ${reqContentType}'`
            }

            // Add generated headers to cURL (skip Content-Type as it's already added above)
            for (const [headerName, headerValue] of Object.entries(reqHeaders)) {
                if (headerName.toLowerCase() === 'content-type') continue;
                curlBlock.textContent += ` \\\n--header '${headerName}: ${headerValue}'`;
            }

            // Add custom headers to cURL
            if (hasCustomHeaders) {
                for (const [headerName, headerValue] of Object.entries(customHeaders)) {
                    curlBlock.textContent += ` \\\n--header '${headerName}: ${headerValue}'`;
                }
            }

            // Add request body to cURL
            if (reqBodyString && method.toLowerCase() !== 'get') {
                curlBlock.textContent += ` \\\n--data '${reqBodyString.replace(/'/g, "\\'")}'`;
            }
            const exampleCurl = res.request?.examples?.curl;
            if (exampleCurl) {
                curlBlock.textContent += ` \\\n${exampleCurl}`;
            }

            // Make actual API call to get response
            if (reqPath) {
                // Convert .root back to empty string for actual API requests
                const apiService = service === '.root' ? '' : service;
                const apiUrl = apiService ? `${config.baseUrl}/${apiService}${reqPath}` : `${config.baseUrl}${reqPath}`;
                const fetchOptions = {
                    method: method.toUpperCase(),
                    headers: { ...reqHeaders }
                };

                // Mark request as coming from the UI
                fetchOptions.headers['X-Cxs-Source'] = 'ui';

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
                    fetchOptions.headers['X-Cxs-Context'] = btoa(JSON.stringify(replacements));
                }

                // Tell upstream middleware which headers to forward (everything else is browser noise)
                const upstreamHeaders = Object.keys({...reqHeaders, ...customHeaders});
                if (upstreamHeaders.length > 0) {
                    fetchOptions.headers['X-Cxs-Upstream-Headers'] = upstreamHeaders.join(',');
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
                    const responseView = commons.getCodeEditor(`response-body`, respEditorMode, {maxLines: Infinity});
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
                    const responseView = commons.getCodeEditor(`response-body`, `json`, {maxLines: Infinity});
                    responseView.setValue(`Error: ${error.message}`);
                    responseView.clearSelection();
                    responseView.setReadOnly(true);
                });
            }

        }).then(onDone);
}

document.getElementById('custom-header-add')
    .addEventListener('click', () => addCustomHeaderRow(true, '', '', true));
