// Crop step of the avatar upload (internal/web/profile/avatar.templ). Cropper.js
// (vendor/cropper.min.js) draws the square; this posts it as fractions of the
// picture, which the server applies to the original and checks again
// (ADR-00033). The step appears in the profile page's dialog after htmx swaps it
// in, or on the crop page when it loads.
(function () {
  'use strict';
  if (typeof Cropper === 'undefined') return;

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
    var img = document.getElementById('crop-image');
    if (img) setup(img);
  }
  scan();
  document.body.addEventListener('htmx:afterSettle', scan);
})();
