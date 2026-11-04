// Avatar dialog of the profile page (internal/web/profile/avatar.templ). The
// avatar button opens the native modal <dialog>, which closes on Escape by
// itself; the close button closes it too. Closing resets the form, so nothing
// chosen and not uploaded is kept.
(function () {
  'use strict';
  var dialog = document.getElementById('avatar-dialog');
  var open = document.querySelector('[data-avatar-open]');
  if (!dialog || !open || typeof dialog.showModal !== 'function') return;

  open.addEventListener('click', function () {
    dialog.showModal();
  });
  var close = dialog.querySelector('[data-avatar-close]');
  if (close) {
    close.addEventListener('click', function () {
      dialog.close();
    });
  }
  dialog.addEventListener('close', function () {
    var form = dialog.querySelector('form');
    if (form) form.reset();
    open.focus();
  });
})();
