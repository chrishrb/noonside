// Confirm and unsubscribe pages: the token comes in the URL fragment (#t=…),
// which is never sent to a server, so link scanners in mail systems can't
// confirm or unsubscribe by opening the link. Copy it into the form; the
// person still has to press the button. A file, not an inline script, so
// the CSP can stay at script-src 'self'.
(function () {
  var m = /(?:^#|&)t=([^&]+)/.exec(location.hash);
  var input = document.querySelector('input[name="token"]');
  var button = document.querySelector('form[data-token] button');
  if (m && input) {
    input.value = decodeURIComponent(m[1]);
    if (button) button.disabled = false;
    history.replaceState(null, '', location.pathname);
  } else {
    var hint = document.getElementById('no-token');
    if (hint) hint.hidden = false;
  }
})();
