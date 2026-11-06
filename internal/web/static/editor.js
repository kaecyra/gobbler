// The recipe editor in the browser (internal/web/editor). The form works
// without this file: every structural button posts the whole form and the
// server answers with the form again. This adds what follows, and an Enter
// key that presses Add equipment in its field.
//
// Structural actions morph the returned form into the page (ADR-00038), so
// scroll, focus and caret survive. This keeps opened rows open and focuses the
// control the server marked autofocus. A refused save has none: this scrolls to
// the first error message and focuses it.
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
// The suggestion lists on the unit, ingredient and equipment fields are
// suggest.js, shared with the catalog form and loaded after this file.
//
// Guarded so a re-run installs nothing twice.
(function () {
  'use strict';

  if (window.gobblerEditor) return;
  window.gobblerEditor = true;
  if (window.PointerEvent) document.documentElement.classList.add('editor-drag');

  function form() { return document.getElementById('editor'); }

  // --ed-top is the site header's height: where the refused-save callout sticks
  // and how far the error links stop short of the top.
  function headerHeight() {
    var top = document.querySelector('.top');
    if (top) document.documentElement.style.setProperty('--ed-top', top.offsetHeight + 'px');
  }
  headerHeight();
  window.addEventListener('resize', headerHeight);

  // ---- keeping opened rows open, focusing the new control ----

  if (window.Idiomorph) {
    window.Idiomorph.defaults.callbacks.beforeAttributeUpdated = function (attr, el, type) {
      return !(attr === 'open' && type === 'remove' && el.matches('.editor details.more'));
    };
  }

  document.addEventListener('htmx:afterSettle', function () {
    var f = form();
    var focus = f && f.querySelector('[autofocus]');
    if (!focus && f && f.querySelector('[role=alert]')) {
      // A refused save comes back as a 200 with the messages inline, often far
      // below the fold: bring the first one into view and focus it.
      var first = f.querySelector('[role=alert]');
      first.setAttribute('tabindex', '-1');
      first.focus({ preventScroll: true });
      first.scrollIntoView({ block: 'center' });
      return;
    }
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

  document.addEventListener('pointerup', function (e) { end(e, false); });
  document.addEventListener('pointercancel', function (e) { end(e, true); });
})();
