// Crop step of the avatar upload (internal/web/profile/avatar.templ). Cropper.js
// (vendor/cropper.min.js) draws the square; this posts it as fractions of the
// picture, which the server applies to the original and checks again
// (ADR-00033). The step appears in the profile page's dialog after htmx swaps it
// in, or on the crop page when it loads. It also tells the person when the
// page's status line cannot be fetched.
(function () {
  'use strict';

  function clamp(n) {
    return Math.min(1, Math.max(0, n));
  }

  function setup(img) {
    var form = img.closest('[data-crop-form]') || document.querySelector('[data-crop-form]');
    if (!form || img.getAttribute('data-cropper') === 'on') return;
    img.setAttribute('data-cropper', 'on');

    function field(name) {
      return form.querySelector('[data-crop="' + name + '"]');
    }
    function record(cropper) {
      var d = cropper.getData();
      var nat = cropper.getImageData();
      if (!nat.naturalWidth || !nat.naturalHeight) return;
      var x = clamp(d.x / nat.naturalWidth);
      var y = clamp(d.y / nat.naturalHeight);
      var w = Math.min(clamp(d.width / nat.naturalWidth), 1 - x);
      var h = Math.min(clamp(d.height / nat.naturalHeight), 1 - y);
      if (w <= 0 || h <= 0) return;
      field('x').value = x;
      field('y').value = y;
      field('w').value = w;
      field('h').value = h;
    }
    function start() {
      var cropper = new Cropper(img, {
        aspectRatio: 1,
        viewMode: 1,
        dragMode: 'move',
        autoCropArea: 1,
        responsive: true,
        background: false,
        checkOrientation: false,
        crop: function () {
          record(cropper);
        },
      });
      form.setAttribute('data-crop-ready', 'true');
    }
    if (img.complete && img.naturalWidth) start();
    else img.addEventListener('load', start, { once: true });
  }

  function scan() {
    if (typeof Cropper === 'undefined') return;
    var img = document.getElementById('crop-image');
    if (img) setup(img);
  }
  // A step replaces the one the person was on, and the focused control with it.
  // Focus goes to the new step's heading, unless the step is in a dialog that
  // is not open.
  function focusStep(e) {
    var gone = e.detail && e.detail.target;
    if (!gone || gone.id !== 'avatar-panel') return;
    var panel = document.getElementById('avatar-panel');
    var dialog = panel && panel.closest('dialog');
    if (!panel || (dialog && !dialog.open)) return;
    var heading = panel.querySelector('[data-step-heading]');
    if (heading) heading.focus();
  }
  // The page's status line (avatarStatusSlot) loads and polls itself. When one
  // of those requests fails, htmx swaps nothing and the line would go on saying
  // the picture is processing; the message beside it says what happened instead.
  // The next status that arrives puts the line back.
  function statusProblem(e) {
    var target = e.detail && e.detail.target;
    var box = document.querySelector('[data-avatar-status-problem]');
    if (!target || target.id !== 'avatar-status' || !box) return;
    var status = e.detail.xhr ? e.detail.xhr.status : 0;
    var signedOut = e.type === 'htmx:responseError' && (status === 401 || status === 403);
    var kind = signedOut ? 'signed-out' : 'check';
    // The poll goes on failing every 2s; the alert is said once, not each time.
    if (!box.hidden && box.getAttribute('data-kind') === kind) return;
    box.setAttribute('data-kind', kind);
    var text = box.querySelector('[data-avatar-status-problem-text]');
    var reload = document.createElement('a');
    reload.className = 'link';
    reload.href = window.location.href;
    reload.textContent = 'Reload this page';
    box.querySelector('[data-avatar-status-problem-label]').textContent = signedOut ? 'Signed out' : 'Check';
    text.textContent = signedOut ? 'You are signed out. ' : 'We could not check on your picture. ';
    text.appendChild(reload);
    text.appendChild(document.createTextNode(signedOut ? ' to sign in again.' : ' to see where it stands.'));
    target.hidden = true;
    box.hidden = false;
  }
  function statusBack(e) {
    var target = e.detail && e.detail.target;
    var box = document.querySelector('[data-avatar-status-problem]');
    if (box && target && target.id === 'avatar-status') box.hidden = true;
  }
  ['htmx:responseError', 'htmx:sendError', 'htmx:timeout'].forEach(function (name) {
    document.body.addEventListener(name, statusProblem);
  });
  document.body.addEventListener('htmx:afterSwap', statusBack);
  document.body.addEventListener('htmx:oobAfterSwap', statusBack);

  scan();
  document.body.addEventListener('htmx:afterSettle', function (e) {
    scan();
    focusStep(e);
  });
})();
