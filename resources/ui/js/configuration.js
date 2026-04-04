import * as config from './config.js';
import * as commons from './commons.js';
import * as navi from './navi.js';
import * as services from './services.js';

const showTabs = (service) => {
    config.serviceTabs.style.display = 'flex';
    config.tabResources.href = `#/services/${service}`;
    config.tabHistory.href = `#/history/${service}`;
    config.tabHistory.style.display = config.historyEnabled ? '' : 'none';
    config.tabConfiguration.href = `#/configuration/${service}`;
    config.tabResources.classList.remove('active');
    config.tabHistory.classList.remove('active');
    config.tabConfiguration.classList.add('active');
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
    config.contentTitleEl.innerHTML = `${displayName} configuration`;

    showTabs(service);
    config.configurationContainer.style.display = 'block';

    const configUrl = `${config.baseUrl}/.config?service=${encodeURIComponent(service)}`;

    fetch(configUrl)
        .then(res => {
            if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
            return res.text();
        })
        .then(text => {
            const editor = commons.getCodeEditor('configuration-editor', 'yaml', {maxLines: Infinity});
            editor.setValue(text);
            editor.clearSelection();
            editor.setReadOnly(true);
        })
        .catch(err => {
            console.error('Failed to fetch config:', err);
            commons.showError('Failed to load service configuration');
        });
};
