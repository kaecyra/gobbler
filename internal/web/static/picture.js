// Turns a recipe picture that fails to load into its placeholder band
// (ADR-00039, Showing). The picture route answers 404 when the object is
// missing from the media store (and logs it), and an <img> cannot fall back by
// itself. The band sits under the picture, so removing the image and its
// data-pic mark is all it takes. Pages load this with layout.PictureFallback.
(function () {
  'use strict';

  function drop(img) {
    var box = img.closest('[data-pic]');
    if (box) box.removeAttribute('data-pic');
    img.remove();
  }

  // Image errors do not bubble, so listen on the capture phase, once per page.
  if (!window.gobblerPictureFallback) {
    window.gobblerPictureFallback = true;
    document.addEventListener(
      'error',
      function (e) {
        if (e.target && e.target.tagName === 'IMG' && e.target.hasAttribute('data-pic-img')) drop(e.target);
      },
      true,
    );
  }

  // A picture that had already failed before this ran raises no event now.
  document.querySelectorAll('img[data-pic-img]').forEach(function (img) {
    if (img.complete && img.naturalWidth === 0) drop(img);
  });
})();
