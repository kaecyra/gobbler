// Click to play for a recipe's YouTube video (ADR-00021). Nothing from a
// YouTube origin loads until Play is pressed: the page holds the player's
// address in data-embed, and this script, embedded in the binary, swaps the
// play control for the privacy-enhanced iframe when it is clicked. Without
// scripts the control is a plain link to the watch page.
(function () {
  'use strict';
  var embedPrefix = 'https://www.youtube-nocookie.com/embed/';

  document.addEventListener('click', function (event) {
    var target = event.target;
    if (!(target instanceof Element)) return;
    var play = target.closest('[data-video-play]');
    if (!play) return;
    var src = play.getAttribute('data-embed') || '';
    // Only the privacy-enhanced embed host is ever loaded.
    if (src.indexOf(embedPrefix) !== 0) return;
    event.preventDefault();

    var frame = document.createElement('iframe');
    frame.src = src;
    frame.title = play.getAttribute('data-title') || 'Video';
    frame.allow = 'autoplay; encrypted-media; picture-in-picture; fullscreen';
    frame.allowFullscreen = true;
    frame.referrerPolicy = 'strict-origin-when-cross-origin';
    frame.setAttribute('data-testid', 'video-frame');
    play.replaceWith(frame);
    frame.focus();
  });
})();
