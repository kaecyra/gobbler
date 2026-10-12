// Shopping pages: the live phone view and the copy button. Plain JavaScript
// because HTMX has no server-sent-events extension in this build (ADR-00002).
// Without it the pages still work: a tap on an item posts its form and the
// server redirects back.
(function () {
  'use strict';

  var live = document.getElementById('live-items');
  var status = document.getElementById('live-status');

  function setStatus(text, state) {
    if (!status) return;
    status.textContent = text;
    status.setAttribute('data-state', state);
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

  if (live && window.EventSource) {
    var source = new EventSource(live.getAttribute('data-events'));
    // Every connection, the first and each reconnect, begins with the whole
    // list, so a view that missed changes while it was away catches up.
    source.addEventListener('list', function (e) {
      live.innerHTML = e.data;
      setStatus('Live', 'live');
    });
    source.addEventListener('gone', function () {
      source.close();
      setStatus('This list was deleted.', 'error');
    });
    source.onerror = function () {
      setStatus('Reconnecting', 'error');
    };
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
