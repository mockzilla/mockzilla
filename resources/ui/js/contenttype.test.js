// Run with: node --test resources/ui/js/contenttype.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isJSONContentType } from './contenttype.js';

test('plain and parameterized application/json', () => {
    assert.equal(isJSONContentType('application/json'), true);
    assert.equal(isJSONContentType('application/json; charset=utf-8'), true);
});

test('vendor and structured-suffix +json types', () => {
    assert.equal(isJSONContentType('application/vnd.mcn.transaction-service.api.v.1+json'), true);
    assert.equal(isJSONContentType('Application/Problem+JSON; charset=utf-8'), true);
});

test('non-JSON and missing types', () => {
    assert.equal(isJSONContentType('text/plain'), false);
    assert.equal(isJSONContentType('application/x-www-form-urlencoded'), false);
    assert.equal(isJSONContentType(null), false);
    assert.equal(isJSONContentType(''), false);
});
