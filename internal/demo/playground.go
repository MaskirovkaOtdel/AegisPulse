package demo

import (
	"net/http"
)

const playgroundHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AegisPulse Gateway — Interactive Playground</title>
  <style>
    :root {
      --bg: #090d16;
      --card-bg: #111827;
      --border: #1f2937;
      --primary: #3b82f6;
      --success: #10b981;
      --warning: #f59e0b;
      --danger: #ef4444;
      --text: #f3f4f6;
      --muted: #9ca3af;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: var(--bg); color: var(--text); padding: 24px; }
    header { max-width: 1100px; margin: 0 auto 24px auto; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid var(--border); padding-bottom: 16px; }
    .logo { font-size: 20px; font-weight: 800; letter-spacing: 1px; color: #60a5fa; display: flex; align-items: center; gap: 8px; }
    .badge { background: rgba(59, 130, 246, 0.2); color: #93c5fd; padding: 4px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; text-transform: uppercase; }
    .nav-links a { color: var(--muted); text-decoration: none; font-size: 14px; margin-left: 16px; }
    .nav-links a:hover { color: var(--text); }
    .container { max-width: 1100px; margin: 0 auto; display: grid; grid-template-columns: 1fr 1fr; gap: 24px; }
    @media (max-width: 768px) { .container { grid-template-columns: 1fr; } }
    .card { background: var(--card-bg); border: 1px solid var(--border); border-radius: 10px; padding: 20px; }
    h2 { font-size: 17px; margin-bottom: 16px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid var(--border); padding-bottom: 10px; }
    .slider-group { margin-bottom: 16px; }
    .slider-header { display: flex; justify-content: space-between; font-size: 13px; color: var(--muted); margin-bottom: 6px; }
    .slider-header span.val { color: var(--text); font-weight: 600; font-family: monospace; }
    input[type=range] { width: 100%; accent-color: var(--primary); cursor: pointer; }
    .btn-row { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 16px; }
    button { background: #1e293b; color: var(--text); border: 1px solid var(--border); padding: 8px 14px; border-radius: 6px; font-size: 13px; font-weight: 600; cursor: pointer; transition: all 0.15s; }
    button:hover { background: #334155; }
    button.primary { background: var(--primary); border-color: var(--primary); }
    button.primary:hover { background: #2563eb; }
    button.danger { background: rgba(239, 68, 68, 0.2); color: #fca5a5; border-color: var(--danger); }
    button.danger:hover { background: rgba(239, 68, 68, 0.35); }
    button.success { background: rgba(16, 185, 129, 0.2); color: #6ee7b7; border-color: var(--success); }
    button.success:hover { background: rgba(16, 185, 129, 0.35); }
    .tank-container { width: 100%; height: 160px; background: #0c1322; border: 2px solid var(--border); border-radius: 8px; position: relative; overflow: hidden; margin-top: 14px; }
    .tank-fill { position: absolute; bottom: 0; left: 0; right: 0; background: linear-gradient(180deg, #3b82f6 0%, #1d4ed8 100%); transition: height 0.2s ease, background 0.3s; }
    .tank-marker { position: absolute; left: 0; right: 0; border-top: 1px dashed rgba(255,255,255,0.4); font-size: 10px; color: rgba(255,255,255,0.7); padding-left: 8px; pointer-events: none; }
    .tank-info { position: absolute; top: 10px; right: 12px; font-family: monospace; font-size: 13px; font-weight: bold; background: rgba(0,0,0,0.6); padding: 4px 8px; border-radius: 4px; }
    .state-badge { padding: 4px 10px; border-radius: 4px; font-size: 12px; font-weight: 700; text-transform: uppercase; font-family: monospace; }
    .state-closed { background: rgba(16, 185, 129, 0.2); color: #34d399; border: 1px solid var(--success); }
    .state-open { background: rgba(239, 68, 68, 0.2); color: #f87171; border: 1px solid var(--danger); }
    .state-half { background: rgba(245, 158, 11, 0.2); color: #fbbf24; border: 1px solid var(--warning); }
    .log-box { background: #0b0f19; border: 1px solid var(--border); border-radius: 6px; padding: 10px; height: 140px; overflow-y: auto; font-family: monospace; font-size: 11px; color: #a5b4fc; margin-top: 14px; }
    .log-entry { margin-bottom: 4px; }
    .log-normal { color: #34d399; }
    .log-burst { color: #fbbf24; }
    .log-blocked { color: #f87171; }
    .cb-visualizer { display: flex; align-items: center; justify-content: space-around; padding: 24px 0; }
    .cb-node { width: 80px; height: 80px; border-radius: 50%; display: flex; flex-direction: column; align-items: center; justify-content: center; font-size: 11px; font-weight: bold; text-align: center; border: 2px solid var(--border); background: #1a2234; transition: all 0.3s; }
    .cb-node.active { transform: scale(1.15); box-shadow: 0 0 15px currentColor; }
  </style>
</head>
<body>
  <header>
    <div class="logo">
      <span>🛡️ AegisPulse</span>
      <span class="badge">Demo Playground</span>
    </div>
    <div class="nav-links">
      <a href="/docs">API Docs (Swagger) &rarr;</a>
      <a href="/healthz">Healthz</a>
      <a href="/metrics">Metrics</a>
    </div>
  </header>

  <div class="container">
    <!-- Panel 1: Token Bucket Rate Limiter Visualizer -->
    <div class="card">
      <h2>
        <span>Dual-Threshold Token Bucket</span>
        <span id="tbStatus" class="state-badge state-closed">NORMAL</span>
      </h2>

      <div class="slider-group">
        <div class="slider-header">
          <span>Soft Limit (Normal Capacity)</span>
          <span class="val" id="softLimitVal">20</span>
        </div>
        <input type="range" id="softLimitInput" min="5" max="50" value="20">
      </div>

      <div class="slider-group">
        <div class="slider-header">
          <span>Hard Limit (Burst Max)</span>
          <span class="val" id="hardLimitVal">30</span>
        </div>
        <input type="range" id="hardLimitInput" min="10" max="60" value="30">
      </div>

      <div class="slider-group">
        <div class="slider-header">
          <span>Refill Rate (Tokens/sec)</span>
          <span class="val" id="refillRateVal">5.0</span>
        </div>
        <input type="range" id="refillRateInput" min="1" max="20" step="0.5" value="5">
      </div>

      <div class="tank-container">
        <div id="tankFill" class="tank-fill" style="height: 100%;"></div>
        <div id="burstMarker" class="tank-marker" style="top: 33%;">Burst Threshold</div>
        <div class="tank-info" id="tankLevelText">30 / 30 tokens</div>
      </div>

      <div class="btn-row">
        <button class="primary" id="btnDrain1">Drain 1 Token</button>
        <button id="btnDrain5">Drain 5 Tokens</button>
        <button class="danger" id="btnBurstDrain">Flood (15 req)</button>
        <button id="btnResetTB">Reset Tank</button>
      </div>

      <div class="log-box" id="tbLogs">
        <div class="log-entry log-normal">[System] Token bucket visualizer initialized.</div>
      </div>
    </div>

    <!-- Panel 2: Circuit Breaker State Machine Visualizer -->
    <div class="card">
      <h2>
        <span>Circuit Breaker State Machine</span>
        <span id="cbStateBadge" class="state-badge state-closed">CLOSED</span>
      </h2>

      <div class="cb-visualizer">
        <div id="nodeClosed" class="cb-node active" style="color: #10b981; border-color: #10b981;">
          <span>CLOSED</span>
          <span style="font-size: 9px; opacity: 0.8;">Normal</span>
        </div>
        <div style="color: var(--muted); font-size: 18px;">&harr;</div>
        <div id="nodeHalfOpen" class="cb-node" style="color: #f59e0b; border-color: #374151;">
          <span>HALF-OPEN</span>
          <span style="font-size: 9px; opacity: 0.8;">Testing</span>
        </div>
        <div style="color: var(--muted); font-size: 18px;">&harr;</div>
        <div id="nodeOpen" class="cb-node" style="color: #ef4444; border-color: #374151;">
          <span>OPEN</span>
          <span style="font-size: 9px; opacity: 0.8;">Tripped</span>
        </div>
      </div>

      <div class="slider-group">
        <div class="slider-header">
          <span>Max Consecutive Failures</span>
          <span class="val" id="maxFailVal">3</span>
        </div>
        <input type="range" id="maxFailInput" min="1" max="10" value="3">
      </div>

      <div class="slider-group">
        <div class="slider-header">
          <span>Cooldown Duration (Seconds)</span>
          <span class="val" id="cooldownVal">5s</span>
        </div>
        <input type="range" id="cooldownInput" min="2" max="15" value="5">
      </div>

      <div style="display: flex; justify-content: space-between; font-size: 13px; font-family: monospace; background: #0c1322; padding: 10px; border-radius: 6px; margin-top: 10px;">
        <span>Consecutive Failures: <strong id="failCount" style="color: #f87171;">0</strong> / <span id="maxFailLabel">3</span></span>
        <span>Cooldown Left: <strong id="cooldownTimer" style="color: #fbbf24;">0s</strong></span>
      </div>

      <div class="btn-row">
        <button class="success" id="btnSendSuccess">Send 200 OK</button>
        <button class="danger" id="btnSendFailure">Send 500 Failure</button>
        <button class="danger" id="btnTripTrip">Trip Immediately</button>
        <button id="btnResetCB">Reset Breaker</button>
      </div>

      <div class="log-box" id="cbLogs">
        <div class="log-entry log-normal">[CircuitBreaker] Initialized in CLOSED state. Traffic passing normally.</div>
      </div>
    </div>
  </div>

  <script>
    // --- Token Bucket State ---
    let softLimit = 20;
    let hardLimit = 30;
    let refillRate = 5;
    let currentTokens = 30;

    const tankFill = document.getElementById('tankFill');
    const burstMarker = document.getElementById('burstMarker');
    const tankLevelText = document.getElementById('tankLevelText');
    const tbStatus = document.getElementById('tbStatus');
    const tbLogs = document.getElementById('tbLogs');

    function updateTBUI() {
      const pct = Math.max(0, Math.min(100, (currentTokens / hardLimit) * 100));
      tankFill.style.height = pct + '%';
      tankLevelText.innerText = Math.round(currentTokens) + ' / ' + hardLimit + ' tokens';

      const burstCapacity = hardLimit - softLimit;
      const markerTopPct = (burstCapacity / hardLimit) * 100;
      burstMarker.style.top = markerTopPct + '%';

      if (currentTokens > burstCapacity) {
        tbStatus.innerText = 'NORMAL';
        tbStatus.className = 'state-badge state-closed';
        tankFill.style.background = 'linear-gradient(180deg, #10b981 0%, #047857 100%)';
      } else if (currentTokens >= 1) {
        tbStatus.innerText = 'BURST';
        tbStatus.className = 'state-badge state-half';
        tankFill.style.background = 'linear-gradient(180deg, #f59e0b 0%, #b45309 100%)';
      } else {
        tbStatus.innerText = 'BLOCKED (429)';
        tbStatus.className = 'state-badge state-open';
        tankFill.style.background = 'linear-gradient(180deg, #ef4444 0%, #b91c1c 100%)';
      }
    }

    function addTBLog(msg, type) {
      const d = document.createElement('div');
      d.className = 'log-entry ' + (type ? 'log-' + type : '');
      const timeStr = new Date().toLocaleTimeString();
      d.innerText = '[' + timeStr + '] ' + msg;
      tbLogs.appendChild(d);
      tbLogs.scrollTop = tbLogs.scrollHeight;
    }

    function drainTokens(n) {
      const burstCapacity = hardLimit - softLimit;
      if (currentTokens >= n) {
        currentTokens -= n;
        if (currentTokens > burstCapacity) {
          addTBLog('Consumed ' + n + ' token(s) — Status: NORMAL. Rem: ' + Math.round(currentTokens - burstCapacity), 'normal');
        } else {
          addTBLog('Consumed ' + n + ' token(s) — Status: BURST. Overages active! Rem: ' + Math.round(currentTokens), 'burst');
        }
      } else {
        addTBLog('Request for ' + n + ' token(s) REJECTED (429 Too Many Requests). Rate limited!', 'blocked');
      }
      updateTBUI();
    }

    // Sliders
    document.getElementById('softLimitInput').oninput = e => {
      softLimit = parseInt(e.target.value);
      if (softLimit >= hardLimit) {
        hardLimit = softLimit + 5;
        document.getElementById('hardLimitInput').value = hardLimit;
        document.getElementById('hardLimitVal').innerText = hardLimit;
      }
      document.getElementById('softLimitVal').innerText = softLimit;
      updateTBUI();
    };
    document.getElementById('hardLimitInput').oninput = e => {
      hardLimit = parseInt(e.target.value);
      if (hardLimit <= softLimit) {
        softLimit = Math.max(1, hardLimit - 5);
        document.getElementById('softLimitInput').value = softLimit;
        document.getElementById('softLimitVal').innerText = softLimit;
      }
      document.getElementById('hardLimitVal').innerText = hardLimit;
      currentTokens = Math.min(currentTokens, hardLimit);
      updateTBUI();
    };
    document.getElementById('refillRateInput').oninput = e => {
      refillRate = parseFloat(e.target.value);
      document.getElementById('refillRateVal').innerText = refillRate.toFixed(1);
    };

    document.getElementById('btnDrain1').onclick = () => drainTokens(1);
    document.getElementById('btnDrain5').onclick = () => drainTokens(5);
    document.getElementById('btnBurstDrain').onclick = () => drainTokens(15);
    document.getElementById('btnResetTB').onclick = () => {
      currentTokens = hardLimit;
      addTBLog('Token bucket reset to capacity (' + hardLimit + ').', 'normal');
      updateTBUI();
    };

    // Refill tick (10 times per second)
    setInterval(() => {
      if (currentTokens < hardLimit) {
        currentTokens = Math.min(hardLimit, currentTokens + (refillRate / 10));
        updateTBUI();
      }
    }, 100);

    // --- Circuit Breaker State ---
    let cbState = 'CLOSED'; // 'CLOSED', 'OPEN', 'HALF-OPEN'
    let consecutiveFailures = 0;
    let maxFailures = 3;
    let cooldownDuration = 5;
    let cooldownRemaining = 0;
    let cooldownInterval = null;

    const cbStateBadge = document.getElementById('cbStateBadge');
    const nodeClosed = document.getElementById('nodeClosed');
    const nodeHalfOpen = document.getElementById('nodeHalfOpen');
    const nodeOpen = document.getElementById('nodeOpen');
    const failCount = document.getElementById('failCount');
    const cooldownTimer = document.getElementById('cooldownTimer');
    const cbLogs = document.getElementById('cbLogs');

    function updateCBUI() {
      cbStateBadge.innerText = cbState;
      failCount.innerText = consecutiveFailures;
      document.getElementById('maxFailLabel').innerText = maxFailures;
      cooldownTimer.innerText = cooldownRemaining > 0 ? cooldownRemaining + 's' : '0s';

      nodeClosed.className = 'cb-node' + (cbState === 'CLOSED' ? ' active' : '');
      nodeHalfOpen.className = 'cb-node' + (cbState === 'HALF-OPEN' ? ' active' : '');
      nodeOpen.className = 'cb-node' + (cbState === 'OPEN' ? ' active' : '');

      if (cbState === 'CLOSED') {
        cbStateBadge.className = 'state-badge state-closed';
        nodeClosed.style.borderColor = '#10b981';
        nodeHalfOpen.style.borderColor = '#374151';
        nodeOpen.style.borderColor = '#374151';
      } else if (cbState === 'HALF-OPEN') {
        cbStateBadge.className = 'state-badge state-half';
        nodeClosed.style.borderColor = '#374151';
        nodeHalfOpen.style.borderColor = '#f59e0b';
        nodeOpen.style.borderColor = '#374151';
      } else {
        cbStateBadge.className = 'state-badge state-open';
        nodeClosed.style.borderColor = '#374151';
        nodeHalfOpen.style.borderColor = '#374151';
        nodeOpen.style.borderColor = '#ef4444';
      }
    }

    function addCBLog(msg, type) {
      const d = document.createElement('div');
      d.className = 'log-entry ' + (type ? 'log-' + type : '');
      const timeStr = new Date().toLocaleTimeString();
      d.innerText = '[' + timeStr + '] ' + msg;
      cbLogs.appendChild(d);
      cbLogs.scrollTop = cbLogs.scrollHeight;
    }

    function tripCircuitBreaker() {
      cbState = 'OPEN';
      cooldownRemaining = cooldownDuration;
      addCBLog('CRITICAL: Consecutive failures threshold reached. Circuit Breaker TRIPPED to OPEN state! Serving L1 cache fallbacks.', 'blocked');
      updateCBUI();

      if (cooldownInterval) clearInterval(cooldownInterval);
      cooldownInterval = setInterval(() => {
        cooldownRemaining--;
        if (cooldownRemaining <= 0) {
          clearInterval(cooldownInterval);
          cbState = 'HALF-OPEN';
          consecutiveFailures = 0;
          addCBLog('Cooldown expired. Circuit Breaker transitioned to HALF-OPEN (Canary probe mode).', 'burst');
        }
        updateCBUI();
      }, 1000);
    }

    document.getElementById('btnSendSuccess').onclick = () => {
      if (cbState === 'OPEN') {
        addCBLog('Upstream call BLOCKED: Circuit is OPEN. Request redirected to cache.', 'blocked');
        return;
      }
      consecutiveFailures = 0;
      if (cbState === 'HALF-OPEN') {
        cbState = 'CLOSED';
        addCBLog('Canary probe succeeded (200 OK)! Circuit Breaker healed to CLOSED.', 'normal');
      } else {
        addCBLog('Request succeeded (200 OK). Breaker healthy.', 'normal');
      }
      updateCBUI();
    };

    document.getElementById('btnSendFailure').onclick = () => {
      if (cbState === 'OPEN') {
        addCBLog('Upstream call BLOCKED: Circuit is already OPEN.', 'blocked');
        return;
      }
      consecutiveFailures++;
      addCBLog('Upstream 500 error recorded! Failure count: ' + consecutiveFailures + '/' + maxFailures, 'burst');
      if (cbState === 'HALF-OPEN' || consecutiveFailures >= maxFailures) {
        tripCircuitBreaker();
      } else {
        updateCBUI();
      }
    };

    document.getElementById('btnTripTrip').onclick = () => {
      consecutiveFailures = maxFailures;
      tripCircuitBreaker();
    };

    document.getElementById('btnResetCB').onclick = () => {
      if (cooldownInterval) clearInterval(cooldownInterval);
      cbState = 'CLOSED';
      consecutiveFailures = 0;
      cooldownRemaining = 0;
      addCBLog('Circuit Breaker manually reset to CLOSED state.', 'normal');
      updateCBUI();
    };

    document.getElementById('maxFailInput').oninput = e => {
      maxFailures = parseInt(e.target.value);
      document.getElementById('maxFailVal').innerText = maxFailures;
      updateCBUI();
    };
    document.getElementById('cooldownInput').oninput = e => {
      cooldownDuration = parseInt(e.target.value);
      document.getElementById('cooldownVal').innerText = cooldownDuration + 's';
      updateCBUI();
    };

    // Initialize UI
    updateTBUI();
    updateCBUI();
  </script>
</body>
</html>`

// PlaygroundHandler serves the interactive HTML5 visualizer for rate limiting and circuit breaking.
func PlaygroundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(playgroundHTML))
	}
}
