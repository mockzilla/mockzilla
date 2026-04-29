import * as commons from './commons.js';
import { addCustomHeaderRow } from './resources.js';

const STORAGE_KEY_PRESETS = 'mz-presets';
const STORAGE_KEY_ACTIVE = 'mz-active-preset';
const NONE_KEY = '__none__';
const NONE_LABEL = '(None)';
const AUTO_SAVE_DELAY = 500;

let initialized = false;
let autoSaveTimer = null;

const getPresets = () => {
    try {
        const raw = localStorage.getItem(STORAGE_KEY_PRESETS);
        return raw ? JSON.parse(raw) : {};
    } catch {
        return {};
    }
};

const savePresets = (presets) => {
    localStorage.setItem(STORAGE_KEY_PRESETS, JSON.stringify(presets));
};

const getActivePresetName = () => {
    return localStorage.getItem(STORAGE_KEY_ACTIVE) || NONE_KEY;
};

const setActivePresetName = (name) => {
    localStorage.setItem(STORAGE_KEY_ACTIVE, name);
};

const getDefaultState = () => ({
    contextReplacements: '',
    customHeaders: [],
    overrides: {
        upstream: { enabled: false, value: '' },
        cache: { enabled: false, value: 'true' },
        latency: { enabled: false, value: '' },
        replay: { enabled: false, value: '' },
    },
});

const getCurrentState = () => {
    const replacementsEditor = commons.getCodeEditor('context-replacements', 'yaml');

    const customHeaders = [];
    const rows = document.querySelectorAll('#custom-headers-rows .custom-header-row');
    for (const row of rows) {
        const cb = row.querySelector('input[type="checkbox"]');
        const nameInput = row.querySelector('.custom-header-name');
        const valueInput = row.querySelector('.custom-header-value');
        customHeaders.push({
            enabled: cb ? cb.checked : true,
            name: nameInput ? nameInput.value : '',
            value: valueInput ? valueInput.value : '',
            visible: valueInput ? valueInput.type === 'text' : true,
        });
    }

    return {
        contextReplacements: replacementsEditor.getValue(),
        customHeaders,
        overrides: {
            upstream: {
                enabled: document.getElementById('override-upstream-enabled').checked,
                value: document.getElementById('override-upstream-url').value,
            },
            cache: {
                enabled: document.getElementById('override-cache-enabled').checked,
                value: document.getElementById('override-cache-value').value,
            },
            latency: {
                enabled: document.getElementById('override-latency-enabled').checked,
                value: document.getElementById('override-latency-value').value,
            },
            replay: {
                enabled: document.getElementById('override-replay-enabled').checked,
                value: document.getElementById('override-replay-value').value,
            },
        },
    };
};

const applyState = (state) => {
    if (!state) return;
    const s = { ...getDefaultState(), ...state };

    const replacementsEditor = commons.getCodeEditor('context-replacements', 'yaml');
    replacementsEditor.setValue(s.contextReplacements || '');
    replacementsEditor.clearSelection();

    const container = document.getElementById('custom-headers-rows');
    container.innerHTML = '';
    let headers = s.customHeaders;
    if (typeof headers === 'string') headers = [];
    if (Array.isArray(headers)) {
        for (const h of headers) {
            addCustomHeaderRow(h.enabled !== false, h.name || '', h.value || '', h.visible !== false);
        }
    }

    const ov = s.overrides || {};

    const setOverride = (id, selectOrInputId, override) => {
        const cb = document.getElementById(id);
        const input = document.getElementById(selectOrInputId);
        if (cb) cb.checked = !!(override && override.enabled);
        if (input) input.value = (override && override.value != null) ? override.value : '';
    };

    setOverride('override-upstream-enabled', 'override-upstream-url', ov.upstream);
    setOverride('override-cache-enabled', 'override-cache-value', ov.cache);
    setOverride('override-latency-enabled', 'override-latency-value', ov.latency);
    setOverride('override-replay-enabled', 'override-replay-value', ov.replay);
};

const populateDropdown = () => {
    const select = document.getElementById('preset-select');
    if (!select) return;

    const active = getActivePresetName();
    const presets = getPresets();

    select.innerHTML = '';

    // (None) is always first - no saving
    const noneOpt = document.createElement('option');
    noneOpt.value = NONE_KEY;
    noneOpt.textContent = NONE_LABEL;
    select.appendChild(noneOpt);

    // Named presets sorted alphabetically
    const names = Object.keys(presets)
        .sort((a, b) => a.localeCompare(b));

    for (const name of names) {
        const opt = document.createElement('option');
        opt.value = name;
        opt.textContent = name;
        select.appendChild(opt);
    }

    select.value = active;
};

const scheduleAutoSave = () => {
    clearTimeout(autoSaveTimer);
    autoSaveTimer = setTimeout(() => {
        const active = getActivePresetName();
        if (active === NONE_KEY) return;

        const presets = getPresets();
        presets[active] = getCurrentState();
        savePresets(presets);
    }, AUTO_SAVE_DELAY);
};

const setupAutoSave = () => {
    const replacementsEditor = commons.getCodeEditor('context-replacements', 'yaml');
    replacementsEditor.session.on('change', scheduleAutoSave);

    const headersContainer = document.getElementById('custom-headers-rows');
    headersContainer.addEventListener('input', scheduleAutoSave);
    headersContainer.addEventListener('change', scheduleAutoSave);
    headersContainer.addEventListener('click', scheduleAutoSave);

    // Config override fields
    const ids = [
        'override-upstream-enabled', 'override-upstream-url',
        'override-cache-enabled', 'override-cache-value',
        'override-latency-enabled', 'override-latency-value',
        'override-replay-enabled', 'override-replay-value',
    ];
    for (const id of ids) {
        const el = document.getElementById(id);
        if (!el) continue;
        if (el.type === 'checkbox') {
            el.addEventListener('change', scheduleAutoSave);
        } else {
            el.addEventListener('input', scheduleAutoSave);
            el.addEventListener('change', scheduleAutoSave);
        }
    }
};

const onDropdownChange = () => {
    const select = document.getElementById('preset-select');
    const name = select.value;
    setActivePresetName(name);

    if (name === NONE_KEY) {
        applyState(getDefaultState());
        return;
    }

    const presets = getPresets();
    const state = presets[name] || getDefaultState();
    applyState(state);
};

const onSaveAs = () => {
    const name = window.prompt('Preset name:');
    if (!name || !name.trim()) return;

    const trimmed = name.trim();
    if (trimmed === NONE_KEY) {
        window.alert('This name is reserved.');
        return;
    }

    const presets = getPresets();
    presets[trimmed] = getCurrentState();
    savePresets(presets);

    setActivePresetName(trimmed);
    populateDropdown();
};

const onDelete = () => {
    const active = getActivePresetName();
    if (active === NONE_KEY) return;

    if (!window.confirm(`Delete preset "${active}"?`)) return;

    const presets = getPresets();
    delete presets[active];
    savePresets(presets);

    setActivePresetName(NONE_KEY);
    populateDropdown();
    applyState(getDefaultState());
};

export const initIfNeeded = () => {
    if (initialized) return;
    initialized = true;

    // Load active preset and apply state
    const active = getActivePresetName();
    if (active !== NONE_KEY) {
        const presets = getPresets();
        const state = presets[active] || getDefaultState();
        applyState(state);
    }

    // Show controls
    const controls = document.getElementById('preset-controls');
    if (controls) controls.style.display = 'flex';

    populateDropdown();

    // Bind events
    document.getElementById('preset-select').addEventListener('change', onDropdownChange);
    document.getElementById('preset-save-as').addEventListener('click', onSaveAs);
    document.getElementById('preset-delete').addEventListener('click', onDelete);

    setupAutoSave();
};
