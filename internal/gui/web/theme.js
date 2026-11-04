'use strict';
// The window's colours, set before anything is drawn: light, dark, or as
// Windows has them. The choice is baton's gui_theme setting; the server
// writes it on <html data-theme-pref>, and app.js passes on a change.
(() => {
  const root = document.documentElement;
  const system = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  let pref = root.dataset.themePref || 'auto';
  const apply = () => {
    const dark = pref === 'dark' || (pref !== 'light' && !!(system && system.matches));
    root.dataset.theme = dark ? 'dark' : 'light';
  };
  apply();
  if (system) system.addEventListener('change', apply);
  window.batonTheme = {
    get: () => pref,
    set(p) {
      pref = p === 'light' || p === 'dark' ? p : 'auto';
      apply();
    },
  };
})();
