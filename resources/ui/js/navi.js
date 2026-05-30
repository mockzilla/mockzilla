import * as config from './config.js';

export const loadPage = pageMap => {
    const currentHash = window.location.hash;
    console.log(`currentHash ${currentHash}`);

    // Check if the current hash matches any exact matches in the map
    if (pageMap.has(currentHash)) {
        console.log("page exact match");
        const pageFunction = pageMap.get(currentHash);
        pageFunction();
        return;
    }

    // Check if the current hash matches any placeholders in the map
    for (const [key, pageFunction] of pageMap) {
        if (isPlaceholderMatch(key, currentHash)) {
            console.log("page matched by pattern");
            const match = getPlaceholderMatch(key, currentHash);
            pageFunction(match);
            return;
        }
    }
    console.log("no page matched");
}

// Helper function to validate parameter value based on pattern constraints
const validateParam = (paramPattern, value) => {
    // Check for numeric constraint (:ix#num means must be a number)
    if (paramPattern.includes('#num')) {
        return /^\d+$/.test(value);
    }
    return true;
}

// Helper function to check if a placeholder pattern matches the current hash
export const isPlaceholderMatch = (placeholderPattern, currentHash) => {
    // Strip query string from hash before processing
    const hashWithoutQuery = currentHash.split('?')[0];

    const patternParts = placeholderPattern.split('/');
    const hashParts = hashWithoutQuery.split('/');

    // Check if pattern has a wildcard (*)
    const wildcardIndex = patternParts.findIndex(part => part.startsWith(':') && part.includes('*'));

    if (wildcardIndex !== -1) {
        // Pattern has wildcard - must match up to wildcard position
        if (hashParts.length < patternParts.length - 1) {
            return false;
        }

        // Check parts before wildcard
        for (let i = 0; i < wildcardIndex; i++) {
            const patternPart = patternParts[i];
            if (patternPart.startsWith(':')) {
                if (!validateParam(patternPart, hashParts[i])) {
                    return false;
                }
                continue;
            }
            if (patternPart !== hashParts[i]) {
                return false;
            }
        }

        // Check parts after wildcard
        const partsAfterWildcard = patternParts.length - wildcardIndex - 1;
        for (let i = 0; i < partsAfterWildcard; i++) {
            const patternPart = patternParts[wildcardIndex + 1 + i];
            const hashPart = hashParts[hashParts.length - partsAfterWildcard + i];
            if (patternPart.startsWith(':')) {
                if (!validateParam(patternPart, hashPart)) {
                    return false;
                }
                continue;
            }
            if (patternPart !== hashPart) {
                return false;
            }
        }

        return true;
    }

    // No wildcard - exact length match required
    if (patternParts.length !== hashParts.length) {
        return false;
    }

    for (let i = 0; i < patternParts.length; i++) {
        const patternPart = patternParts[i];
        if (patternPart.startsWith(':')) {
            if (!validateParam(patternPart, hashParts[i])) {
                return false;
            }
            continue; // Skip placeholder parts
        }

        if (patternPart !== hashParts[i]) {
            return false;
        }
    }

    return true;
}

// Helper function to extract the placeholder values from the current hash
export const getPlaceholderMatch = (placeholderPattern, currentHash) => {
    // Strip query string from hash before processing
    const hashWithoutQuery = currentHash.split('?')[0];

    const patternParts = placeholderPattern.split('/');
    const hashParts = hashWithoutQuery.split('/');

    const match = {
        params: {}
    };

    // Check if pattern has a wildcard (*)
    const wildcardIndex = patternParts.findIndex(part => part.startsWith(':') && part.includes('*'));

    if (wildcardIndex !== -1) {
        // Extract wildcard parameter name (e.g., ":name*" -> "name")
        const wildcardPart = patternParts[wildcardIndex];
        const paramName = wildcardPart.substring(1).replace('*', '');

        // Extract parts before wildcard
        for (let i = 0; i < wildcardIndex; i++) {
            const patternPart = patternParts[i];
            if (patternPart.startsWith(':')) {
                const name = patternPart.substring(1);
                match.params[name] = decodeURI(hashParts[i]);
            }
        }

        // Extract wildcard value (all parts from wildcardIndex to end minus parts after wildcard)
        const partsAfterWildcard = patternParts.length - wildcardIndex - 1;
        const wildcardEndIndex = hashParts.length - partsAfterWildcard;
        const wildcardValue = hashParts.slice(wildcardIndex, wildcardEndIndex).join('/');
        match.params[paramName] = decodeURI(wildcardValue);

        // Extract parts after wildcard
        for (let i = 0; i < partsAfterWildcard; i++) {
            const patternPart = patternParts[wildcardIndex + 1 + i];
            if (patternPart.startsWith(':')) {
                const name = patternPart.substring(1);
                const hashPart = hashParts[hashParts.length - partsAfterWildcard + i];
                match.params[name] = decodeURI(hashPart);
            }
        }

        return match;
    }

    // No wildcard - simple parameter extraction
    for (let i = 0; i < patternParts.length; i++) {
        const patternPart = patternParts[i];
        if (patternPart.startsWith(':')) {
            const paramName = patternPart.substring(1);
            match.params[paramName] = decodeURI(hashParts[i]);
        }
    }

    return match;
}

export const applySelection = (targetEl, selectionClassName) => {
    console.log(`applying selection for ${targetEl}`);
    if (!targetEl) {
        return;
    }

    if (targetEl === `service-root`) {
        targetEl = `service-`;
    }

    const collection = document.getElementsByClassName(selectionClassName);
    for (let i = 0; i < collection.length; i++) {
        collection[i].classList.remove(selectionClassName);
    }
    const row = document.getElementById(targetEl);
    if (!row) {
        console.log(`no row found for ${targetEl}`);
        return;
    }
    row.classList.add(selectionClassName);
}

export const setActiveView = (view) => {
    const cp = document.querySelector('.content-panels');
    if (!cp) return;
    cp.classList.remove('view-resources', 'view-history', 'view-replay', 'view-config');
    if (view) cp.classList.add(`view-${view}`);

    const hint = document.getElementById('right-pane-hint');
    if (hint) {
        if (view === 'history') hint.textContent = 'Select an entry from the list';
        else if (view === 'replay') hint.textContent = 'Select a recording from the list';
        else if (view === 'resources') hint.textContent = 'Select a resource from the list';
        else hint.textContent = '';
        hint.style.display = view === 'resources' || view === 'history' || view === 'replay' ? '' : 'none';
    }
};

export const resetContents = () => {
    console.log(`reset contents`);
    config.homeContents.style.display = 'none';
    config.contentTitleEl.innerHTML = '';
    config.iframeContents.src = '';
    config.iframeContents.style.display = 'none';

    if (config.serviceList) config.serviceList.style.display = 'none';

    // LEFT pane: hide swappable bodies and clear stale data
    document.getElementById('fixed-service-table-list').style.display = 'none';
    document.getElementById('fixed-service-table-body').innerHTML = '';
    document.getElementById('history-table-list').style.display = 'none';
    document.getElementById('history-table-body').innerHTML = '';
    document.getElementById('replay-table-list').style.display = 'none';
    document.getElementById('replay-table-body').innerHTML = '';
    // Hide the wrap; the inner #configuration-editor must keep display:''
    // so ace can size it. Setting display:none on the editor element directly
    // sticks even after the wrap becomes visible, leaving an invisible editor.
    const _cfgWrap = document.getElementById('configuration-editor-wrap');
    if (_cfgWrap) _cfgWrap.style.display = 'none';
    document.getElementById('history-actions').style.display = 'none';
    document.getElementById('replay-actions').style.display = 'none';

    // RIGHT pane: hide swappable bodies, reset titles
    config.generatorCont.style.display = 'none';
    document.getElementById('resource-edit-container').style.display = 'none';
    document.getElementById('history-detail').style.display = 'none';
    document.getElementById('replay-detail').style.display = 'none';
    document.getElementById('resource-result').innerHTML = '';
    document.getElementById('resource-panel-title').textContent = 'Select a resource';
    document.getElementById('resource-panel-title').style.display = '';
    document.getElementById('history-detail-title').style.display = 'none';
    document.getElementById('history-detail-title').textContent = 'Select an entry';
    document.getElementById('replay-detail-title').style.display = 'none';
    document.getElementById('replay-detail-title').textContent = 'Select a recording';
    const replayDelBtn = document.getElementById('replay-delete');
    if (replayDelBtn) replayDelBtn.style.display = 'none';
    config.resourceRefreshBtn.style.display = 'none';
    const presetCtl = document.getElementById('preset-controls');
    if (presetCtl) presetCtl.style.display = 'none';
    const tabs = document.getElementById('resource-tabs');
    if (tabs) tabs.style.display = 'none';
    const hTabs = document.getElementById('history-tabs');
    if (hTabs) hTabs.style.display = 'none';
    const rTabs = document.getElementById('replay-tabs');
    if (rTabs) rTabs.style.display = 'none';
    const cfgWrap = document.getElementById('configuration-editor-wrap');
    if (cfgWrap) cfgWrap.style.display = 'none';
    const hint = document.getElementById('right-pane-hint');
    if (hint) hint.style.display = 'none';

    config.fixedServiceContainer.style.display = 'none';
    config.serviceTabs.style.display = 'none';
}
