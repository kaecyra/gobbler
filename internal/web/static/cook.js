// Cook mode in the browser: step timers and the screen wake lock. HTMX swaps
// the step panel when stepping, so everything here listens on the document and
// the timer list lives outside the swapped panel. Timer arithmetic is in
// cook-timers.js.
(function () {
  'use strict';

  var T = window.CookTimers;
  var root = document.getElementById('cook');
  if (!root || !T) return;

  var list = document.getElementById('cook-timer-list');
  var emptyNote = document.getElementById('cook-timers-empty');
  var alertBox = document.getElementById('cook-alert');
  var wakeNote = document.getElementById('cook-wake');
  var storageKey = 'gobbler.cook.' + root.getAttribute('data-recipe') + '.timers';
  var baseTitle = document.title;

  // ---- timers ----
  var timers = [];
  var audio = null;

  function load() {
    try {
      timers = T.restore(window.sessionStorage.getItem(storageKey) || '[]');
    } catch (e) {
      timers = []; // storage blocked: timers still work for this page view
    }
  }

  function persist() {
    try {
      window.sessionStorage.setItem(storageKey, T.save(timers));
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

  function render() {
    var now = Date.now();
    emptyNote.hidden = timers.length > 0;
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
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.setAttribute('data-cook-timer-dismiss', t.step);
        btn.textContent = 'Dismiss';
        li.appendChild(label);
        li.appendChild(out);
        li.appendChild(btn);
        list.appendChild(li);
      }
      delete existing[t.step];
      li.querySelector('.cook-timer-label').textContent = t.label;
      var done = T.isDone(t, now);
      li.classList.toggle('done', done);
      li.querySelector('output').textContent = done ? 'Done' : T.format(T.remainingMs(t, now));
    });
    Object.keys(existing).forEach(function (k) { list.removeChild(existing[k]); });
  }

  function announce() {
    var finished = T.due(timers, Date.now());
    if (finished.length === 0) return;
    timers = T.markNotified(timers, finished.map(function (t) { return t.step; }));
    persist();
    alertBox.textContent = finished.map(function (t) { return 'Timer done: ' + t.label; }).join(' ');
    document.title = 'Timer done - ' + baseTitle;
    beep();
  }

  function tick() {
    announce();
    render();
  }

  document.addEventListener('click', function (e) {
    var startBtn = e.target.closest && e.target.closest('[data-cook-timer]');
    if (startBtn) {
      audioContext(); // created inside the click so the browser allows sound later
      var seconds = Number(startBtn.getAttribute('data-seconds'));
      if (!Number.isFinite(seconds) || seconds <= 0) return;
      timers = T.upsert(timers, T.start(startBtn.getAttribute('data-step'), startBtn.getAttribute('data-label'), seconds, Date.now()));
      persist();
      alertBox.textContent = '';
      document.title = baseTitle;
      tick();
      return;
    }
    var dismiss = e.target.closest && e.target.closest('[data-cook-timer-dismiss]');
    if (dismiss) {
      timers = T.remove(timers, dismiss.getAttribute('data-cook-timer-dismiss'));
      persist();
      alertBox.textContent = '';
      document.title = baseTitle;
      tick();
    }
  });

  load();
  tick();
  window.setInterval(tick, 500);
  // A backgrounded page may not have ticked; the end timestamps make catching
  // up exact.
  document.addEventListener('visibilitychange', tick);

  // ---- screen wake lock ----
  var sentinel = null;
  var wantLock = true;

  function note(text) {
    wakeNote.textContent = text;
    wakeNote.hidden = text === '';
  }

  function acquire() {
    if (!wantLock || sentinel) return;
    if (!('wakeLock' in navigator) || !navigator.wakeLock || !navigator.wakeLock.request) {
      note('Your browser cannot keep the screen awake, so it may dim while you cook.');
      return;
    }
    navigator.wakeLock.request('screen').then(function (s) {
      sentinel = s;
      note('');
      s.addEventListener('release', function () { if (sentinel === s) sentinel = null; });
    }, function () {
      note('The screen could not be kept awake (permission denied or low battery), so it may dim while you cook.');
    });
  }

  function release() {
    wantLock = false;
    if (sentinel) {
      var s = sentinel;
      sentinel = null;
      s.release().catch(function () { /* already released */ });
    }
  }

  document.addEventListener('visibilitychange', function () {
    // The browser drops the lock when the page is hidden; take it back.
    if (document.visibilityState === 'visible') acquire();
  });
  window.addEventListener('pagehide', release);
  document.addEventListener('click', function (e) {
    if (e.target.closest && e.target.closest('[data-cook-exit]')) release();
  });
  acquire();
})();
