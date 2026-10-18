// Pure timer logic for cook mode. A timer is an end timestamp, not a tick
// count, so it stays right while the page is backgrounded and timers are
// throttled or frozen. No DOM access here: cook.js wires it to the page, and
// e2e/cook/timers.spec.ts tests it directly in Node.
(function (root) {
  'use strict';

  // start returns a running timer that ends `seconds` after `now` (ms epoch).
  function start(step, label, seconds, now) {
    return { step: step, label: label, endAt: now + seconds * 1000, seconds: seconds, notified: false };
  }

  // A paused timer carries pausedMs, the time that was left when it stopped,
  // and its endAt is ignored until resume. A running timer has no pausedMs.
  function isPaused(timer) {
    return Number.isFinite(timer.pausedMs);
  }

  // remainingMs is how long is left, never negative.
  function remainingMs(timer, now) {
    if (isPaused(timer)) return Math.max(0, timer.pausedMs);
    return Math.max(0, timer.endAt - now);
  }

  function isDone(timer, now) {
    return remainingMs(timer, now) === 0;
  }

  // pause freezes a running timer at its remaining time. A finished or already
  // paused timer is returned unchanged.
  function pause(timer, now) {
    if (isPaused(timer) || isDone(timer, now)) return timer;
    return Object.assign({}, timer, { pausedMs: remainingMs(timer, now) });
  }

  // resume continues a paused timer from the time it had left.
  function resume(timer, now) {
    if (!isPaused(timer)) return timer;
    var next = Object.assign({}, timer, { endAt: now + timer.pausedMs });
    delete next.pausedMs;
    return next;
  }

  // update returns a new list with fn applied to the timer of the given step.
  function update(timers, step, fn) {
    return timers.map(function (t) { return t.step === step ? fn(t) : t; });
  }

  // format renders remaining time as m:ss, or h:mm:ss from an hour up. Partial
  // seconds round up, so a timer never shows 0:00 before it has finished.
  function format(ms) {
    var total = Math.ceil(Math.max(0, ms) / 1000);
    var h = Math.floor(total / 3600);
    var m = Math.floor((total % 3600) / 60);
    var s = total % 60;
    var ss = (s < 10 ? '0' : '') + s;
    if (h > 0) return h + ':' + (m < 10 ? '0' : '') + m + ':' + ss;
    return m + ':' + ss;
  }

  // upsert replaces the timer of the same step, or adds one. It returns a new
  // list; the input is not changed.
  function upsert(timers, timer) {
    return timers.filter(function (t) { return t.step !== timer.step; }).concat([timer]);
  }

  function remove(timers, step) {
    return timers.filter(function (t) { return t.step !== step; });
  }

  // due lists the timers that have finished and not yet been announced.
  function due(timers, now) {
    return timers.filter(function (t) { return !t.notified && isDone(t, now); });
  }

  // markNotified returns a new list with the given steps marked announced.
  function markNotified(timers, steps) {
    return timers.map(function (t) {
      return steps.indexOf(t.step) >= 0 ? Object.assign({}, t, { notified: true }) : t;
    });
  }

  function save(timers) {
    return JSON.stringify(timers);
  }

  // restore parses saved timers, dropping anything malformed. Corrupt storage
  // yields an empty list rather than an error: timers are a convenience and
  // must never stop the page working.
  function restore(text) {
    var parsed;
    try {
      parsed = JSON.parse(text);
    } catch (e) {
      return [];
    }
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(function (t) {
      return t && typeof t.step === 'string' && typeof t.label === 'string' &&
        Number.isFinite(t.endAt) && Number.isFinite(t.seconds);
    }).map(function (t) {
      var timer = { step: t.step, label: t.label, endAt: t.endAt, seconds: t.seconds, notified: t.notified === true };
      if (Number.isFinite(t.pausedMs) && t.pausedMs >= 0) timer.pausedMs = t.pausedMs;
      return timer;
    });
  }

  var api = {
    start: start, remainingMs: remainingMs, isDone: isDone, isPaused: isPaused,
    pause: pause, resume: resume, update: update, format: format, upsert: upsert,
    remove: remove, due: due, markNotified: markNotified, save: save, restore: restore,
  };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.CookTimers = api;
})(typeof window !== 'undefined' ? window : globalThis);
