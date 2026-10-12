import { test, expect } from '@playwright/test';
import { createRequire } from 'node:module';

// Unit tests for the browser-free timer logic (internal/web/static/cook-timers.js).
// They need no server; they run in Node through the Playwright runner so the
// suite has one entry point (`make e2e`).
const require = createRequire(import.meta.url);
const T = require('../../internal/web/static/cook-timers.js');

test('a timer ends at a timestamp, so the time left depends on the clock only', () => {
  const t = T.start('2', 'Step 2: rise', 600, 1_000_000);
  expect(t.endAt).toBe(1_600_000);
  expect(T.remainingMs(t, 1_000_000)).toBe(600_000);
  // Many ticks missed while backgrounded: one look at the clock is exact.
  expect(T.remainingMs(t, 1_400_000)).toBe(200_000);
  expect(T.isDone(t, 1_599_999)).toBe(false);
  expect(T.isDone(t, 1_600_000)).toBe(true);
  expect(T.remainingMs(t, 9_999_999)).toBe(0);
});

test('remaining time formats as m:ss or h:mm:ss and rounds partial seconds up', () => {
  expect(T.format(0)).toBe('0:00');
  expect(T.format(1)).toBe('0:01');
  expect(T.format(59_001)).toBe('1:00');
  expect(T.format(600_000)).toBe('10:00');
  expect(T.format(3_600_000)).toBe('1:00:00');
  expect(T.format(3_725_000)).toBe('1:02:05');
  expect(T.format(-5)).toBe('0:00');
});

test('several timers coexist, and starting a step again replaces its timer', () => {
  let list = [];
  list = T.upsert(list, T.start('1', 'a', 60, 0));
  list = T.upsert(list, T.start('6', 'b', 60, 0));
  expect(list.map((t) => t.step)).toEqual(['1', '6']);
  list = T.upsert(list, T.start('1', 'a', 120, 0));
  expect(list.map((t) => t.step).sort()).toEqual(['1', '6']);
  expect(list.find((t) => t.step === '1').seconds).toBe(120);
  expect(T.remove(list, '1').map((t) => t.step)).toEqual(['6']);
});

test('a finished timer is announced once', () => {
  let list = [T.start('1', 'a', 60, 0), T.start('2', 'b', 600, 0)];
  expect(T.due(list, 30_000)).toEqual([]);
  expect(T.due(list, 60_000).map((t) => t.step)).toEqual(['1']);
  list = T.markNotified(list, ['1']);
  expect(T.due(list, 90_000)).toEqual([]);
  expect(T.due(list, 600_000).map((t) => t.step)).toEqual(['2']);
});

test('saved timers restore, and corrupt storage yields no timers instead of an error', () => {
  const list = [T.start('1', 'a', 60, 5)];
  expect(T.restore(T.save(list))).toEqual(list);
  expect(T.restore('not json')).toEqual([]);
  expect(T.restore('{"a":1}')).toEqual([]);
  expect(T.restore(JSON.stringify([{ step: 1 }, null, { step: '3', label: 'x', endAt: 10, seconds: 5 }]))).toEqual([
    { step: '3', label: 'x', endAt: 10, seconds: 5, notified: false },
  ]);
});
