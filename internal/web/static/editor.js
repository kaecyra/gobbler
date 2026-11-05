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
    if (cancelled || to === d.from) {
      d.list.insertBefore(d.item, d.next);
      return;
    }
    var base = d.list.getAttribute('data-drag-list');
    var action = base + (base.indexOf(':') >= 0 ? '.' : ':') + d.from + '.' + to;
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

  document.addEventListener('pointerup', function (e) { end(e, false); });
  document.addEventListener('pointercancel', function (e) { end(e, true); });
})();
