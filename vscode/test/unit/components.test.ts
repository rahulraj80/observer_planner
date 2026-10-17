// Unit tests for src/components-internals.ts (T-670): the extension hides a
// surface only when the daemon says its component is not active, and fails
// open on everything else.

import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import {
  GATED_SURFACES,
  contextValues,
  isComponentInactive,
  isRouteInactive,
  type ComponentsStateLite,
} from '../../src/components-internals';

const st: ComponentsStateLite = {
  view: {
    components: [
      { id: 'core', state: 'active' },
      { id: 'capture.proxy', state: 'available' },
      { id: 'analytics', state: 'absent' },
      { id: 'terminals', state: 'active' },
    ],
  },
  inactive_routes: ['GET /api/cache/status', '/api/discover/', 'GET /api/sessions/{id}/predict'],
};

describe('component gate', () => {
  test('a component is inactive only when the view lists it as not active', () => {
    assert.equal(isComponentInactive(st, 'capture.proxy'), true);
    assert.equal(isComponentInactive(st, 'terminals'), false);
    assert.equal(isComponentInactive(st, 'core'), false);
    assert.equal(isComponentInactive(st, 'never-heard-of'), false, 'unknown id fails open');
    assert.equal(isComponentInactive(undefined, 'capture.proxy'), false, 'no read fails open');
    assert.equal(isComponentInactive({}, 'capture.proxy'), false, 'older daemon shape fails open');
  });

  test('a route is inactive when it matches a registered pattern the daemon listed', () => {
    assert.equal(isRouteInactive(st, '/api/cache/status'), true, 'exact, method prefix ignored');
    assert.equal(isRouteInactive(st, '/api/cache/status?session=x'), true);
    assert.equal(isRouteInactive(st, '/api/cache/status/extra'), false, 'an exact pattern is not a prefix');
    assert.equal(isRouteInactive(st, '/api/discover/files'), true, 'a trailing slash is a prefix');
    assert.equal(isRouteInactive(st, '/api/sessions/abc/predict'), true, 'a {name} segment');
    assert.equal(isRouteInactive(st, '/api/sessions/abc'), false);
    assert.equal(isRouteInactive(st, '/api/status'), false);
    assert.equal(isRouteInactive(undefined, '/api/cache/status'), false);
  });

  test('context values: every gated surface has a key; the proxy and cache bar hide here', () => {
    const v = contextValues(st);
    assert.deepEqual(Object.keys(v).sort(), GATED_SURFACES.map((s) => s.contextKey).sort());
    assert.equal(v['observer.proxyInactive'], true);
    assert.equal(v['observer.cacheStatusInactive'], true);
    const none = contextValues(undefined);
    assert.ok(Object.values(none).every((x) => x === false), 'no read shows everything');
  });

  test('every gated surface names what it needs, by component id or route, never a tool', () => {
    for (const s of GATED_SURFACES) {
      assert.ok(s.contextKey.startsWith('observer.'), s.contextKey);
      assert.ok(!!s.component || !!s.route, `${s.contextKey} names neither a component nor a route`);
    }
  });
});
