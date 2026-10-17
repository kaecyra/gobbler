// Fits the time ribbon to its width (docs/ui/design.md, Time ribbon).
//
// The server prints every ribbon at two merge levels and shows the first. This
// script only chooses, as the mockup's fitRib does: level 0 shows every step,
// level 1 the merged segments, level 2 the merged segments with tighter
// padding. It takes the first level that fits on one line. If none does, it
// leaves the ribbon at level 2 and lets it wrap, so nothing is clipped.
// Without this script the ribbon shows level 0 and wraps when narrow.
(function () {
  'use strict';

  function overflows(el) {
    return el.scrollWidth > el.clientWidth + 1;
  }

  // Fixed height and one line while measuring; wrapping when nothing fits.
  function fit(el) {
    // A hidden ribbon has no width to measure; it is fitted when it shows.
    if (!el.clientWidth) return;
    el.setAttribute('data-fit', '');
    for (var level = 0; level <= 2; level++) {
      el.setAttribute('data-level', String(level));
      if (!overflows(el)) return;
    }
    el.removeAttribute('data-fit');
  }

  // Wrapping changes the height, so only a change of width refits; otherwise
  // the observer would answer its own change.
  var observer = typeof ResizeObserver === 'function'
    ? new ResizeObserver(function (entries) {
        entries.forEach(function (entry) {
          var el = entry.target;
          var width = el.clientWidth;
          if (el.__ribWidth === width) return;
          el.__ribWidth = width;
          fit(el);
        });
      })
    : null;

  function scan(root) {
    var ribbons = Array.prototype.slice.call(root.querySelectorAll('[data-rib]'));
    if (root.matches && root.matches('[data-rib]')) ribbons.push(root);
    ribbons.forEach(function (el) {
      // Observing fires once at start, which does the first fit.
      if (observer) observer.observe(el);
      else fit(el);
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
