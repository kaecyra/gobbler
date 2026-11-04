// Avatar dialog of the profile page (internal/web/profile/avatar.templ). The
// avatar is a link to the upload page; this turns it into a button that opens
// the native modal <dialog>, which closes on Escape by itself. Without scripts,
// or without <dialog>, the link works as a link. Any failure of a request made
// from the dialog is shown in it: htmx swaps nothing on an error status or a
// dropped connection, so without this the person would see no change at all.
(function () {
  'use strict';
  var dialog = document.getElementById('avatar-dialog');
  var open = document.querySelector('[data-avatar-open]');
  if (!dialog || !open || typeof dialog.showModal !== 'function') return;

  var errorBox = dialog.querySelector('[data-avatar-error]');
  var errorText = dialog.querySelector('[data-avatar-error-text]');
  var uploadStep = open.getAttribute('href');

  function showError(text) {
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

  open.addEventListener('click', function (e) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button > 0) return;
    e.preventDefault();
    dialog.showModal();
  });
  // The link has role=button, which also answers to Space.
  open.addEventListener('keydown', function (e) {
    if (e.key === ' ') {
      e.preventDefault();
      dialog.showModal();
    }
  });
  dialog.querySelector('[data-avatar-close]').addEventListener('click', function () {
    dialog.close();
  });
  dialog.querySelector('[data-avatar-retry]').addEventListener('click', startOver);
  dialog.addEventListener('click', function (e) {
    // The crop step's Cancel is a link for the page without scripts.
    if (e.target.closest && e.target.closest('[data-crop-cancel]')) {
      e.preventDefault();
      dialog.close();
    }
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

  dialog.addEventListener('close', function () {
    var panel = document.getElementById('avatar-panel');
    // Anything past the first step, and a refusal shown on it, is dropped. An
    // upload that was not cropped waits for the person, and the status says so.
    var stale = panel && (panel.getAttribute('data-step') !== 'upload' || panel.querySelector('[data-testid=avatar-message]'));
    if (stale || !errorBox.hidden) startOver();
    var form = dialog.querySelector('form');
    if (form) form.reset();
    open.focus();
  });
})();
