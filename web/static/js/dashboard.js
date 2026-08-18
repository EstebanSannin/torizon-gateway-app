// Dashboard live health: consumes the /sse/metrics numeric tick stream and
// renders the radial gauges, the CPU frequency bar and the network chart.
// Vanilla JS, no framework — gauges/bar animate via CSS transitions, the
// network chart slides continuously via requestAnimationFrame.
(function () {
  var cpuArc = document.getElementById('cpuArc');
  if (!cpuArc) return; // not the dashboard

  var $ = function (id) { return document.getElementById(id); };
  var ARC = 75; // normalized arc length (270° of a pathLength=100 circle)

  function zone(v, warn, danger) {
    return v < warn ? 'var(--status-ok)' : v < danger ? 'var(--status-warn)' : 'var(--status-danger)';
  }
  function setGauge(arc, numEl, pct, color, text) {
    var p = Math.max(0, Math.min(1, pct / 100));
    arc.style.strokeDashoffset = (ARC * (1 - p)).toFixed(2);
    arc.style.stroke = color;
    numEl.textContent = text;
  }
  function fmtRate(bps) { // bytes/s -> [value, unit]
    if (bps >= 1048576) return [(bps / 1048576).toFixed(1), 'MB/s'];
    if (bps >= 1024) return [(bps / 1024).toFixed(0), 'KB/s'];
    return [Math.max(0, Math.round(bps)).toString(), 'B/s'];
  }
  function fmtBytes(b) {
    if (b >= 1073741824) return (b / 1073741824).toFixed(1) + ' GB';
    if (b >= 1048576) return (b / 1048576).toFixed(0) + ' MB';
    if (b >= 1024) return (b / 1024).toFixed(0) + ' KB';
    return b + ' B';
  }

  // ---- network chart state ----
  var N = 60, rx = new Array(N).fill(0), tx = new Array(N).fill(0);
  var tPush = performance.now(), smax = 1;
  var W = 600, H = 112, PAD = 6, step = W / (N - 2);
  var rxLine = $('rxLine'), txLine = $('txLine'), rxArea = $('rxArea'),
      txArea = $('txArea'), rxDot = $('rxDot'), txDot = $('txDot');

  function draw() {
    var f = Math.min(1, (performance.now() - tPush) / 1000); // 0..1 fractional slide
    var peak = 1;
    for (var i = 0; i < N; i++) { if (rx[i] > peak) peak = rx[i]; if (tx[i] > peak) peak = tx[i]; }
    smax += (peak - smax) * 0.06; // smoothly adapt vertical scale
    var sc = function (v) { return H - PAD - (v / smax) * (H - 2 * PAD); };
    function build(arr, line, area, dot, dotCls) {
      var d = '';
      for (var i = 0; i < N; i++) {
        var x = (i - 1) * step - f * step;
        d += (i ? 'L' : 'M') + x.toFixed(1) + ' ' + sc(arr[i]).toFixed(1) + ' ';
      }
      var lastx = (N - 2) * step - f * step;
      line.setAttribute('d', d.trim());
      area.setAttribute('d', 'M' + (-step) + ' ' + H + ' ' + d.trim().replace(/^M/, 'L') + 'L' + lastx.toFixed(1) + ' ' + H + ' Z');
      dot.setAttribute('cx', lastx.toFixed(1));
      dot.setAttribute('cy', sc(arr[N - 1]).toFixed(1));
    }
    build(rx, rxLine, rxArea, rxDot);
    build(tx, txLine, txArea, txDot);
    requestAnimationFrame(draw);
  }

  function onTick(m) {
    setGauge(cpuArc, $('cpuNum'), m.cpu, zone(m.cpu, 70, 90), Math.round(m.cpu));
    setGauge($('memArc'), $('memNum'), m.mem, zone(m.mem, 75, 90), Math.round(m.mem));
    setGauge($('tempArc'), $('tempNum'), m.temp / m.tempScale * 100,
      zone(m.temp, m.tempWarn, m.tempAlarm), m.temp.toFixed(1));
    if ($('tempCap')) $('tempCap').textContent =
      'scale 0–' + Math.round(m.tempScale) + ' °C · alarm ≥ ' + Math.round(m.tempAlarm) + ' °C';

    if ($('memUsed')) $('memUsed').textContent = fmtBytes(m.memUsed);
    if ($('load')) $('load').textContent = m.load.toFixed(2);
    if ($('uptime')) $('uptime').textContent = m.uptime;

    // frequency bar: current clock as a fraction of max (always visible, floored
    // at the idle min shown by the marker). data-max is in MHz.
    var fill = $('freqFill');
    if (fill && m.freqCur > 0) {
      var hi = +fill.dataset.max;
      if (hi > 0) fill.style.width = (Math.max(0, Math.min(1, m.freqCur / hi)) * 100).toFixed(1) + '%';
      $('freqNow').textContent = (m.freqCur / 1000).toFixed(2);
    }

    // network numbers + push a sample
    var r = fmtRate(m.rx), t = fmtRate(m.tx);
    if ($('rxNum')) { $('rxNum').textContent = r[0]; $('rxU').textContent = r[1]; }
    if ($('txNum')) { $('txNum').textContent = t[0]; $('txU').textContent = t[1]; }
    if ($('netIface') && m.iface) $('netIface').textContent = m.iface;
    rx.push(m.rx); rx.shift(); tx.push(m.tx); tx.shift();
    tPush = performance.now();
  }

  var es = new EventSource('/sse/metrics');
  es.addEventListener('tick', function (e) {
    try { onTick(JSON.parse(e.data)); } catch (err) { /* ignore malformed frame */ }
  });
  requestAnimationFrame(draw);
})();
