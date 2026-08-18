// Wi-Fi connect dialog. The network rows are server-rendered (and htmx-swapped),
// so they use native onclick to open the modal; the modal's form posts via htmx.
function wifiOpen(el) {
  var d = el.dataset;
  var secured = d.secured === '1';
  document.getElementById('wifi-m-ssid').textContent = d.ssid;
  document.getElementById('wifi-m-ssid-input').value = d.ssid;
  var sub = d.sec + ' · ' + d.band + (Number(d.ch) > 0 ? ' · channel ' + d.ch : '') + ' · signal ' + d.sig + '%';
  document.getElementById('wifi-m-sub').textContent = sub;
  document.getElementById('wifi-m-secured').style.display = secured ? 'block' : 'none';
  document.getElementById('wifi-m-open').style.display = secured ? 'none' : 'block';
  var pw = document.getElementById('wifi-m-pw');
  pw.value = ''; pw.type = 'password';
  document.getElementById('wifi-pw-toggle').textContent = 'Show';
  document.getElementById('wifi-overlay').classList.add('open');
  if (secured) setTimeout(function () { pw.focus(); }, 60);
}

function wifiClose() {
  var o = document.getElementById('wifi-overlay');
  if (o) o.classList.remove('open');
}

function wifiTogglePw() {
  var i = document.getElementById('wifi-m-pw'), t = document.getElementById('wifi-pw-toggle');
  if (i.type === 'password') { i.type = 'text'; t.textContent = 'Hide'; }
  else { i.type = 'password'; t.textContent = 'Show'; }
}

// Close the dialog on Escape.
document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape') wifiClose();
});
