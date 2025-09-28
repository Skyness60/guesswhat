let ws = null;
let pseudo = "";
let currentRoomCode = "";
let isHost = false;
let isDrawingPlayer = false;
let currentWord = "";
let hasGuessed = false;
let timerInterval = null;

let canvas, ctx;
let drawing = false;
let currentTool = "pencil";
let currentColor = "#000000";
let currentSize = 5;
let lastPos = null;
const historyStack = [];

const PALETTE = [
  "#ffffff", "#000000", "#888888", "#444444",
  "#ff0000", "#990000", "#ff8000", "#cc6600",
  "#ffff00", "#cccc00", "#00ff00", "#008800",
  "#00cfff", "#0077bb", "#008888", "#004455",
  "#8000ff", "#440088", "#ff00bb", "#990066",
  "#ffe4b5", "#bfa76f", "#7a5230", "#3d2615"
];

const $ = (s) => document.querySelector(s);
const $$ = (s) => Array.from(document.querySelectorAll(s));

function setVisible(el, show) {
  if (el) el.style.display = show ? "" : "none";
}

function log(m) {
  const li = document.createElement("li");
  li.textContent = m;
  $("#log").appendChild(li);
  $("#log").scrollTop = $("#log").scrollHeight;
}

function revealWord(word) {
  $("#word-display").textContent = `Mot : ${word}`;
}

function onCreateRoomClick() {
  const error = $("#home-error");
  const p = $("#pseudo-home").value.trim();
  const type = $("#room-type").value;
  if (!p) return (error.textContent = "Pseudo obligatoire");

  pseudo = p;
  connectWS(() => {
    ws.send(JSON.stringify({ type: "nickname", content: pseudo }));
    ws.send(JSON.stringify({ type: "create_room", content: type }));
  });
}

function joinRoom() {
  const error = $("#home-error");
  const p = $("#pseudo-home").value.trim();
  const code = $("#roomcode").value.trim().toUpperCase();

  if (!p) return (error.textContent = "Pseudo obligatoire");
  if (!code) return (error.textContent = "Code room manquant");

  pseudo = p;
  currentRoomCode = code;
  error.textContent = "";

  if (!ws || ws.readyState !== WebSocket.OPEN) {
    connectWS(() => {
      ws.send(JSON.stringify({ type: "nickname", content: pseudo }));
      ws.send(JSON.stringify({ type: "join_room", content: currentRoomCode }));
    });
  } else {
    ws.send(JSON.stringify({ type: "nickname", content: pseudo }));
    ws.send(JSON.stringify({ type: "join_room", content: currentRoomCode }));
  }
}

function enterRoomUI() {
  setVisible($("#home"), false);
  setVisible($("#game"), true);
  setVisible($(".sidebar.right"), true);
}

function showRoomConfig(code) {
  const cfg = $("#room-config");
  cfg.innerHTML = `
    <h2>Paramètres de la partie</h2>
    <p><b>Code :</b> ${code}</p>
    <button id="btn-start" class="btn-primary" onclick="hostStartGame()">🚀 Démarrer la partie</button>
    <p><i>(Visible uniquement par l’hôte et si 2 joueurs minimum)</i></p>
  `;
  setVisible(cfg, true);
  updateStartButtonState();
}

function updateStartButtonState() {
  const count = $("#player-list")?.children.length || 0;
  const btn = $("#btn-start");
  if (!btn) return;
  btn.disabled = count < 2;
  btn.style.display = isHost ? "" : "none";
}

function hideRoomConfig() {
  setVisible($("#room-config"), false);
}

function hostStartGame() {
  if (isHost && ws) {
    ws.send(JSON.stringify({ type: "start_game" }));
  }
}

function startGameUI() {
  hideRoomConfig();
  hasGuessed = false;

  const drawContainer = $("#draw-container");
  drawContainer.innerHTML = "";

  canvas = document.createElement("canvas");
  canvas.id = "draw";
  canvas.width = 1200;
  canvas.height = 800;
  canvas.style.maxWidth = "100%";
  canvas.style.border = "2px solid var(--accent)";
  drawContainer.appendChild(canvas);

  ctx = canvas.getContext("2d");
  ctx.lineCap = "round";
  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, canvas.width, canvas.height);

  injectTools();
  bindDrawingEvents();
  setDrawingEnabled(isDrawingPlayer);
}

function injectTools() {
  const container = document.createElement("div");
  container.className = "toolbar";

  const palette = document.createElement("div");
  palette.className = "palette";
  PALETTE.forEach(c => {
    const b = document.createElement("button");
    b.className = "color-swatch";
    b.style.background = c;
    b.onclick = () => setColor(c);
    palette.appendChild(b);
  });

  const picker = document.createElement("input");
  picker.type = "color";
  picker.value = currentColor;
  picker.oninput = (e) => setColor(e.target.value);

  const thickness = document.createElement("div");
  thickness.className = "thickness";
  thickness.innerHTML = `
    <label>Épaisseur</label>
    <input type="range" id="size-picker" min="1" max="32" value="${currentSize}">
    <span id="size-preview"></span>
  `;

  const tools = document.createElement("div");
  tools.className = "tools-right";
  tools.innerHTML = `
    <button id="tool-pencil" title="Crayon">✏️</button>
    <button id="tool-bucket" title="Seau">🪣</button>
    <button id="tool-undo" title="Annuler">↩️</button>
    <button id="tool-clear" title="Effacer">🗑️</button>
  `;

  container.append(palette, picker, thickness, tools);
  $("#draw-container").appendChild(container);

  $("#size-picker").oninput = (e) => updateSize(e.target.value);
  $("#tool-pencil").onclick = () => setTool("pencil");
  $("#tool-bucket").onclick = () => setTool("fill");
  $("#tool-undo").onclick = sendUndo;
  $("#tool-clear").onclick = clearDraw;

  updateSize(currentSize);
  setTool("pencil");
}

function setTool(tool) {
  currentTool = tool;
  $$("#tool-pencil, #tool-bucket").forEach(b => b.classList.remove("active"));
  if (tool === "pencil") $("#tool-pencil").classList.add("active");
  else $("#tool-bucket").classList.add("active");
}

function setColor(c) {
  currentColor = c;
  $("#size-preview").style.background = c;
}

function updateSize(v) {
  currentSize = parseInt(v, 10);
  const prev = $("#size-preview");
  prev.style.width = `${currentSize}px`;
  prev.style.height = `${currentSize}px`;
}

function setDrawingEnabled(enabled) {
  if (!canvas) return;
  canvas.style.pointerEvents = enabled ? "" : "none";
  canvas.style.opacity = enabled ? "1" : "0.5";
}

function bindDrawingEvents() {
  if (!canvas) return;

  canvas.onmousedown = (e) => {
    if (!isDrawingPlayer) return;
    drawing = true;
    const pos = getPos(e);
    historyStack.push(ctx.getImageData(0, 0, canvas.width, canvas.height));
    lastPos = pos;
  };

  canvas.onmousemove = (e) => {
    if (!drawing || currentTool !== "pencil" || !isDrawingPlayer) return;
    const pos = getPos(e);
    drawLine(lastPos, pos, currentColor, currentSize);
    ws.send(JSON.stringify({
      type: "draw",
      content: JSON.stringify({ from: lastPos, to: pos, color: currentColor, size: currentSize })
    }));
    lastPos = pos;
  };

  canvas.onmouseup = () => drawing = false;
  canvas.onmouseleave = () => drawing = false;

  canvas.onclick = (e) => {
    if (currentTool === "fill" && isDrawingPlayer) {
      const pos = getPos(e);
      floodFill(pos.x, pos.y, currentColor);
      ws.send(JSON.stringify({ type: "fill", content: JSON.stringify({ x: pos.x, y: pos.y, color: currentColor }) }));
    }
  };
}

function getPos(e) {
  const rect = canvas.getBoundingClientRect();
  return { x: Math.floor(e.clientX - rect.left), y: Math.floor(e.clientY - rect.top) };
}

function drawLine(from, to, color, size) {
  if (!ctx) return;
  ctx.strokeStyle = color;
  ctx.lineWidth = size;
  ctx.beginPath();
  ctx.moveTo(from.x, from.y);
  ctx.lineTo(to.x, to.y);
  ctx.stroke();
}

function floodFill(x, y, fillColor) {
  const imgData = ctx.getImageData(0, 0, canvas.width, canvas.height);
  const data = imgData.data;
  const targetColor = getPixelColor(imgData, x, y);
  const replacement = hexToRgb(fillColor);
  if (colorsMatch(targetColor, replacement)) return;

  const stack = [{ x, y }];
  while (stack.length > 0) {
    const { x, y } = stack.pop();
    const idx = (y * canvas.width + x) * 4;
    const current = [data[idx], data[idx + 1], data[idx + 2]];
    if (!colorsMatch(current, targetColor)) continue;
    data[idx] = replacement.r;
    data[idx + 1] = replacement.g;
    data[idx + 2] = replacement.b;
    if (x > 0) stack.push({ x: x - 1, y });
    if (x < canvas.width - 1) stack.push({ x: x + 1, y });
    if (y > 0) stack.push({ x, y: y - 1 });
    if (y < canvas.height - 1) stack.push({ x, y: y + 1 });
  }
  ctx.putImageData(imgData, 0, 0);
}

function getPixelColor(imgData, x, y) {
  const i = (y * canvas.width + x) * 4;
  return [imgData.data[i], imgData.data[i + 1], imgData.data[i + 2]];
}

function colorsMatch(a, b) {
  return a[0] === b.r && a[1] === b.g && a[2] === b.b;
}

function hexToRgb(hex) {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

function sendMsg() {
  const input = $("#msg");
  const content = input.value.trim();
  if (!content || !ws || isDrawingPlayer || hasGuessed) return;
  ws.send(JSON.stringify({ type: "message", content }));
  input.value = "";
}

function startTimer(seconds) {
  clearInterval(timerInterval);
  const timer = $("#word-timer");
  timer.textContent = `${seconds}s`;

  timerInterval = setInterval(() => {
    seconds--;
    timer.textContent = `${seconds}s`;
    if (seconds <= 0) clearInterval(timerInterval);
  }, 1000);
}

function stopTimer() {
  clearInterval(timerInterval);
  $("#word-timer").textContent = `0s`;
}

function connectWS(onReady) {
  ws = new WebSocket("ws://" + location.host + "/ws");
  ws.onopen = () => onReady?.();
  ws.onmessage = async (e) => {
    const text = e.data instanceof Blob ? await e.data.text() : e.data;
    handleMsg(text);
  };
}

