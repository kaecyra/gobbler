// Fits the time ribbon to its width (docs/ui/design.md, Time ribbon).
//
// The server prints every ribbon at two merge levels and shows the first. This
// script only chooses: level 0 shows every step, level 1 the merged segments,
// level 2 the merged segments with tighter padding. It takes the first level
// that does not overflow. Without it the ribbon stays at level 0, complete but
// possibly cropped.
(function () {
  'use strict';

  function overflows(el) {
    return el.scrollWidth > el.clientWidth + 1;
  }

  function fit(el) {
    // A hidden ribbon has no width to measure; it is fitted when it shows.
    if (!el.clientWidth) return;
    var merges = el.querySelector('.rib-merged') !== null;
    var last = merges ? 2 : 0;
    for (var level = 0; level <= last; level++) {
      el.setAttribute('data-level', String(level));
      if (!overflows(el)) return;
    }
  }

  var observer = typeof ResizeObserver === 'function'
    ? new ResizeObserver(function (entries) {
        entries.forEach(function (entry) { fit(entry.target); });
      })
    : null;
  var seen = typeof WeakSet === 'function' ? new WeakSet() : null;

  function scan(root) {
    var ribbons = root.querySelectorAll('[data-rib]');
    Array.prototype.forEach.call(ribbons, function (el) {
      if (observer) {
        // Observing fires once at start, which does the first fit.
        if (seen && !seen.has(el)) {
          seen.add(el);
          observer.observe(el);
        }
      } else {
        fit(el);
      }
    });
  }

  function start() { scan(document); }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
  // Content swapped in by HTMX arrives after load.
  document.addEventListener('htmx:load', function (event) {
    if (event.target instanceof Element) scan(event.target);
  });
  if (!observer) window.addEventListener('resize', start);
})();
