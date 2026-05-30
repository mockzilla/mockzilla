import * as commons from './commons.js';
import * as navi from './navi.js';
import * as services from './services.js';
import * as home from './home.js';
import * as resources from './resources.js';
import * as history from './history.js';
import * as replay from './replay.js';
import * as configuration from './configuration.js';

const pageMap = new Map([
    ['', home.home],
    ['#/services/:name*', resources.show],
    ['#/services', services.show],
]);

if (appConfig.historyUrl) {
    pageMap.set('#/history/:name*', history.show);
}

if (appConfig.replayUrl) {
    pageMap.set('#/replay/:name*', replay.show);
}

if (!appConfig.disableConfigUI) {
    pageMap.set('#/configuration/:name*', configuration.show);
}

async function onLoad() {
    navi.resetContents();
    navi.loadPage(pageMap);
    home.showVersion();

    // Theme toggle
    const themeToggle = document.getElementById('theme-toggle');
    const savedTheme = localStorage.getItem('theme') || 'light';
    if (savedTheme === 'dark') {
        document.documentElement.setAttribute('data-theme', 'dark');
        themeToggle.textContent = '☀️';
    }
    themeToggle.addEventListener('click', () => {
        const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
        if (isDark) {
            document.documentElement.removeAttribute('data-theme');
            localStorage.setItem('theme', 'light');
            themeToggle.textContent = '🌙';
        } else {
            document.documentElement.setAttribute('data-theme', 'dark');
            localStorage.setItem('theme', 'dark');
            themeToggle.textContent = '☀️';
        }
        commons.initAceThemeSelect();
        commons.updateAllEditorThemes();
    });

    commons.initAceThemeSelect();

    // Panel resizers - each container gets its own stored split
    const initResizer = (containerEl, resizerEl, leftSelector, cssVar, storageKey) => {
        if (!containerEl || !resizerEl) return;

        const savedSplit = localStorage.getItem(storageKey);
        if (savedSplit) {
            containerEl.style.setProperty(cssVar, savedSplit + '%');
        }

        resizerEl.addEventListener('mousedown', (e) => {
            e.preventDefault();
            resizerEl.classList.add('dragging');
            containerEl.classList.add('resizing');

            const onMouseMove = (e) => {
                const rect = containerEl.getBoundingClientRect();
                const pct = ((e.clientX - rect.left) / rect.width) * 100;
                const clamped = Math.min(Math.max(pct, 10), 80);
                containerEl.style.setProperty(cssVar, clamped + '%');
            };

            const onMouseUp = () => {
                document.removeEventListener('mousemove', onMouseMove);
                document.removeEventListener('mouseup', onMouseUp);
                resizerEl.classList.remove('dragging');
                containerEl.classList.remove('resizing');
                const leftPanel = containerEl.querySelector(leftSelector);
                if (!leftPanel) return;
                const pct = (leftPanel.offsetWidth / containerEl.offsetWidth) * 100;
                localStorage.setItem(storageKey, pct.toFixed(1));
            };

            document.addEventListener('mousemove', onMouseMove);
            document.addEventListener('mouseup', onMouseUp);
        });
    };

    const contentPanels = document.querySelector('.content-panels');
    const panelResizer = contentPanels?.querySelector('.panel-resizer');
    initResizer(contentPanels, panelResizer, '.panel-resources', '--resources-width', 'panel-split');

    const mainEl = document.querySelector('.main');
    const sidebarResizer = document.getElementById('sidebar-resizer');
    initResizer(mainEl, sidebarResizer, '.sidebar', '--sidebar-width', 'sidebar-split');

    // Copy buttons
    document.addEventListener('click', (e) => {
        const btn = e.target.closest('.copy-btn');
        if (!btn) return;
        e.stopPropagation();

        const target = btn.dataset.copyTarget;
        let text = '';
        if (target === 'curl') {
            text = document.getElementById('example-curl').textContent;
        } else {
            const el = document.getElementById(target);
            if (el && el.env) {
                text = el.env.editor.getValue();
            }
        }

        if (!text) return;
        navigator.clipboard.writeText(text).then(() => {
            btn.textContent = 'Copied!';
            setTimeout(() => { btn.textContent = 'Copy'; }, 2000);
        });
    });

    // Generic horizontal tab strip setup. Used for both #resource-tabs and
    // #history-tabs. Persists the active tab per group; auto-scrolls the
    // active tab into view when the strip becomes visible or resizes.
    const setupTabGroup = (tabsId, panesContainerId, storageKey, defaultTab) => {
        const tabStrip = document.getElementById(tabsId);
        const panesContainer = document.getElementById(panesContainerId);
        if (!tabStrip || !panesContainer) return;
        const tabs = tabStrip.querySelectorAll('.resource-tab');
        const panes = panesContainer.querySelectorAll('.tab-pane');

        const scrollIntoView = (el) => {
            if (!el) return;
            const stripRect = tabStrip.getBoundingClientRect();
            if (stripRect.width === 0) return;
            const elRect = el.getBoundingClientRect();
            if (elRect.left < stripRect.left) {
                tabStrip.scrollLeft += elRect.left - stripRect.left;
            } else if (elRect.right > stripRect.right) {
                tabStrip.scrollLeft += elRect.right - stripRect.right;
            }
        };

        const activate = (name) => {
            let matched = null;
            let activePane = null;
            tabs.forEach(t => {
                const isActive = t.dataset.tab === name;
                t.classList.toggle('active', isActive);
                if (isActive) matched = t;
            });
            panes.forEach(p => {
                const isActive = p.dataset.tab === name;
                p.classList.toggle('active', isActive);
                if (isActive) activePane = p;
            });
            if (activePane && window.ace) {
                // ACE editors cache layout dimensions and don't recompute when
                // their container goes from display:none to visible. Force a
                // resize so editors fill the now-visible pane.
                activePane.querySelectorAll('.ace_editor').forEach(el => {
                    const ed = window.ace.edit(el);
                    if (ed) requestAnimationFrame(() => ed.resize(true));
                });
            }
            if (matched) {
                requestAnimationFrame(() => scrollIntoView(matched));
                setTimeout(() => scrollIntoView(matched), 100);
                setTimeout(() => scrollIntoView(matched), 500);
            }
            return !!matched;
        };

        new ResizeObserver(() => {
            const active = tabStrip.querySelector('.resource-tab.active');
            if (active) scrollIntoView(active);
        }).observe(tabStrip);

        const saved = localStorage.getItem(storageKey);
        if (!saved || !activate(saved)) activate(defaultTab);

        tabs.forEach(tab => {
            tab.addEventListener('click', (e) => {
                e.preventDefault();
                const name = tab.dataset.tab;
                activate(name);
                localStorage.setItem(storageKey, name);
            });
        });
    };

    setupTabGroup('resource-tabs', 'generator-container', 'resource-tab', 'response');
    setupTabGroup('history-tabs', 'history-detail', 'history-tab', 'response');
    setupTabGroup('replay-tabs', 'replay-detail', 'replay-tab', 'response');
}

window.addEventListener('hashchange', _ => {
    commons.hideMessage();
    navi.loadPage(pageMap);
})
window.addEventListener("DOMContentLoaded", _ => onLoad())
