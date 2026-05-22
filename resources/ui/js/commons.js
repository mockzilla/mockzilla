import * as config from './config.js';

const aceThemes = {
    Bright: ['chrome','clouds','crimson_editor','dawn','dreamweaver','eclipse','github','iplastic','katzenmilch','kuroir','solarized_light','sqlserver','textmate','tomorrow','xcode'],
    Dark: ['ambiance','chaos','clouds_midnight','cobalt','dracula','gob','gruvbox','idle_fingers','kr_theme','merbivore','merbivore_soft','mono_industrial','monokai','pastel_on_dark','solarized_dark','terminal','tomorrow_night','tomorrow_night_blue','tomorrow_night_bright','tomorrow_night_eighties','vibrant_ink']
};

const isDarkMode = () => document.documentElement.getAttribute('data-theme') === 'dark';

export const getEditorTheme = () => {
    const key = isDarkMode() ? 'ace-dark-theme' : 'ace-light-theme';
    const saved = localStorage.getItem(key);
    if (saved) return saved;
    return isDarkMode() ? (config.editor.darkTheme || 'cobalt') : (config.editor.theme || 'chrome');
}

export const updateAllEditorThemes = () => {
    const theme = `ace/theme/${getEditorTheme()}`;
    for (const editor of activeEditors) {
        editor.setTheme(theme);
    }
}

export const initAceThemeSelect = () => {
    const sel = document.getElementById('ace-theme-select');
    if (!sel) return;
    const group = isDarkMode() ? 'Dark' : 'Bright';
    sel.innerHTML = '';
    aceThemes[group].forEach(t => {
        const opt = document.createElement('option');
        opt.value = t;
        opt.textContent = t.replace(/_/g, ' ');
        sel.appendChild(opt);
    });
    sel.value = getEditorTheme();
    sel.onchange = () => {
        const key = isDarkMode() ? 'ace-dark-theme' : 'ace-light-theme';
        localStorage.setItem(key, sel.value);
        updateAllEditorThemes();
    };
}

const activeEditors = new Set();

export const showSuccess = text => {
    showMessage(text, 'success')
}

export const showWarning = text => {
    showMessage(text, 'warning')
}

export const showError = text => {
    showMessage(text, 'error')
}

export const showSuccessOrError = (text, success) => {
    showMessage(text, success ? 'success' : 'error')
}

export const showMessage = (text, alertType) => {
    config.messageCont.textContent = text;
    config.messageCont.className = `alert-${alertType}`
    config.messageCont.style.display = 'inline-block';
    config.messageCont.style.opacity = '1';
}

export const hideMessage = () => {
    config.messageCont.style.display = 'none';
}

export const getCodeEditor = (htmlID, mode, opts) => {
    const codeEditorContainer = document.getElementById(htmlID);
    const editor = ace.edit(codeEditorContainer);

    const options = {
        showLineNumbers: true,
        mode: `ace/mode/${mode}`,
        showPrintMargin: false,
        ...opts,
    };
    editor.setOptions(options);

    editor.setTheme(`ace/theme/${getEditorTheme()}`);
    editor.setFontSize(`${config.editor.fontSize}px`);
    editor.resize();

    activeEditors.add(editor);
    return editor;
}

export const getCodeEditorMode = value => {
    const contentMap = {
        yml: `yaml`,
        md: `markdown`,
        txt: `text`,
    }
    return contentMap.hasOwnProperty(value) ? contentMap[value] : value;
}

export const getEditorForm = (editorId, typeId) => {
    console.log(`response edit in ${editorId}`);

    const editor = getCodeEditor(editorId, `json`);
    editor.setValue(``);
    editor.clearSelection();
    console.log(`typeID:`, typeId);
    if (typeId !== undefined) {
        document.getElementById(typeId).addEventListener(`change`, el => {
            const value = el.target.value;
            const mapped = getCodeEditorMode(value)
            editor.setOptions({
                mode: `ace/mode/${mapped}`,
            })
        })
    }

    return editor;
}

export const isResourceEditable = contentType => {
    const editableContentTypes = {
        'application/json': true,
        'application/x-yaml': true,
        'text/plain; charset=utf-8': true,
        'text/markdown; charset=utf-8' : true,
        'text/html; charset=utf-8': true,
        'text/xml; charset=utf-8': true,
    }
    return editableContentTypes.hasOwnProperty(contentType);
}
