// Light/dark toggle and the nav menu (docs/ui/design.md, Light always).
//
// Loaded in the head without defer so a remembered dark choice is on the root
// element before the first paint. Light is the default and nothing here reads
// prefers-color-scheme. Storage may be blocked (private windows, site data
// cleared), so every access is guarded and the page works without it.
(function () {
  var KEY = 'gob-mode';
  var root = document.documentElement;

  function stored() {
    try {
      return window.localStorage.getItem(KEY) === 'dark';
    } catch (e) {
      return false;
    }
  }

  function remember(dark) {
    try {
      window.localStorage.setItem(KEY, dark ? 'dark' : 'light');
    } catch (e) {
      // Not remembered; the choice still holds until the page is left.
    }
  }

  function apply(dark) {
    if (dark) {
      root.setAttribute('data-mode', 'dark');
    } else {
      root.removeAttribute('data-mode');
    }
  }

  function sync(button, dark) {
    button.setAttribute('aria-pressed', dark ? 'true' : 'false');
    button.setAttribute('aria-label', dark ? 'Switch to light' : 'Switch to dark');
    button.setAttribute('title', dark ? 'Light mode' : 'Dark mode');
  }

  apply(stored());

  document.addEventListener('DOMContentLoaded', function () {
    var button = document.querySelector('[data-theme-toggle]');
    if (button) {
      var dark = root.getAttribute('data-mode') === 'dark';
      sync(button, dark);
      button.addEventListener('click', function () {
        dark = !dark;
        apply(dark);
        sync(button, dark);
        remember(dark);
      });
    }

    var menu = document.querySelector('[data-menu]');
    if (menu) {
      document.addEventListener('click', function (e) {
        if (menu.open && !menu.contains(e.target)) {
          menu.open = false;
        }
      });
      document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' && menu.open) {
          menu.open = false;
          menu.querySelector('summary').focus();
        }
      });
    }
  });
})();
