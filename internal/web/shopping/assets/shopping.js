// Shopping pages: the live phone view and the copy button. Plain JavaScript
// because HTMX has no server-sent-events extension in this build (ADR-00002).
// Without it the pages still work: a tap on an item posts its form and the
// server redirects back.
(function () {
  'use strict';

  var live = document.getElementById('live-items');
  var status = document.getElementById('live-status');
  var reload = document.getElementById('live-reload');

  function setStatus(text, state) {
    if (!status) return;
    status.textContent = text;
    status.setAttribute('data-state', state);
    // A reload is offered while the view is not live, because a stream that
    // keeps failing may mean the Access session ended, which a new page load
    // renews.
    if (reload) reload.hidden = state === 'live' || state === 'connecting';
  }

  if (reload) {
    reload.addEventListener('click', function () { window.location.reload(); });
  }

  // A tap posts the item's form in the background. The new state comes back on
  // the event stream, to this view and to every other open one.
  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (!form.matches || !form.matches('form[data-check]')) return;
    e.preventDefault();
    fetch(form.action, {
      method: 'POST',
      headers: { 'X-Requested-With': 'fetch' },
      body: new URLSearchParams(new FormData(form)),
    }).then(function (res) {
      if (!res.ok) setStatus('Could not save that tap. Try again.', 'error');
    }, function () {
      setStatus('Offline. Reconnecting.', 'error');
    });
  });

  var RETRY_MIN = 1000;
  var RETRY_MAX = 30000;

  function follow(url) {
    var delay = RETRY_MIN;
    var source;

    function connect() {
      source = new EventSource(url);
      // Every connection, the first and each reconnect, begins with the whole
      // list, so a view that missed changes while it was away catches up.
      source.addEventListener('list', function (e) {
        delay = RETRY_MIN;
        live.innerHTML = e.data;
        setStatus('Live', 'live');
      });
      source.addEventListener('gone', function () {
        source.close();
        setStatus('This list was deleted.', 'error');
      });
      source.onerror = function () {
        if (source.readyState !== EventSource.CLOSED) {
          // The browser is retrying the dropped connection itself.
          setStatus('Reconnecting', 'error');
          return;
        }
        // The browser gave up: the answer was not an event stream (a 502 while
        // the server restarts, or a redirect to the Access login). It never
        // retries that, so do it here, slower each time up to a cap.
        source.close();
        setStatus('Reconnecting in ' + Math.round(delay / 1000) + 's', 'error');
        setTimeout(connect, delay);
        delay = Math.min(delay * 2, RETRY_MAX);
      };
    }

    connect();
  }

  if (live && window.EventSource) {
    follow(live.getAttribute('data-events'));
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest ? e.target.closest('button[data-copy]') : null;
    if (!btn) return;
    var field = document.getElementById(btn.getAttribute('data-copy'));
    if (!field) return;
    field.select();
    var done = function () { btn.textContent = 'Copied'; };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(field.value).then(done, function () {
        btn.textContent = 'Press copy on the selected text';
      });
    } else {
      btn.textContent = 'Press copy on the selected text';
    }
  });
})();
