// The recipe editor in the browser (internal/web/editor). The form works
// without this file: every structural button posts the whole form and the
// server answers with the form again. This adds two things.
//
// Keeping the place. HTMX swaps the whole form (#editor) after a structural
// action. Before the request this notes the scroll position and which rows the
// person had opened; after the swap it opens them again, puts the page back
// where it was and focuses the control the action made (the server marks it
// autofocus), without scrolling to it unless it is out of view.
//
// Drag to reorder. Each draggable row has a .grip handle inside a
// [data-drag-list] container whose value is the action's verb and position,
// such as "move-line:0" or "move-equip". Dragging moves the row in the page
// as a preview; on release the form is posted with the action
// "<verb>:<position>.<from>.<to>", and the server does the reordering. Field
// names are positional and unchanged by the preview, so what posts is the
// form as it was. Pointer events cover a mouse, a pen and a finger on an iPad.
//
// HTMX runs the scripts of swapped content again, so this file can execute
// many times in one document. Only the first run installs the listeners.
(function () {
  'use strict';

  if (window.gobblerEditor) return;
  window.gobblerEditor = true;
  if (window.PointerEvent) document.documentElement.classList.add('editor-drag');

  function form() { return document.getElementById('editor'); }

  // ---- keeping the place across a swap ----

  var kept = null;

  // keyOf names the row a disclosure belongs to: by its stored key or step id
  // when it has one, which survives a move, else by its position.
  function keyOf(details) {
    var li = details.closest('li.item');
    if (!li) return '';
    var el = li.querySelector('[data-item]');
    if (el && el.getAttribute('data-item')) return 'k:' + el.getAttribute('data-item');
    var anchor = li.querySelector('[id^="line-"], [id^="step-"], [id^="equip-"]');
    return anchor ? 'i:' + anchor.id : '';
  }

  function find(f, key) {
    if (key.indexOf('k:') === 0) {
      var want = key.slice(2);
      var all = f.querySelectorAll('[data-item]');
      for (var i = 0; i < all.length; i++) {
        if (all[i].getAttribute('data-item') === want) return all[i];
      }
      return null;
    }
    return document.getElementById(key.slice(2));
  }

  document.addEventListener('htmx:beforeRequest', function (e) {
    var f = form();
    if (!f || e.target !== f) return;
    var open = [];
    f.querySelectorAll('details.more[open]').forEach(function (d) {
      var k = keyOf(d);
      if (k) open.push(k);
    });
    var meta = f.querySelector('details.more-meta');
    kept = { y: window.scrollY, open: open, meta: !!(meta && meta.open) };
  });

  document.addEventListener('htmx:afterSettle', function () {
    var f = form();
    if (!f || !kept) return;
    var k = kept;
    kept = null;
    k.open.forEach(function (key) {
      var el = find(f, key);
      var li = el && el.closest('li.item');
      var d = li && li.querySelector('details.more');
      if (d) d.open = true;
    });
    var meta = f.querySelector('details.more-meta');
    if (meta && k.meta) meta.open = true;
    window.scrollTo(0, k.y);
    var focus = f.querySelector('[autofocus]');
    if (focus) {
      focus.focus({ preventScroll: true });
      var r = focus.getBoundingClientRect();
      if (r.top < 0 || r.bottom > window.innerHeight) focus.scrollIntoView({ block: 'center' });
    }
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

  document.addEventListener('pointermove', function (e) {
    if (!drag || e.pointerId !== drag.id) return;
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

  document.addEventListener('pointerup', function (e) { end(e, false); });
  document.addEventListener('pointercancel', function (e) { end(e, true); });
})();
