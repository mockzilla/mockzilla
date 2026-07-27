// Run with: node --test resources/ui/js/url.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { joinServiceUrl, resolveConfigUrl } from './url.js';

const BASE = 'http://localhost:2200';

test('root-mounted service: prefix "/" collapses with leading-slash path', () => {
    assert.equal(joinServiceUrl(BASE, '/', '/'), 'http://localhost:2200/');
    assert.equal(joinServiceUrl(BASE, '/', '/pets'), 'http://localhost:2200/pets');
});

test('named service: single-segment prefix is preserved', () => {
    assert.equal(joinServiceUrl(BASE, '/petstore', '/'), 'http://localhost:2200/petstore/');
    assert.equal(joinServiceUrl(BASE, '/petstore', '/pets/1'), 'http://localhost:2200/petstore/pets/1');
});

test('multi-segment mount prefix is preserved', () => {
    assert.equal(joinServiceUrl(BASE, '/foo/bar', '/'), 'http://localhost:2200/foo/bar/');
    assert.equal(joinServiceUrl(BASE, '/foo/bar', '/items'), 'http://localhost:2200/foo/bar/items');
});

test('trailing slash on prefix is collapsed at the join', () => {
    assert.equal(joinServiceUrl(BASE, '/foo/bar/', '/'), 'http://localhost:2200/foo/bar/');
    assert.equal(joinServiceUrl(BASE, '/foo/bar/', '/items'), 'http://localhost:2200/foo/bar/items');
    assert.equal(joinServiceUrl(BASE, '/foo/bar///', '/items'), 'http://localhost:2200/foo/bar/items');
});

test('empty or nullish prefix joins base + path directly', () => {
    assert.equal(joinServiceUrl(BASE, '', '/pets'), 'http://localhost:2200/pets');
    assert.equal(joinServiceUrl(BASE, null, '/pets'), 'http://localhost:2200/pets');
    assert.equal(joinServiceUrl(BASE, undefined, '/pets'), 'http://localhost:2200/pets');
});

test('paths with query strings round-trip through the join', () => {
    assert.equal(joinServiceUrl(BASE, '/foo/bar', '/items?q=1'), 'http://localhost:2200/foo/bar/items?q=1');
    assert.equal(joinServiceUrl(BASE, '/', '/?q=1'), 'http://localhost:2200/?q=1');
});

test('resolveConfigUrl: relative path is joined onto origin', () => {
    assert.equal(resolveConfigUrl(BASE, '/.services'), 'http://localhost:2200/.services');
    assert.equal(resolveConfigUrl(BASE, '/'), 'http://localhost:2200/');
});

test('resolveConfigUrl: already-absolute value (AppConfig.BaseURL set, e.g. APP_BASE_URL) passes through unchanged', () => {
    assert.equal(resolveConfigUrl(BASE, 'http://localhost:2200/.services'), 'http://localhost:2200/.services');
    assert.equal(resolveConfigUrl(BASE, 'https://payments.example.com/.services'), 'https://payments.example.com/.services');
});

test('resolveConfigUrl: empty or nullish value resolves to empty string', () => {
    assert.equal(resolveConfigUrl(BASE, ''), '');
    assert.equal(resolveConfigUrl(BASE, null), '');
    assert.equal(resolveConfigUrl(BASE, undefined), '');
});
