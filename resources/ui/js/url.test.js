// Run with: node --test resources/ui/js/url.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { joinServiceUrl } from './url.js';

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
