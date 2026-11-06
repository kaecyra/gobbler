// The recipe editor in the browser (internal/web/editor). The form works
// without this file: every structural button posts the whole form and the
// server answers with the form again. This adds what follows, and an Enter
// key that presses Add equipment in its field.
//
// Structural actions morph the returned form into the page (ADR-00038), so
// scroll, focus and caret survive. This keeps opened rows open and focuses the
// control the server marked autofocus.
//
// Drag to reorder. Each draggable row has a .grip handle inside a
// [data-drag-list] container whose value is the action's verb and position,
// such as "move-line:0" or "move-equip". Dragging moves the row in the page
// as a preview; on release the form is posted with the action
// "<verb>:<position>.<from>.<to>", and the server does the reordering. Field
// names are positional and unchanged by the preview, so what posts is the
// form as it was. Pointer events cover a mouse, a pen and a finger on an iPad.
//
// Lists that share a data-drag-group (the lines of every part, the steps of
// every part) take each other's rows: a row dragged over another part's list
// previews there, and on release the action is
// "<verb>:<from part>.<from>.<to part>.<to>".
//
// A part's name is typed in two inputs, under Ingredients and under Steps. Both
// carry data-part-name; only the one under Ingredients has a name attribute, so
// the form posts one value, and typing in either copies it to the other.
//
// Suggestion lists. The unit, ingredient and equipment fields carry a
// <datalist>, which the browser filters to the entries matching the text
// already in the box and marks with a dropdown arrow, so a unit set to "cup"
// offered "cup" alone. This replaces it with a list of our own that opens
// showing every suggestion and narrows as the person types. The field stays
// free text. Without this script the datalist is left alone and works as before.
//
// Guarded so a re-run installs nothing twice.
(function () {
  'use strict';

  if (window.gobblerEditor) return;
  window.gobblerEditor = true;
  if (window.PointerEvent) document.documentElement.classList.add('editor-drag');

  function form() { return document.getElementById('editor'); }

  // ---- keeping opened rows open, focusing the new control ----

  if (window.Idiomorph) {
    window.Idiomorph.defaults.callbacks.beforeAttributeUpdated = function (attr, el, type) {
      return !(attr === 'open' && type === 'remove' && el.matches('.editor details.more'));
    };
  }

  document.addEventListener('htmx:afterSettle', function () {
    var f = form();
    var focus = f && f.querySelector('[autofocus]');
    if (!focus || focus === document.activeElement) return;
    focus.focus({ preventScroll: true });
    var r = focus.getBoundingClientRect();
    if (r.top < 0 || r.bottom > window.innerHeight) focus.scrollIntoView({ block: 'center' });
  });

  // ---- drag to reorder ----

  function itemsOf(list) {
    return Array.prototype.filter.call(list.children, function (el) {
      var g = el.querySelector('.grip');
      return g && g.closest('[data-drag-list]') === list;
    });
  }

  function itemFor(grip, list) {
    var el = grip;
    while (el && el.parentElement !== list) el = el.parentElement;
    return el;
  }

  var drag = null;

  // listsOf is the lists a row dragged from list may land in: its own group
  // across every part, or just list when it has none.
  function listsOf(list) {
    var group = list.getAttribute('data-drag-group');
    if (!group) return [list];
    return Array.prototype.filter.call(
      list.closest('form').querySelectorAll('[data-drag-list]'),
      function (l) { return l.getAttribute('data-drag-group') === group; }
    );
  }

  // The list under the pointer: the one whose part (the whole box, so an empty
  // list is a target too) holds y. Between parts it is the list the row is in.
  function listAt(y) {
    var found = drag.list;
    drag.lists.forEach(function (l) {
      var box = l.closest('.comp') || l;
      var r = box.getBoundingClientRect();
      if (y >= r.top && y < r.bottom) found = l;
    });
    return found;
  }

  // position is the number after the verb's colon: the part a list belongs to.
  function partOf(list) {
    var v = list.getAttribute('data-drag-list');
    return v.slice(v.indexOf(':') + 1);
  }

  document.addEventListener('pointerdown', function (e) {
    var grip = e.target.closest && e.target.closest('.editor .grip');
    if (!grip || e.button > 0) return;
    var list = grip.closest('[data-drag-list]');
    var item = list && itemFor(grip, list);
    if (!item) return;
    e.preventDefault();
    grip.setPointerCapture(e.pointerId);
    var rect = item.getBoundingClientRect();
    drag = {
      grip: grip, list: list, item: item, id: e.pointerId,
      from: itemsOf(list).indexOf(item), next: item.nextSibling, grab: e.clientY - rect.top,
      origin: list, lists: listsOf(list),
    };
    item.classList.add('dragging');
  });

  // moveAcross is the preview for a list of chips that wraps across lines
  // (data-drag-axis="x"): the chip moves next to the one nearest the pointer,
  // before it or after it by which half the pointer is over.
  function moveAcross(e) {
    var item = drag.item;
    var best = null;
    var dist = Infinity;
    itemsOf(drag.list).forEach(function (el) {
      if (el === item) return;
      var r = el.getBoundingClientRect();
      var dx = e.clientX - (r.left + r.width / 2);
      var dy = e.clientY - (r.top + r.height / 2);
      var d = dx * dx + dy * dy;
      if (d < dist) { dist = d; best = { el: el, before: dx < 0 }; }
    });
    if (!best) return;
    var r = item.getBoundingClientRect();
    if (e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom) return;
    drag.list.insertBefore(item, best.before ? best.el : best.el.nextSibling);
  }

  document.addEventListener('pointermove', function (e) {
    if (!drag || e.pointerId !== drag.id) return;
    if (drag.list.getAttribute('data-drag-axis') === 'x') {
      moveAcross(e);
      return;
    }
    var y = e.clientY;
    var item = drag.item;
    item.style.transform = '';
    var over = listAt(y);
    if (over !== drag.list) {
      drag.list = over;
      over.appendChild(item);
    }
    var items = itemsOf(drag.list);
    var i = items.indexOf(item);
    var moved = true;
    while (moved) {
      moved = false;
      var prev = items[i - 1];
      var next = items[i + 1];
      if (prev) {
        var p = prev.getBoundingClientRect();
        if (y < p.top + p.height / 2) {
          drag.list.insertBefore(item, prev);
          items = itemsOf(drag.list);
          i--;
          moved = true;
          continue;
        }
      }
      if (next) {
        var n = next.getBoundingClientRect();
        if (y > n.top + n.height / 2) {
          drag.list.insertBefore(next, item);
          items = itemsOf(drag.list);
          i++;
          moved = true;
        }
      }
    }
    var top = item.getBoundingClientRect().top;
    item.style.transform = 'translateY(' + (y - drag.grab - top) + 'px)';
    if (y < 80) window.scrollBy(0, -12);
    else if (y > window.innerHeight - 90) window.scrollBy(0, 12);
  });

  function end(e, cancelled) {
    if (!drag || e.pointerId !== drag.id) return;
    var d = drag;
    drag = null;
    d.item.classList.remove('dragging');
    d.item.style.transform = '';
    var to = itemsOf(d.list).indexOf(d.item);
    if (cancelled || (d.list === d.origin && to === d.from)) {
      d.origin.insertBefore(d.item, d.next);
      return;
    }
    var base = d.origin.getAttribute('data-drag-list');
    var action = base + (base.indexOf(':') >= 0 ? '.' : ':') + d.from + '.' + to;
    if (d.list !== d.origin) action = base + '.' + d.from + '.' + partOf(d.list) + '.' + to;
    var f = d.list.closest('form');
    var b = document.createElement('button');
    b.type = 'submit';
    b.name = 'action';
    b.value = action;
    b.hidden = true;
    f.appendChild(b);
    // A click, not requestSubmit(b): htmx takes the submitter from the button
    // clicked last, and a requestSubmit submitter reaches it as no action at
    // all, which the server reads as Save.
    b.click();
  }

  // Enter in a field marked data-enter presses the structural button beside
  // it, such as Add equipment, rather than the form's default Save. Without a
  // script Enter saves, and what was typed is saved with the recipe.
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' || e.isComposing) return;
    var input = e.target.closest && e.target.closest('.editor [data-enter]');
    if (!input) return;
    var b = input.parentElement.querySelector('button[name="action"][value="' + input.getAttribute('data-enter') + '"]');
    if (!b) return;
    e.preventDefault();
    b.click();
  });

  document.addEventListener('input', function (e) {
    var name = e.target.closest && e.target.closest('.editor [data-part-name]');
    if (!name) return;
    var same = document.querySelectorAll('.editor [data-part-name="' + name.getAttribute('data-part-name') + '"]');
    Array.prototype.forEach.call(same, function (el) { if (el !== name) el.value = name.value; });
  });

  // ---- suggestion lists ----

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
    Array.prototype.forEach.call(document.querySelectorAll('.editor input[list]'), enhance);
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
    if (t.matches && t.matches('.editor input[list]')) enhance(t);
    if (t.matches && t.matches('.editor input[data-suggest]')) open(t);
    else if (current && t !== current) close();
  });
  // A second tap on a field that is already focused opens the list again after
  // Escape or a choice.
  document.addEventListener('pointerdown', function (e) {
    var t = e.target;
    if (t.closest && t.closest('#gb-suggest')) return;
    if (t.matches && t.matches('.editor input[data-suggest]')) {
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
    if (!t.matches || !t.matches('.editor input[data-suggest]') || e.isComposing) return;
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

  document.addEventListener('pointerup', function (e) { end(e, false); });
  document.addEventListener('pointercancel', function (e) { end(e, true); });
})();
