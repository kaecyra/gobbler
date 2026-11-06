// Suggestion lists, shared by the recipe editor and the catalog item form.
// A field with a list="<id>" attribute carries a <datalist>, which the browser
// filters to the entries matching the text already in the box and marks with a
// dropdown arrow, so a unit set to "cup" offered "cup" alone. This replaces it
// with a list of our own that opens showing every suggestion and narrows as the
// person types. The field stays free text. Without this script the datalist is
// left alone and works as before.
//
// A page opts in by loading this script and styling the list with
// suggest.css. Load it after editor.js: both wrap Idiomorph's attribute
// callback, and the wrap here must be the outer one.
//
// Guarded so a re-run installs nothing twice.
(function () {
  'use strict';

  if (window.gobblerSuggest) return;
  window.gobblerSuggest = true;


  var pop = null;      // the one shared listbox, in the body so a morph never removes it
  var current = null;  // the input it is open for
  var active = -1;     // index of the highlighted option

  // enhance takes an input off the native datalist and makes it a combobox.
  function enhance(input) {
    var id = input.getAttribute('list');
    if (!id) return;
    input.setAttribute('data-suggest', id);
    input.removeAttribute('list');
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-autocomplete', 'list');
    input.setAttribute('aria-expanded', 'false');
    input.setAttribute('aria-haspopup', 'listbox');
    input.setAttribute('aria-controls', 'gb-suggest');
    popup();
  }

  function enhanceAll() {
    Array.prototype.forEach.call(document.querySelectorAll('input[list]'), enhance);
  }

  function popup() {
    if (pop) return pop;
    pop = document.createElement('div');
    pop.id = 'gb-suggest';
    pop.className = 'suggest';
    pop.setAttribute('role', 'listbox');
    pop.hidden = true;
    // Keep the field focused while the list is pressed, so the field does not
    // blur and close the list before the click lands.
    pop.addEventListener('mousedown', function (e) { e.preventDefault(); });
    pop.addEventListener('click', function (e) {
      var o = e.target.closest('[role=option]');
      if (o && current) choose(o.textContent);
    });
    document.body.appendChild(pop);
    return pop;
  }

  function optionsFor(input) {
    var dl = document.getElementById(input.getAttribute('data-suggest'));
    if (!dl) return [];
    return Array.prototype.map.call(dl.options, function (o) { return o.value; });
  }

  function place() {
    if (!current || pop.hidden) return;
    var r = current.getBoundingClientRect();
    var below = window.innerHeight - r.bottom - 8;
    var above = r.top - 8;
    var up = below < 180 && above > below;
    pop.style.left = Math.max(8, Math.min(r.left, window.innerWidth - 8 - Math.max(r.width, 160))) + 'px';
    pop.style.minWidth = Math.min(Math.max(r.width, 160), window.innerWidth - 16) + 'px';
    pop.style.maxHeight = Math.max(120, Math.min(320, up ? above : below)) + 'px';
    if (up) { pop.style.top = ''; pop.style.bottom = (window.innerHeight - r.top + 2) + 'px'; }
    else { pop.style.bottom = ''; pop.style.top = (r.bottom + 2) + 'px'; }
  }

  // render fills the list: every suggestion when filter is false (just opened),
  // otherwise the ones containing the typed text.
  function render(filter) {
    var input = current;
    var q = filter ? input.value.trim().toLowerCase() : '';
    var value = input.value.trim().toLowerCase();
    var shown = optionsFor(input).filter(function (v) { return !q || v.toLowerCase().indexOf(q) >= 0; });
    pop.textContent = '';
    var selected = null;
    shown.forEach(function (v, i) {
      var o = document.createElement('div');
      o.id = 'gb-suggest-' + i;
      o.setAttribute('role', 'option');
      o.textContent = v;
      var on = !filter && v.toLowerCase() === value;
      o.setAttribute('aria-selected', on ? 'true' : 'false');
      if (on) selected = o;
      pop.appendChild(o);
    });
    active = -1;
    input.removeAttribute('aria-activedescendant');
    pop.hidden = shown.length === 0;
    input.setAttribute('aria-expanded', pop.hidden ? 'false' : 'true');
    place();
    if (selected) selected.scrollIntoView({ block: 'nearest' });
  }

  // nameOf is what the listbox is called while open: the field's label, else
  // its placeholder.
  function nameOf(input) {
    var l = input.id && document.querySelector('label[for="' + input.id.replace(/"/g, '\\"') + '"]');
    return (l && l.textContent.trim()) || input.getAttribute('placeholder') || 'Suggestions';
  }

  function open(input) {
    if (!input.hasAttribute('data-suggest')) return;
    popup();
    pop.setAttribute('aria-label', nameOf(input));
    current = input;
    render(false);
  }

  function close() {
    if (!pop || !current) return;
    pop.hidden = true;
    current.setAttribute('aria-expanded', 'false');
    current.removeAttribute('aria-activedescendant');
    current = null;
    active = -1;
  }

  function isOpen() { return !!current && !pop.hidden; }

  function choose(value) {
    var input = current;
    close();
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }

  function highlight(i) {
    var opts = pop.querySelectorAll('[role=option]');
    if (!opts.length) return;
    active = (i + opts.length) % opts.length;
    Array.prototype.forEach.call(opts, function (o, n) { o.classList.toggle('active', n === active); });
    current.setAttribute('aria-activedescendant', opts[active].id);
    opts[active].scrollIntoView({ block: 'nearest' });
  }

  enhanceAll();
  document.addEventListener('DOMContentLoaded', enhanceAll);
  document.addEventListener('htmx:afterSettle', function () {
    enhanceAll();
    // A morph can remove the field the list is open for.
    if (current && !current.isConnected) close();
  });

  // A morph copies the server's list attribute back onto a field and drops the
  // attributes added here; leave them as they are.
  if (window.Idiomorph) {
    var prior = window.Idiomorph.defaults.callbacks.beforeAttributeUpdated;
    window.Idiomorph.defaults.callbacks.beforeAttributeUpdated = function (attr, el, type) {
      if (el.hasAttribute && el.hasAttribute('data-suggest') &&
          (attr === 'list' || attr === 'data-suggest' || attr === 'role' || attr.indexOf('aria-') === 0)) return false;
      return prior ? prior(attr, el, type) : true;
    };
  }

  document.addEventListener('focusin', function (e) {
    var t = e.target;
    if (t.matches && t.matches('input[list]')) enhance(t);
    if (t.matches && t.matches('input[data-suggest]')) open(t);
    else if (current && t !== current) close();
  });
  // A second tap on a field that is already focused opens the list again after
  // Escape or a choice.
  document.addEventListener('pointerdown', function (e) {
    var t = e.target;
    if (t.closest && t.closest('#gb-suggest')) return;
    if (t.matches && t.matches('input[data-suggest]')) {
      if (t === document.activeElement && t !== current) open(t);
    } else if (current) close();
  });
  document.addEventListener('input', function (e) {
    if (e.target === current) render(true);
  });
  document.addEventListener('focusout', function (e) {
    if (e.target === current) close();
  });
  window.addEventListener('resize', place);
  window.addEventListener('scroll', place, true);

  // Capture, so a key that picks an option never reaches the Enter handler
  // below, which would press Add equipment, or the browser's submit.
  document.addEventListener('keydown', function (e) {
    var t = e.target;
    if (!t.matches || !t.matches('input[data-suggest]') || e.isComposing) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (t !== current || pop.hidden) open(t);
      highlight(e.key === 'ArrowDown' ? active + 1 : (active < 0 ? -1 : active - 1));
    } else if (e.key === 'Enter' && isOpen() && active >= 0) {
      e.preventDefault();
      e.stopImmediatePropagation();
      choose(pop.children[active].textContent);
    } else if (e.key === 'Escape' && isOpen()) {
      e.preventDefault();
      e.stopImmediatePropagation();
      close();
    }
  }, true);
})();
