// Cook mode in the browser: step timers and the screen wake lock. HTMX swaps
// the step panel when stepping, so everything here listens on the document and
// the timer list lives outside the swapped panel. Timer arithmetic is in
// cook-timers.js.
//
// htmx restores history (Back) by swapping the body and running its scripts
// again, so this file can execute many times in one document. Only the first
// run installs listeners and the interval; later runs just refresh the page.
// Elements are looked up by id when used, never cached, because a restore
// replaces them.
(function () {
  'use strict';

  if (window.gobblerCook) {
    window.gobblerCook.refresh();
    return;
  }
  var T = window.CookTimers;
  if (!T) return;

  function root() { return document.getElementById('cook'); }
  function byId(id) { return document.getElementById(id); }
  function storageKey() {
    var r = root();
    return r ? 'gobbler.cook.' + r.getAttribute('data-recipe') + '.timers' : '';
  }

  // ---- timers ----
  var timers = [];
  var loadedKey = '';
  var audio = null;
  var baseTitle = document.title;

  function load() {
    loadedKey = storageKey();
    try {
      timers = loadedKey ? T.restore(window.sessionStorage.getItem(loadedKey) || '[]') : [];
    } catch (e) {
      timers = []; // storage blocked: timers still work for this page view
    }
  }

  function persist() {
    if (!loadedKey) return;
    try {
      window.sessionStorage.setItem(loadedKey, T.save(timers));
    } catch (e) {
      // Storage is a convenience (timers survive a reload); not having it is fine.
    }
  }

  function audioContext() {
    if (audio) return audio;
    var Ctx = window.AudioContext || window.webkitAudioContext;
    if (Ctx) {
      try { audio = new Ctx(); } catch (e) { audio = null; }
    }
    return audio;
  }

  function beep() {
    var ctx = audioContext();
    if (!ctx) return;
    try {
      if (ctx.state === 'suspended') ctx.resume();
      for (var i = 0; i < 3; i++) {
        var osc = ctx.createOscillator();
        var gain = ctx.createGain();
        osc.frequency.value = 880;
        gain.gain.value = 0.2;
        osc.connect(gain);
        gain.connect(ctx.destination);
        var at = ctx.currentTime + i * 0.4;
        osc.start(at);
        osc.stop(at + 0.2);
      }
    } catch (e) {
      // Audio is best effort: the visible alert is the guaranteed signal.
    }
    if (navigator.vibrate) navigator.vibrate([200, 100, 200]);
  }

  // Icon-only controls, drawn like the rest of the cook page: 24px grid,
  // stroke in currentColor. The accessible name is the button's aria-label.
  var ICON_PAUSE = 'M9 6v12M15 6v12';
  var ICON_PLAY = 'M8 5l11 7-11 7z';
  var ICON_CLOSE = 'M6 6l12 12M18 6L6 18';

  function icon(d) {
    var ns = 'http://www.w3.org/2000/svg';
    var svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('fill', 'none');
    svg.setAttribute('stroke', 'currentColor');
    svg.setAttribute('stroke-width', '2');
    svg.setAttribute('stroke-linecap', 'round');
    svg.setAttribute('stroke-linejoin', 'round');
    svg.setAttribute('aria-hidden', 'true');
    svg.appendChild(document.createElementNS(ns, 'path')).setAttribute('d', d);
    return svg;
  }

  function setIcon(btn, d) {
    var path = btn.querySelector('path');
    if (path) path.setAttribute('d', d);
    else btn.appendChild(icon(d));
  }

  function renderList(now) {
    var list = byId('cook-timer-list');
    if (!list) return;
    var emptyNote = byId('cook-timers-empty');
    if (emptyNote) emptyNote.hidden = timers.length > 0;
    var existing = {};
    Array.prototype.forEach.call(list.children, function (li) { existing[li.getAttribute('data-step')] = li; });
    timers.forEach(function (t) {
      var li = existing[t.step];
      if (!li) {
        li = document.createElement('li');
        li.setAttribute('data-step', t.step);
        li.setAttribute('data-testid', 'timer');
        var label = document.createElement('span');
        label.className = 'cook-timer-label';
        var out = document.createElement('output');
        out.className = 'cook-timer-remaining';
        var actions = document.createElement('span');
        actions.className = 'cook-timer-actions';
        var toggle = document.createElement('button');
        toggle.type = 'button';
        toggle.className = 'cook-timer-toggle';
        toggle.setAttribute('data-cook-timer-toggle', t.step);
        toggle.setAttribute('data-testid', 'timer-toggle');
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.setAttribute('data-cook-timer-dismiss', t.step);
        btn.className = 'cook-timer-dismiss';
        btn.setAttribute('data-testid', 'timer-dismiss');
        btn.appendChild(icon(ICON_CLOSE));
        actions.appendChild(toggle);
        actions.appendChild(btn);
        li.appendChild(label);
        li.appendChild(out);
        li.appendChild(actions);
        list.appendChild(li);
      }
      delete existing[t.step];
      li.querySelector('.cook-timer-label').textContent = t.label;
      var done = T.isDone(t, now);
      li.classList.toggle('done', done);
      var paused = T.isPaused(t);
      li.classList.toggle('paused', paused);
      li.querySelector('output').textContent = done ? 'Done' : T.format(T.remainingMs(t, now));
      var toggle = li.querySelector('.cook-timer-toggle');
      toggle.hidden = done;
      setIcon(toggle, paused ? ICON_PLAY : ICON_PAUSE);
      li.querySelector('.cook-timer-dismiss').setAttribute('aria-label', 'Dismiss ' + t.label + ' timer');
      toggle.setAttribute('aria-label', (paused ? 'Resume ' : 'Pause ') + t.label + ' timer');
    });
    Object.keys(existing).forEach(function (k) { list.removeChild(existing[k]); });
  }

  // A running timer's button says so, so a second tap reads as a restart.
  function renderButtons(now) {
    Array.prototype.forEach.call(document.querySelectorAll('[data-cook-timer]'), function (btn) {
      var step = btn.getAttribute('data-step');
      var running = timers.some(function (t) { return t.step === step && !T.isDone(t, now); });
      btn.textContent = (running ? 'Restart timer' : 'Start timer') + ' (' + btn.getAttribute('data-duration') + ')';
    });
  }

  function announce() {
    var finished = T.due(timers, Date.now());
    if (finished.length === 0) return;
    timers = T.markNotified(timers, finished.map(function (t) { return t.step; }));
    persist();
    var alertBox = byId('cook-alert');
    if (alertBox) alertBox.textContent = finished.map(function (t) { return 'Timer done: ' + t.label; }).join(' ');
    document.title = 'Timer done - ' + baseTitle;
    beep();
  }

  function clearAlert() {
    var alertBox = byId('cook-alert');
    if (alertBox) alertBox.textContent = '';
    document.title = baseTitle;
  }

  function tick() {
    if (!root()) return; // not on a cook page (restored page is something else)
    if (storageKey() !== loadedKey) load();
    announce();
    var now = Date.now();
    renderList(now);
    renderButtons(now);
  }

  document.addEventListener('click', function (e) {
    var startBtn = e.target.closest && e.target.closest('[data-cook-timer]');
    if (startBtn) {
      audioContext(); // created inside the click so the browser allows sound later
      var seconds = Number(startBtn.getAttribute('data-seconds'));
      if (!Number.isFinite(seconds) || seconds <= 0) return;
      timers = T.upsert(timers, T.start(startBtn.getAttribute('data-step'), startBtn.getAttribute('data-label'), seconds, Date.now()));
      persist();
      clearAlert();
      tick();
      return;
    }
    var toggle = e.target.closest && e.target.closest('[data-cook-timer-toggle]');
    if (toggle) {
      var now = Date.now();
      timers = T.update(timers, toggle.getAttribute('data-cook-timer-toggle'), function (t) {
        return T.isPaused(t) ? T.resume(t, now) : T.pause(t, now);
      });
      persist();
      tick();
      return;
    }
    var dismiss = e.target.closest && e.target.closest('[data-cook-timer-dismiss]');
    if (dismiss) {
      timers = T.remove(timers, dismiss.getAttribute('data-cook-timer-dismiss'));
      persist();
      clearAlert();
      tick();
    }
  });

  // A backgrounded page may not have ticked; the end timestamps make catching
  // up exact. After htmx swaps in a new step panel its buttons need their text.
  document.addEventListener('visibilitychange', tick);
  document.addEventListener('htmx:afterSettle', tick);
  window.setInterval(tick, 500);

  // ---- screen wake lock ----
  var sentinel = null;
  var requesting = false;

  function note(text) {
    var el = byId('cook-wake');
    if (!el) return;
    el.textContent = text;
    el.hidden = text === '';
  }

  function acquire() {
    if (!root() || sentinel || requesting) return;
    if (!('wakeLock' in navigator) || !navigator.wakeLock || !navigator.wakeLock.request) {
      note('Your browser cannot keep the screen awake, so it may dim while you cook.');
      return;
    }
    requesting = true;
    navigator.wakeLock.request('screen').then(function (s) {
      requesting = false;
      sentinel = s;
      note('');
      s.addEventListener('release', function () { if (sentinel === s) sentinel = null; });
    }, function () {
      requesting = false;
      note('The screen could not be kept awake (permission denied or low battery), so it may dim while you cook.');
    });
  }

  function release() {
    if (sentinel) {
      var s = sentinel;
      sentinel = null;
      s.release().catch(function () { /* already released */ });
    }
  }

  // Leaving releases the lock, but leaving can be undone: a page restored from
  // the back/forward cache, a cancelled exit, or a tab brought back all need
  // it again, so every way back in re-requests it.
  document.addEventListener('visibilitychange', function () {
    // The browser drops the lock when the page is hidden; take it back.
    if (document.visibilityState === 'visible') acquire();
  });
  window.addEventListener('pageshow', acquire);
  window.addEventListener('focus', acquire);
  window.addEventListener('pagehide', release);
  document.addEventListener('click', function (e) {
    if (e.target.closest && e.target.closest('[data-cook-exit]')) release();
  });

  window.gobblerCook = {
    refresh: function () {
      tick();
      acquire();
    },
  };
  window.gobblerCook.refresh();
})();
