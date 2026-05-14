import * as config from './config.js';
import * as navi from "./navi.js";

const STORAGE_KEY_STARS = 'mz-starred-services';

let cachedServices = null;
let loadPromise = null;
const servicePrefixes = {};

export const getServicePrefix = (name) => servicePrefixes[name];

// ensureLoaded resolves once the services list (and its name → prefix map)
// is fetched. Callers that need getServicePrefix must await this first,
// otherwise a deep link straight to #/services/<name> can race the list
// fetch and fall back to using <name> as the URL prefix.
export const ensureLoaded = () => {
    if (cachedServices) return Promise.resolve(cachedServices);
    if (loadPromise) return loadPromise;
    loadPromise = fetch(config.serviceUrl)
        .then(res => res.json())
        .then(data => {
            cachedServices = data['items'];
            for (const item of cachedServices) {
                servicePrefixes[item.name] = item.prefix;
            }
            return cachedServices;
        });
    return loadPromise;
};

const getStarred = () => {
    try {
        const raw = localStorage.getItem(STORAGE_KEY_STARS);
        return raw ? JSON.parse(raw) : [];
    } catch {
        return [];
    }
};

const setStarred = (list) => {
    localStorage.setItem(STORAGE_KEY_STARS, JSON.stringify(list));
};

const toggleStar = (name) => {
    const starred = getStarred();
    const idx = starred.indexOf(name);
    if (idx === -1) {
        starred.push(name);
    } else {
        starred.splice(idx, 1);
    }
    setStarred(starred);
};

const renderServices = (services, selected) => {
    const list = config.serviceList;
    list.innerHTML = '';

    const starred = getStarred();

    const sorted = [...services].sort((a, b) => {
        const aStarred = starred.includes(a.name);
        const bStarred = starred.includes(b.name);
        if (aStarred !== bStarred) return aStarred ? -1 : 1;
        return a.name.localeCompare(b.name);
    });

    for (let { name, type, resourceNumber } of sorted) {
        const li = document.createElement('li');
        li.id = `service-${name}`;

        const originalName = name;
        const isStarred = starred.includes(originalName);

        const star = document.createElement('span');
        star.className = 'star-toggle' + (isStarred ? ' starred' : '');
        star.innerHTML = isStarred
            ? '<i class="fa-solid fa-star"></i>'
            : '<i class="fa-regular fa-star"></i>';
        star.title = 'Toggle favorite';
        star.addEventListener('click', (e) => {
            e.stopPropagation();
            e.preventDefault();
            toggleStar(originalName);
            const current = config.serviceList.querySelector('li.selected-service');
            const currentId = current ? current.id.replace('service-', '') : '';
            renderServices(services, currentId);
        });
        li.appendChild(star);

        let nameLink = name;
        if (name === ``) {
            name = "/"
            nameLink = `.root`
        }

        const svcName = document.createElement('a');
        svcName.href = `#/services/${nameLink}`;
        svcName.className = 'service-name';
        svcName.textContent = name;
        li.appendChild(svcName);

        const count = document.createElement('span');
        count.className = 'service-count';
        count.textContent = resourceNumber;
        count.title = 'Number of resources';
        li.appendChild(count);

        li.addEventListener('click', () => { window.location.hash = `#/services/${nameLink}`; });

        list.appendChild(li);
    }

    document.getElementById('service-list-header').style.display = 'flex';
    config.serviceList.style.display = 'block';
    if (selected !== ``) {
        navi.applySelection(`service-${selected}`, 'selected-service');
    }
};

export const show = (selected = '') => {
    if (!cachedServices) {
        console.log("loading service list");
        config.serviceList.innerHTML = '';
    }
    ensureLoaded().then(items => renderServices(items, selected));
}
