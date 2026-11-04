// Avatar dialog of the profile page (internal/web/profile/avatar.templ). The
// avatar is a link to the upload page; this turns it into a button that opens
// the system's file chooser straight away. Choosing a file opens the native
// modal <dialog>, which closes on Escape by itself, says the picture is loading
// and uploads it, so the next thing the person sees is the crop. Without
// scripts, or without <dialog>, the link works as a link. Any failure of a
// request made from the dialog is shown in it: htmx swaps nothing on an error
// status or a dropped connection, so without this the person would see no
// change at all.
(function () {
  'use strict';
  var dialog = document.getElementById('avatar-dialog');
  var open = document.querySelector('[data-avatar-open]');
  if (!dialog || !open || typeof dialog.showModal !== 'function') return;
  dialog.setAttribute('data-enhanced', 'true');

  var errorBox = dialog.querySelector('[data-avatar-error]');
  var errorText = dialog.querySelector('[data-avatar-error-text]');
  var uploadStep = open.getAttribute('href');
  var statusSrc = dialog.getAttribute('data-status-src');

  function showError(text) {
    stopLoading();
    errorText.textContent = text;
    errorBox.hidden = false;
  }
  function clearError() {
    errorBox.hidden = true;
    errorText.textContent = '';
  }
  // startOver puts the first step back, without drawing the page again.
  function startOver() {
    clearError();
    if (typeof htmx !== 'undefined') {
      htmx.ajax('GET', uploadStep, { target: '#avatar-panel', swap: 'outerHTML' });
    }
  }
  // refreshStatus asks again where the last upload stands, for the line on the
  // page: closing the dialog can leave a picture waiting to be cropped.
  function refreshStatus() {
    if (typeof htmx !== 'undefined' && statusSrc && document.getElementById('avatar-status')) {
      htmx.ajax('GET', statusSrc, { target: '#avatar-status', swap: 'outerHTML' });
    }
  }
  function show() {
    if (!dialog.open) dialog.showModal();
  }

  // choose opens the file chooser of the upload step. Without one (a step is
  // still being put back), the dialog opens on whatever step it holds.
  function choose() {
    var input = dialog.querySelector('#avatar-panel input[type=file]');
    if (input) input.click();
    else show();
  }
  function loading() {
    return dialog.querySelector('#avatar-panel [data-avatar-loading]');
  }
  function stopLoading() {
    var panel = document.getElementById('avatar-panel');
    var line = loading();
    if (panel) panel.classList.remove('is-loading');
    if (line) line.hidden = true;
  }

  open.addEventListener('click', function (e) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button > 0) return;
    e.preventDefault();
    choose();
  });
  // The link has role=button, which also answers to Space.
  open.addEventListener('keydown', function (e) {
    if (e.key === ' ') {
      e.preventDefault();
      choose();
    }
  });
  // A file chosen goes up at once; the crop step replaces the loading line.
  dialog.addEventListener('change', function (e) {
    var input = e.target;
    if (!input.matches || !input.matches('input[type=file]') || !input.files || !input.files.length) return;
    var form = input.form;
    var panel = document.getElementById('avatar-panel');
    var line = loading();
    clearError();
    if (panel) panel.classList.add('is-loading');
    if (line) line.hidden = false;
    show();
    if (typeof form.requestSubmit === 'function') form.requestSubmit();
    else htmx.trigger(form, 'submit');
  });
  dialog.querySelector('[data-avatar-close]').addEventListener('click', function () {
    dialog.close();
  });
  dialog.querySelector('[data-avatar-retry]').addEventListener('click', startOver);
  dialog.addEventListener('click', function (e) {
    if (!e.target.closest) return;
    if (e.target.closest('[data-avatar-choose]')) {
      choose();
      return;
    }
    // The crop step's Cancel is a link for the page without scripts.
    if (e.target.closest('[data-crop-cancel]')) {
      e.preventDefault();
      dialog.close();
    }
  });
  // The status line on the page links to the crop step, which is in the dialog.
  document.addEventListener('click', function (e) {
    if (e.target.closest && e.target.closest('#avatar-status [data-avatar-resume]')) show();
  });

  dialog.addEventListener('htmx:responseError', function (e) {
    var status = e.detail.xhr ? e.detail.xhr.status : 0;
    if (status === 401 || status === 403) {
      showError('You are signed out. Reload this page to sign in again.');
    } else {
      showError('Something went wrong on our side, and your picture was not saved.');
    }
  });
  dialog.addEventListener('htmx:sendError', function () {
    showError('The connection failed, and your picture was not saved. Check it and try again.');
  });
  dialog.addEventListener('htmx:timeout', function () {
    showError('The request took too long, and your picture was not saved.');
  });
  document.body.addEventListener('htmx:afterSwap', function (e) {
    if (e.detail.target && e.detail.target.id === 'avatar-panel') clearError();
  });
  // Once the crop is saved, the page's status line takes over: it says the
  // picture is processing, and the page reloads when it is ready.
  document.body.addEventListener('htmx:afterSettle', function () {
    var panel = document.getElementById('avatar-panel');
    if (dialog.open && panel && panel.getAttribute('data-step') === 'saved') dialog.close();
  });

  dialog.addEventListener('close', function () {
    var panel = document.getElementById('avatar-panel');
    // Anything past the first step, and a refusal shown on it, is dropped. An
    // upload that was not cropped waits for the person, and the status says so.
    var stale = panel && (panel.getAttribute('data-step') !== 'upload' || panel.querySelector('[data-testid=avatar-message]'));
    var saved = panel && panel.getAttribute('data-step') === 'saved';
    // Closed while the picture was going up: the crop must not arrive later in
    // a dialog the person dismissed.
    var form = dialog.querySelector('form');
    if (panel && panel.classList.contains('is-loading') && form && typeof htmx !== 'undefined') {
      htmx.trigger(form, 'htmx:abort');
    }
    if (stale || !errorBox.hidden) startOver();
    else stopLoading();
    if (form) form.reset();
    // The saved step already put a polling status on the page.
    if (!saved) refreshStatus();
    open.focus();
  });
})();
