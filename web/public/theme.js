// Apply a stored theme before the first paint (see src/lib/theme.ts). A file,
// not an inline script, so the CSP can stay at script-src 'self'.
// It runs first, so it also moves keys from the app's former name (solar0.*).
try {
  for (var i = localStorage.length - 1; i >= 0; i--) {
    var k = localStorage.key(i);
    if (k && k.indexOf('solar0.') === 0) {
      var n = 'noonside.' + k.slice(7);
      if (localStorage.getItem(n) === null) localStorage.setItem(n, localStorage.getItem(k));
      localStorage.removeItem(k);
    }
  }
  var t = localStorage.getItem('noonside.theme'); if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t);
} catch (e) {}
