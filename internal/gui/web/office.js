'use strict';
// baton office: every Claude Code conversation on this PC, live, as people at
// work in an isometric office. Just for fun, and off unless Settings turns it
// on.
//
// The server (internal/office) reads the transcripts and answers /api/office
// with a snapshot of the session in view plus what happened since the page
// last asked. Here those become Claude and its subagents walking between
// rooms. All the art is drawn in code (no image files, no fonts), and no
// markup is ever built from strings: the page's CSP allows neither.
//
// app.js mounts it: BatonOffice.mount(container, {api, t, h, lang, focus,
// popout, standalone, accounts}), then show() / hide() as the tab changes.

(() => {
  // ---- the floor plan ---------------------------------------------------------------
  // Tiles are 2:1 isometric: x runs down-right and y down-left from the back
  // corner, where the two walls meet. A tile is 64×32 world px at zoom 1.
  const GW = 20, GH = 16;
  const HW = 32, HH = 16;
  const WALL = 104, SLAB = 14;
  const pt = (x, y, z = 0) => [(x - y) * HW, (x + y) * HH - z];
  // What the whole building covers, in world px (walls and a margin included).
  const WORLD = { l: -GH * HW - 24, r: GW * HW + 24, t: -WALL - 34, b: (GW + GH) * HH + SLAB + 26 };

  const ROOMS = ['library', 'dev', 'server', 'search', 'meeting', 'desk', 'seat', 'lounge', 'elevator'];
  const ROOM_EN = { library: 'LIBRARY', dev: 'DEV', server: 'SERVER', search: 'SEARCH', meeting: 'MEETING', desk: 'MY DESK',
    seat: 'CLAUDE', lounge: 'LOUNGE', elevator: 'ELEVATOR' };
  const ROOM_ICON = { library: '📚', dev: '💻', server: '🗄️', search: '🌐', meeting: '📋', desk: '🙋', seat: '🪑', lounge: '☕', elevator: '🛗' };

  // Each room's floor. The rest of the floor is corridor.
  const ZONES = {
    library: [0, 0, 6, 6], dev: [6, 0, 14, 6], elevator: [14, 0, 20, 4], server: [14, 4, 20, 10],
    meeting: [0, 6, 6, 11], lounge: [0, 11, 6, 16], desk: [7, 7, 13, 11], seat: [9, 7, 13, 11], search: [8, 12, 20, 16],
  };

  // Low walls between rooms, on tile edges: [axis, at, from, to, door, glass].
  // 'x' runs along the line x=at from y=from to y=to; the door is the one gap.
  const WALLS = [
    ['x', 6, 0, 6, 4, false], ['y', 6, 0, 6, 4, false], ['y', 6, 6, 14, 9, false], ['x', 14, 0, 4, 2, false],
    ['x', 14, 4, 10, 7, true], ['y', 4, 14, 20, 17, true], ['y', 10, 14, 20, 17, true],
    ['x', 6, 6, 11, 9, false], ['y', 11, 0, 6, 4, false],
  ];

  // Spots are where people stand or sit: a tile to walk to, the exact point
  // (px, py), the way they face (se, sw: towards us; ne, nw: away) and a pose.
  function spot(room, x, y, face, pose, more) {
    return Object.assign({ room, x, y, px: x + 0.5, py: y + 0.5, face, pose: pose || 'stand', by: null }, more || {});
  }
  const SPOTS = {
    library: [spot('library', 1, 2, 'se', 'sit'), spot('library', 4, 3, 'nw', 'sit'), spot('library', 2, 4, 'ne', 'sit'),
      spot('library', 2, 1, 'ne'), spot('library', 1, 3, 'se', 'sit'), spot('library', 4, 2, 'nw', 'sit'), spot('library', 3, 4, 'ne', 'sit'),
      spot('library', 4, 1, 'ne'), spot('library', 1, 5, 'nw')],
    dev: [8, 10, 7, 11].flatMap((x) => [spot('dev', x, 2, 'ne', 'sit', { desk: x + ',1' }), spot('dev', x, 5, 'ne', 'sit', { desk: x + ',4' })]),
    server: [spot('server', 17, 6, 'ne'), spot('server', 16, 7, 'nw'), spot('server', 19, 6, 'ne'), spot('server', 16, 8, 'nw'),
      spot('server', 18, 6, 'ne'), spot('server', 16, 6, 'nw')],
    search: [spot('search', 11, 13, 'se', 'stand', { py: 13.9 }), spot('search', 16, 14, 'sw', 'sit'), spot('search', 9, 13, 'se', 'stand', { py: 13.9 }),
      spot('search', 13, 13, 'se', 'stand', { py: 13.9 }), spot('search', 17, 13, 'se', 'stand', { py: 13.9 })],
    meeting: [spot('meeting', 1, 8, 'nw', 'stand', { board: true }), spot('meeting', 2, 6, 'sw', 'sit'), spot('meeting', 4, 8, 'nw', 'sit'),
      spot('meeting', 3, 9, 'ne', 'sit'), spot('meeting', 3, 6, 'sw', 'sit'), spot('meeting', 4, 7, 'nw', 'sit'), spot('meeting', 2, 9, 'ne', 'sit')],
    desk: [spot('desk', 7, 8, 'se', 'stand', { px: 7.6, py: 8.55 }), spot('desk', 7, 9, 'se'), spot('desk', 8, 10, 'ne')],
    seat: [spot('seat', 12, 8, 'nw'), spot('seat', 11, 10, 'ne'), spot('seat', 12, 9, 'nw'), spot('seat', 10, 10, 'ne')],
    // Claude takes the first free sofa: the end by the door, so it does not
    // walk across the other one.
    lounge: [spot('lounge', 3, 11, 'sw', 'sit', { sofa: true }), spot('lounge', 1, 13, 'se', 'sit', { sofa: true }),
      spot('lounge', 2, 11, 'sw', 'sit', { sofa: true }), spot('lounge', 1, 12, 'se', 'sit', { sofa: true }),
      spot('lounge', 1, 15, 'nw', 'stand', { coffee: true }), spot('lounge', 4, 13, 'nw'), spot('lounge', 1, 14, 'nw', 'stand', { coffee: true })],
    elevator: [spot('elevator', 15, 1, 'ne'), spot('elevator', 18, 1, 'nw'), spot('elevator', 15, 3, 'ne'), spot('elevator', 19, 2, 'nw')],
  };
  // freeSpots empties every spot (a new conversation starts with a new cast).
  function freeSpots() { for (const list of Object.values(SPOTS)) for (const s of list) s.by = null; }
  const SEAT_MAIN = spot('seat', 10, 8, 'sw', 'sit', { px: 11, py: 8.45, main: true });
  const SEAT_ME = spot('desk', 8, 8, 'sw', 'sit', { px: 9, py: 8.45, me: true });
  // The tiles of every spot, with their chairs and sofas: a crowd's overflow
  // stands elsewhere.
  const SPOT_TILES = new Set([...Object.values(SPOTS).flat(), SEAT_MAIN, SEAT_ME].flatMap((s) => [s.x + ',' + s.y, Math.floor(s.px) + ',' + Math.floor(s.py)]));
  const LIFT_DOOR = [17, 0.3];
  // Where people step into the elevator to leave (anyone may stand there).
  const DOOR = spot('elevator', 17, 0, 'ne', 'stand', { px: LIFT_DOOR[0], py: LIFT_DOOR[1], door: true });
  // Inside the elevator car, behind the doors (y < 0): where newcomers wait
  // for the doors to open and where people leaving turn round. Up to three
  // stand side by side, in the middle of what the open doors show at that
  // depth (a point at y < 0 shows further right).
  const CAR_Y = -0.55, CAR_X = [16.7, 16.3, 17.1];

  // ---- looks ---------------------------------------------------------------------------
  // Claude wears its baton account's color (by account order); a session
  // outside baton wears Claude's own coral.
  const CORAL = '#d97757';
  const ACCOUNT_COLORS = ['#4f7fdc', '#2ea67a', '#9a6be0', '#e39b2d', '#e05d8f', '#2aa5b8', '#7d8a99', '#c8693f'];
  const ROLES = {
    'Explore': { color: '#3fae6a', acc: 'cap' },
    'Plan': { color: '#8e6bd8', acc: 'glasses' },
    'general-purpose': { color: '#4a86e8', acc: 'headset' },
    'claude-code-guide': { color: '#23a69a', acc: 'book' },
    'workflow-subagent': { color: '#64748b', acc: 'hardhat' },
    'statusline-setup': { color: '#e2b52f', acc: 'beanie' },
  };
  const HAIR = ['#3b2a22', '#5a3b28', '#2b2b33', '#7a4b2a', '#4a3530', '#8a5a3c'];

  const DAY = {
    night: false,
    slabL: '#ddd5c9', slabR: '#cec5b7', shadow: 'rgba(70,58,44,.20)',
    floor: '#f3f0ea', floorLine: 'rgba(110,100,85,.10)',
    wood: '#edd5b0', woodLine: 'rgba(150,105,60,.22)',
    dev: '#e8edf4', devLine: 'rgba(90,110,140,.14)',
    marble: '#f0eef5', marbleLine: 'rgba(120,110,150,.14)',
    raised: '#e3e7ec', raisedLine: 'rgba(80,95,115,.20)',
    violet: '#ece7f7', violetEdge: '#d8cff0',
    check1: '#f7e3cf', check2: '#fdf6ee',
    blue: '#d9e7f9', blueEdge: '#adc8ed', blueDot: 'rgba(255,255,255,.75)',
    mint1: '#dcf2ea', mint2: '#cdebdf', mintLine: 'rgba(60,140,110,.12)',
    wallR: '#f7f3ed', wallL: '#ece6dc', wallTop: '#fffdf9', wallTrim: '#d6cdbf',
    sky1: '#9fd3ff', sky2: '#e6f4ff', cloud: 'rgba(255,255,255,.95)', frame: '#ffffff', frameLine: '#cdd6e1',
    low: '#f8f5f0', lowL: '#e8e2d8', lowR: '#dcd5ca', glass: 'rgba(160,200,235,.30)', glassLine: 'rgba(255,255,255,.9)', post: '#c4ccd7',
    shelf: '#c99a6b', shelfL: '#b07f52', shelfR: '#966a42', shelfIn: '#7f5636',
    books: ['#e76f51', '#f4a261', '#e9c46a', '#2a9d8f', '#457b9d', '#9b7fd1', '#ef8fa8', '#6cc0a4', '#d65a5a'],
    table: '#ebd3ad', tableL: '#d7b98c', tableR: '#c6a678',
    white: '#fbfaf7', whiteL: '#e6e1d8', whiteR: '#d6d0c5',
    desk: '#f7f3ec', deskL: '#e3dbcf', deskR: '#d2c9bb',
    mon: '#3c4352', monL: '#30364a', screen: '#1f2a3c', screenOff: '#2a3346',
    code: ['#7dd3fc', '#fde68a', '#f9a8d4', '#86efac', '#c4b5fd', '#fdba74'],
    chair: '#66748b', chairL: '#556179', chairR: '#47526a', chairBase: '#8f99a8',
    rackTop: '#4a5163', rackL: '#363c4b', rackR: '#2c313e', rackLine: 'rgba(255,255,255,.10)',
    led: ['#4ade80', '#60a5fa', '#fbbf24', '#f87171'],
    kiosk: '#a58ae6', kioskL: '#8c6fd8', kioskR: '#765ac4', kioskTop: '#c8f1ff',
    globe: '#6bb6ec', land: '#88d08b', brass: '#c9a46c',
    sofa: '#e7a07f', sofaL: '#d58a6c', sofaR: '#c47a5e', cushion: '#f3b89d',
    pot: '#e2a983', potL: '#d0966f', potR: '#bd855f', leaf: ['#62b671', '#4e9e5e', '#80cd8b'], soil: '#7a5a43',
    board: '#ffffff', boardFrame: '#c3cad4', ink: '#3b5bdb', inkDone: '#a0a9b4', ink2: '#e8590c',
    counter: '#f0e7db', counterL: '#ddd0bf', counterR: '#cdbeab', machine: '#4b3a2f', machineL: '#3c2f26',
    lift: '#d8dee6', liftLine: '#aab3c0', liftFrame: '#a3adba', liftIn: '#eef1f5', panel: '#2b2f38', digit: '#ffb547',
    sign: '#fffdf8', signLine: '#e0d6c7', signText: '#5a5249',
    label: 'rgba(85,76,64,.42)',
    cooler: '#eef2f6', water: 'rgba(110,180,240,.65)',
    lamp: '#ffd98a', glow: 'rgba(255,214,140,0)',
    skin: '#f9dcc5', blush: 'rgba(244,128,128,.42)', eye: '#2b2522', pants: '#434a5c', shoe: '#2c303b',
    tagShadow: 'rgba(0,0,0,.18)',
  };
  const NIGHT = Object.assign({}, DAY, {
    night: true,
    slabL: '#2a2e39', slabR: '#22262f', shadow: 'rgba(0,0,0,.45)',
    floor: '#2c303a', floorLine: 'rgba(255,255,255,.05)',
    wood: '#584432', woodLine: 'rgba(0,0,0,.28)',
    dev: '#2c3341', devLine: 'rgba(255,255,255,.06)',
    marble: '#353345', marbleLine: 'rgba(255,255,255,.06)',
    raised: '#272c35', raisedLine: 'rgba(120,200,255,.10)',
    violet: '#38334d', violetEdge: '#484162',
    check1: '#46372e', check2: '#3b2f28',
    blue: '#2a3b56', blueEdge: '#3b5679', blueDot: 'rgba(255,255,255,.12)',
    mint1: '#22423b', mint2: '#1e3a34', mintLine: 'rgba(255,255,255,.05)',
    wallR: '#363c4a', wallL: '#2f3441', wallTop: '#474e5e', wallTrim: '#262a34',
    sky1: '#0b1330', sky2: '#26386a', cloud: 'rgba(255,255,255,.07)', frame: '#4b5364', frameLine: '#3a4151',
    low: '#3d4352', lowL: '#353a48', lowR: '#2d323e', glass: 'rgba(110,150,210,.16)', glassLine: 'rgba(180,210,255,.32)', post: '#4b5466',
    shelf: '#6d4f37', shelfL: '#5c422e', shelfR: '#4d3726', shelfIn: '#36271c',
    books: ['#b5573f', '#bf7f4b', '#b79b52', '#237a70', '#37607d', '#7a64a5', '#b87083', '#55977f', '#a64848'],
    table: '#6e5640', tableL: '#5c4734', tableR: '#4d3b2b',
    white: '#4a4f5c', whiteL: '#3e434f', whiteR: '#343843',
    desk: '#4a4f5c', deskL: '#3e434f', deskR: '#343843',
    mon: '#1d212b', monL: '#171a22', screen: '#16202f', screenOff: '#1b2230',
    chair: '#3f4757', chairL: '#353c4a', chairR: '#2c323e', chairBase: '#5b6372',
    rackTop: '#2f3542', rackL: '#232833', rackR: '#1c2029',
    kiosk: '#6a55a8', kioskL: '#5a4695', kioskR: '#4b3a80', kioskTop: '#7fd8ff',
    globe: '#3f7fb5', land: '#5a9a62', brass: '#9a7c4c',
    sofa: '#9c5f49', sofaL: '#87513d', sofaR: '#764533', cushion: '#ad6c53',
    pot: '#9e7458', potL: '#8a644b', potR: '#77553f', leaf: ['#3f7f4c', '#336b40', '#4f9259'], soil: '#4a372a',
    board: '#d9dde4', boardFrame: '#596273', ink: '#2f4bb8', inkDone: '#7c8592',
    counter: '#4c4740', counterL: '#403b35', counterR: '#36322d', machine: '#2a211b', machineL: '#211a15',
    lift: '#525b69', liftLine: '#3f4754', liftFrame: '#606a79', liftIn: '#3d4556', panel: '#15181e',
    sign: '#3f4555', signLine: '#525a6b', signText: '#e6e1d8',
    label: 'rgba(230,224,214,.30)',
    cooler: '#596273', water: 'rgba(90,160,230,.55)',
    glow: 'rgba(255,214,140,0)',
    skin: '#efceb6', blush: 'rgba(240,120,120,.35)', pants: '#2f3542', shoe: '#1d2028',
  });

  // ---- small helpers ----------------------------------------------------------------------
  function hash(s) {
    let x = 2166136261;
    for (let i = 0; i < s.length; i++) { x ^= s.charCodeAt(i); x = Math.imul(x, 16777619); }
    return x >>> 0;
  }
  function rng(seed) { // mulberry32: the same seed draws the same shelf of books every time
    let a = seed >>> 0;
    return () => { a = (a + 0x6D2B79F5) | 0; let r = Math.imul(a ^ (a >>> 15), 1 | a); r = (r + Math.imul(r ^ (r >>> 7), 61 | r)) ^ r; return ((r ^ (r >>> 14)) >>> 0) / 4294967296; };
  }
  function rgb(hex) {
    if (typeof hex !== 'string' || !/^#[0-9a-f]{6}$/i.test(hex)) hex = CORAL; // one bad colour must not stop the drawing
    const n = parseInt(hex.slice(1), 16); return [n >> 16, (n >> 8) & 255, n & 255];
  }
  function hex3(r, g, b) { return '#' + ((1 << 24) | (Math.round(r) << 16) | (Math.round(g) << 8) | Math.round(b)).toString(16).slice(1); }
  function shade(hex, f) { // f < 0 darkens, f > 0 lightens
    const [r, g, b] = rgb(hex), to = f < 0 ? 0 : 255, k = Math.abs(f);
    return hex3(r + (to - r) * k, g + (to - g) * k, b + (to - b) * k);
  }
  function hslHex(h, s, l) {
    s /= 100; l /= 100;
    const f = (n) => { const k = (n + h / 30) % 12; return l - s * Math.min(l, 1 - l) * Math.max(-1, Math.min(k - 3, 9 - k, 1)); };
    return hex3(f(0) * 255, f(8) * 255, f(4) * 255);
  }
  const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
  const short = (id) => (id || '').slice(0, 8);
  const toolRoom = (tool) => {
    if (!tool) return 'seat';
    if (/^(Read|Grep|Glob|LS|ToolSearch|NotebookRead)$/.test(tool)) return 'library';
    if (/^(Edit|Write|MultiEdit|NotebookEdit)$/.test(tool)) return 'dev';
    if (/^(Bash|PowerShell|BashOutput|KillShell|Monitor|TaskStop)$/.test(tool)) return 'server';
    if (tool === 'mcp__ccd_turn__update_plan') return 'meeting';
    if (tool === 'mcp__ccd_turn__wrap_up') return 'seat';
    if (/^(WebFetch|WebSearch)$/.test(tool) || tool.startsWith('mcp__')) return 'search';
    if (/^(TodoWrite|TaskCreate|TaskUpdate|TaskList|TaskGet|EnterPlanMode|Workflow|Skill)$/.test(tool)) return 'meeting';
    if (/^(AskUserQuestion|ExitPlanMode)$/.test(tool)) return 'desk';
    return 'seat';
  };
  const isAgentTool = (tool) => tool === 'Agent' || tool === 'Task';
  // A git command gets a little git flag (the server may say so, or the
  // command's short description starts with git).
  const isGit = (ev) => ev.git === true || (/^(Bash|PowerShell)$/.test(ev.tool || '') && /(^|[\s"'`])git(\s|$)/i.test(ev.detail || ''));

  // ---- the office's state ----------------------------------------------------------------
  let opts = null, h = null, t = (k) => k;
  let root = null, els = {};
  let shown = false, destroyed = false;
  let P = DAY, themeKey = 'day';
  // Night falls with the window's dark colours (theme.js sets data-theme).
  new MutationObserver(() => theme()).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

  let seq = 0, skew = 0, me = '', building = null, view = null, acctOrder = [];
  let follow = true, pinned = '';
  let notice = ''; // '', 'off', 'gone', 'expired', 'retry'
  const actors = new Map();
  let ghostN = 0;
  const log = [];
  const openFloors = new Set(), closedFloors = new Set();
  let quietOpen = false;
  // What the user hid from the office is kept by the server, which flags it
  // in the building. A change just made is laid over those flags until the
  // building shows it (or for 5 s): 's:<id>' or 'f:<floor>' -> {hidden, at}.
  const pend = new Map();
  const PEND_MS = 5000;
  let hiddenOpen = false;
  const floorPaths = new Map(); // floor number -> its folder, to see a number go to another project

  try {
    const saved = JSON.parse(sessionStorage.getItem('baton_office') || 'null');
    if (saved && typeof saved.pinned === 'string') { pinned = saved.pinned; follow = !pinned; }
  } catch (_) { /* a fresh office */ }
  function remember() {
    try { sessionStorage.setItem('baton_office', JSON.stringify({ pinned })); } catch (_) { /* this load only */ }
  }

  const langOf = () => (opts && (typeof opts.lang === 'function' ? opts.lang() : opts.lang)) || 'ko';
  const serverNow = () => Date.now() + skew;
  function ago(iso) {
    const m = Math.round((serverNow() - new Date(iso)) / 60000);
    if (!iso || isNaN(m)) return '';
    if (m < 1) return t('ago_now');
    if (m < 60) return t('ago_min', { n: m });
    if (m < 48 * 60) return t('ago_hour', { n: Math.round(m / 60) });
    return t('ago_day', { n: Math.round(m / 1440) });
  }
  function span(ms) { // a short length of time: 12초, 3분, 2시간
    const s = Math.max(0, Math.round(ms / 1000));
    if (s < 60) return t('office_secs', { n: s });
    if (s < 3600) return t('office_mins', { n: Math.floor(s / 60) });
    return t('office_hours', { n: Math.floor(s / 3600) });
  }
  function clockOf(iso) {
    const d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleTimeString(langOf() === 'ko' ? 'ko-KR' : 'en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  }
  function accountColor(name, index) {
    if (!name) return CORAL;
    // The server's order (accounts.json), the one account_index counts in.
    const list = acctOrder.length ? acctOrder : (opts && opts.accounts ? opts.accounts() : []);
    const i = list.indexOf(name);
    const k = i >= 0 ? i : (typeof index === 'number' && index >= 0 ? index : hash(name));
    return ACCOUNT_COLORS[k % ACCOUNT_COLORS.length];
  }
  function roleOf(type) {
    // An own property only: a custom agent may well be called "constructor".
    if (Object.prototype.hasOwnProperty.call(ROLES, type)) return ROLES[type];
    const hh = hash(type || 'agent') % 360;
    return { color: hslHex(hh, 55, 55), acc: 'tag' };
  }
  function lookFor(s) {
    const seed = hash(s.id || 'x');
    if (s.kind === 'me') return { color: '#7d8fb8', acc: 'hood', hair: '#2a2522', seed, me: true };
    if (s.kind === 'claude') return { color: accountColor(s.account, s.account_index), acc: 'claude', hair: '#4a3328', seed, claude: true };
    const r = roleOf(s.type);
    return { color: r.color, acc: r.acc, hair: HAIR[seed % HAIR.length], seed };
  }
  function nameOf(s) {
    if (!s) return '';
    if (s.kind === 'me' || s.id === 'me') return t('office_me', { name: s.label || me || 'me' });
    if (s.kind === 'claude' || s.id === 'main') return s.account ? 'Claude · ' + s.account : 'Claude';
    return s.label || s.type || t('office_agent');
  }

  // ---- the pathfinding grid -------------------------------------------------------------
  const blocked = new Uint8Array(GW * GH);
  const edgeX = new Set(), edgeY = new Set();
  for (const [axis, at, from, to, door] of WALLS) {
    for (let i = from; i < to; i++) if (i !== door) (axis === 'x' ? edgeX : edgeY).add(axis === 'x' ? at + ',' + i : i + ',' + at);
  }
  const freeTile = (x, y) => x >= 0 && y >= 0 && x < GW && y < GH && !blocked[y * GW + x];
  function canStep(x, y, nx, ny) {
    if (!freeTile(nx, ny)) return false;
    if (nx !== x && ny !== y) return canStep(x, y, nx, y) && canStep(nx, y, nx, ny) && canStep(x, y, x, ny) && canStep(x, ny, nx, ny);
    if (nx !== x) return !edgeX.has(Math.max(x, nx) + ',' + y);
    return !edgeY.has(x + ',' + Math.max(y, ny));
  }
  const DIRS = [[1, 0], [-1, 0], [0, 1], [0, -1], [1, 1], [1, -1], [-1, 1], [-1, -1]];
  // findPath: A* over the tiles, 8 ways, never cutting a corner or a wall.
  function findPath(sx, sy, tx, ty) {
    sx = clamp(sx, 0, GW - 1); sy = clamp(sy, 0, GH - 1);
    if (sx === tx && sy === ty) return [];
    const N = GW * GH, goal = ty * GW + tx;
    const g = new Float32Array(N).fill(Infinity), from = new Int32Array(N).fill(-1), done = new Uint8Array(N);
    const hf = (i) => { const dx = Math.abs(i % GW - tx), dy = Math.abs(((i / GW) | 0) - ty); return Math.max(dx, dy) + 0.414 * Math.min(dx, dy); };
    const open = [sy * GW + sx];
    g[open[0]] = 0;
    while (open.length) {
      let bi = 0;
      for (let k = 1; k < open.length; k++) if (g[open[k]] + hf(open[k]) < g[open[bi]] + hf(open[bi])) bi = k;
      const cur = open.splice(bi, 1)[0];
      if (cur === goal) break;
      done[cur] = 1;
      const cx = cur % GW, cy = (cur / GW) | 0;
      for (const [dx, dy] of DIRS) {
        const nx = cx + dx, ny = cy + dy;
        if (!canStep(cx, cy, nx, ny)) continue;
        const ni = ny * GW + nx;
        if (done[ni]) continue;
        const ng = g[cur] + (dx && dy ? 1.414 : 1);
        if (ng < g[ni]) { g[ni] = ng; from[ni] = cur; if (!open.includes(ni)) open.push(ni); }
      }
    }
    if (from[goal] < 0) return null;
    const out = [];
    for (let i = goal; i !== sy * GW + sx; i = from[i]) out.unshift([i % GW + 0.5, ((i / GW) | 0) + 0.5]);
    return out;
  }

  // ---- props --------------------------------------------------------------------------------
  // A prop is drawn once into a sprite of its own and blitted every frame;
  // the parts that move (code on monitors, LEDs, steam) are drawn live on top.
  const props = [];
  function prop(type, x, y, w, d, ht, more) {
    const p = Object.assign({ id: type + '@' + x + ',' + y, type, x, y, w, d, ht, solid: true }, more || {});
    p.depth = p.depth ?? (x + w / 2 + y + d / 2);
    props.push(p);
    if (p.solid) for (let i = x; i < x + w; i++) for (let j = y; j < y + d; j++) blocked[j * GW + i] = 1;
    return p;
  }
  for (let x = 0; x < 6; x++) prop('shelfR', x, 0, 1, 1, 80, { depth: x + 0.6 });
  for (let y = 1; y < 6; y++) prop('shelfL', 0, y, 1, 1, 80, { depth: y + 0.6 });
  prop('rtable', 2, 2, 2, 2, 40);
  prop('plant', 5, 5, 1, 1, 46, { kind: 'palm' });
  for (const x of [7, 8, 10, 11]) for (const y of [1, 4]) prop('desk', x, y, 1, 1, 52);
  prop('printer', 13, 5, 1, 1, 30);
  prop('plant', 13, 0, 1, 1, 46, { kind: 'bush' });
  prop('plant', 14, 0, 1, 1, 50, { kind: 'tall' });
  prop('plant', 19, 0, 1, 1, 46, { kind: 'palm' });
  for (let y = 5; y < 9; y++) prop('rack', 15, y, 1, 1, 90, { front: 'x' });
  for (let x = 16; x < 20; x++) prop('rack', x, 5, 1, 1, 90, { front: 'y' });
  prop('mtable', 2, 7, 2, 2, 40);
  prop('plant', 5, 10, 1, 1, 46, { kind: 'bush' });
  // The sofas along the meeting room's partition come after it (or it would
  // hide their backs) and before whoever sits on them. The armrest at the
  // near end of each sofa is in front of whoever sits next to it, so it is a
  // prop of its own, drawn after them (like the back of a chair).
  prop('sofa', 2, 11, 1, 1, 26, { solid: false, face: 'sw', end: 'l', depth: 13.6 });
  prop('sofa', 3, 11, 1, 1, 26, { solid: false, face: 'sw', depth: 14.6 });
  prop('sofa', 1, 12, 1, 1, 26, { solid: false, face: 'se', end: 'l', depth: 13.5 });
  prop('sofa', 1, 13, 1, 1, 26, { solid: false, face: 'se', depth: 14.5 });
  prop('sofaarm', 3, 11, 1, 1, 17, { solid: false, face: 'sw', depth: 3.5 + 11.5 + 0.02 });
  prop('sofaarm', 1, 13, 1, 1, 17, { solid: false, face: 'se', depth: 1.5 + 13.5 + 0.02 });
  prop('ctable', 2, 12, 2, 2, 24);
  prop('counter', 0, 14, 1, 1, 40);
  prop('counter', 0, 15, 1, 1, 56, { machine: true });
  prop('plant', 0, 11, 1, 1, 46, { kind: 'tall' });
  prop('plant', 5, 15, 1, 1, 46, { kind: 'palm' });
  prop('lamp', 5, 11, 1, 1, 66);
  prop('cooler', 6, 6, 1, 1, 50);
  prop('udesk', 8, 9, 2, 1, 54, { who: 'me' });
  prop('udesk', 10, 9, 2, 1, 54, { who: 'claude' });
  prop('plant', 7, 7, 1, 1, 40, { kind: 'small' });
  for (const x of [10, 12, 14]) prop('kiosk', x, 13, 1, 1, 70);
  prop('globe', 18, 13, 1, 1, 60);
  prop('wtable', 16, 15, 1, 1, 40);
  prop('plant', 8, 15, 1, 1, 46, { kind: 'bush' });
  prop('plant', 19, 15, 1, 1, 50, { kind: 'palm' });
  prop('plant', 19, 12, 1, 1, 46, { kind: 'tall' });
  prop('plant', 6, 11, 1, 1, 40, { kind: 'small' });
  // Chairs at every seat that is not a sofa; the back of a chair facing away
  // from us is drawn after its sitter, so it hides their lower back.
  for (const list of [...Object.values(SPOTS), [SEAT_MAIN, SEAT_ME]]) {
    for (const s of list) {
      if (s.pose !== 'sit' || s.sofa) continue;
      const away = s.face === 'ne' || s.face === 'nw';
      prop('chair', s.px, s.py, 0, 0, 30, { id: 'chair@' + s.px + ',' + s.py, solid: false, face: s.face, depth: s.px + s.py - 0.02 });
      prop('chairback', s.px, s.py, 0, 0, 30, { id: 'chairback@' + s.px + ',' + s.py, solid: false, face: s.face, depth: s.px + s.py + (away ? 0.02 : -0.01) });
    }
  }
  // Nobody walks over the user: the tile on our side of their chair is
  // taken. People pass on the far side, where they are drawn under the user.
  blocked[Math.floor(SEAT_ME.py) * GW + Math.floor(SEAT_ME.px)] = 1;
  // Wall segments sort with everything else, one tile edge at a time.
  const segs = [];
  for (const [axis, at, from, to, door, glass] of WALLS) {
    for (let i = from; i < to; i++) {
      if (i === door) continue;
      segs.push({ axis, at, i, glass, depth: at + i + 0.5, p0: i === from || i - 1 === door || (glass && (i - from) % 2 === 0),
        p1: i + 1 === to || i + 1 === door });
    }
  }

  // ---- drawing helpers --------------------------------------------------------------------
  function poly(g, pts, fill, stroke, lw) {
    g.beginPath();
    g.moveTo(pts[0][0], pts[0][1]);
    for (let i = 1; i < pts.length; i++) g.lineTo(pts[i][0], pts[i][1]);
    g.closePath();
    if (fill) { g.fillStyle = fill; g.fill(); }
    if (stroke) { g.strokeStyle = stroke; g.lineWidth = lw || 1; g.stroke(); }
  }
  // box draws a block on footprint x..x+w, y..y+d from height z to z+ht, with
  // its three faces we can see: top, the +y face (left) and the +x face (right).
  function box(g, x, y, w, d, z, ht, top, left, right, edge) {
    poly(g, [pt(x, y + d, z), pt(x + w, y + d, z), pt(x + w, y + d, z + ht), pt(x, y + d, z + ht)], left, edge, 0.6);
    poly(g, [pt(x + w, y, z), pt(x + w, y + d, z), pt(x + w, y + d, z + ht), pt(x + w, y, z + ht)], right, edge, 0.6);
    poly(g, [pt(x, y, z + ht), pt(x + w, y, z + ht), pt(x + w, y + d, z + ht), pt(x, y + d, z + ht)], top, edge, 0.6);
  }
  function floorQuad(g, x0, y0, x1, y1, fill, z = 0) {
    poly(g, [pt(x0, y0, z), pt(x1, y0, z), pt(x1, y1, z), pt(x0, y1, z)], fill);
  }
  function line(g, a, b, color, lw) {
    g.beginPath(); g.moveTo(a[0], a[1]); g.lineTo(b[0], b[1]);
    g.strokeStyle = color; g.lineWidth = lw || 1; g.stroke();
  }
  function ellipse(g, x, y, rx, ry, fill, stroke, lw) {
    g.beginPath(); g.ellipse(x, y, Math.max(0.1, rx), Math.max(0.1, ry), 0, 0, Math.PI * 2);
    if (fill) { g.fillStyle = fill; g.fill(); }
    if (stroke) { g.strokeStyle = stroke; g.lineWidth = lw || 1; g.stroke(); }
  }
  function rrect(g, x, y, w, ht, r, fill, stroke, lw) {
    g.beginPath(); g.roundRect(x, y, w, ht, r);
    if (fill) { g.fillStyle = fill; g.fill(); }
    if (stroke) { g.strokeStyle = stroke; g.lineWidth = lw || 1; g.stroke(); }
  }
  // Faces: local drawing on a wall or a box side. On the face at y=Y the
  // local u axis runs along +x; on the face at x=X it runs along +y; on the
  // left wall (x=0) text reads along -y. v always points down.
  function onFaceY(g, x, Y, z) { const p = pt(x, Y, z); g.transform(1, 0.5, 0, 1, p[0], p[1]); }
  function onFaceX(g, X, y, z) { const p = pt(X, y, z); g.transform(-1, 0.5, 0, 1, p[0], p[1]); }
  function onWallL(g, y, z) { const p = pt(0, y, z); g.transform(1, -0.5, 0, 1, p[0], p[1]); }
  function onFloor(g, x, y) { const p = pt(x, y); g.transform(1, 0.5, -1, 0.5, p[0], p[1]); }
  const FONT = '"Segoe UI Variable Text", "Segoe UI", "Malgun Gothic", system-ui, sans-serif';

  // ---- prop painters ----------------------------------------------------------------------
  const PAINT = {
    shelfR(g, p) {
      const x = p.x;
      box(g, x + 0.02, 0.04, 0.96, 0.44, 0, 78, P.shelf, P.shelfL, P.shelfR);
      g.save(); onFaceY(g, x + 0.02, 0.48, 78);
      books(g, 0.96 * HW, 78, hash(p.id));
      g.restore();
    },
    shelfL(g, p) {
      const y = p.y;
      box(g, 0.04, y + 0.02, 0.44, 0.96, 0, 78, P.shelf, P.shelfL, P.shelfR);
      g.save(); onFaceX(g, 0.48, y + 0.02, 78); g.transform(-1, 0, 0, 1, 0.96 * HW, 0);
      books(g, 0.96 * HW, 78, hash(p.id));
      g.restore();
    },
    rtable(g, p) { table(g, p, P.table, P.tableL, P.tableR, true); },
    mtable(g, p) { table(g, p, P.white, P.whiteL, P.whiteR, false); },
    desk(g, p) {
      const { x, y } = p;
      box(g, x + 0.08, y + 0.12, 0.06, 0.76, 0, 19, P.deskL, P.deskL, P.deskR);
      box(g, x + 0.86, y + 0.12, 0.06, 0.76, 0, 19, P.deskL, P.deskL, P.deskR);
      box(g, x + 0.05, y + 0.08, 0.9, 0.84, 19, 3, P.desk, P.deskL, P.deskR, 'rgba(0,0,0,.06)');
      box(g, x + 0.44, y + 0.24, 0.12, 0.08, 22, 6, P.mon, P.monL, P.monL);
      box(g, x + 0.12, y + 0.2, 0.76, 0.08, 27, 24, P.mon, P.mon, P.monL);
      g.save(); onFaceY(g, x + 0.12, y + 0.28, 51); rrect(g, 1.5, 1.5, 0.76 * HW - 3, 21, 1.5, P.screenOff); g.restore();
      poly(g, [pt(x + 0.24, y + 0.5, 22.2), pt(x + 0.72, y + 0.5, 22.2), pt(x + 0.72, y + 0.66, 22.2), pt(x + 0.24, y + 0.66, 22.2)], P.night ? '#5a6070' : '#d7d2c8');
      const r = rng(hash(p.id));
      if (r() < 0.5) box(g, x + 0.78, y + 0.55, 0.1, 0.1, 22, 5, '#fff', P.books[(r() * 9) | 0], P.books[(r() * 9) | 0]);
    },
    udesk(g, p) {
      const { x, y } = p, mine = p.who === 'me';
      // What is under the top comes first: the right leg (flush with the
      // front panel), the panel, then the top over both.
      box(g, x + 1.87, y + 0.12, 0.06, 0.7, 1, 19, P.deskL, P.deskL, P.deskR);
      box(g, x + 0.07, y + 0.78, 1.86, 0.08, 1, 19, P.deskL, P.deskL, P.deskR);
      box(g, x + 0.04, y + 0.1, 1.92, 0.8, 20, 3.5, P.desk, P.deskL, P.deskR, 'rgba(0,0,0,.06)');
      // Left of the monitor and behind it (on the sitter's side): a desk lamp
      // or a stack of paper, drawn before the monitor so it hides them.
      if (mine) {
        box(g, x + 0.1, y + 0.48, 0.18, 0.18, 23.5, 2, '#d0c8ba', '#bfb6a6', '#b0a696');
        const arm = P.night ? '#8f97a6' : '#6b7280', b = pt(x + 0.19, y + 0.57, 25.5);
        const elbow = [b[0] - 3, b[1] - 15], head = [elbow[0] + 7, elbow[1] + 2];
        line(g, b, elbow, arm, 1.6); line(g, elbow, head, arm, 1.6);
        poly(g, [[head[0] - 2.5, head[1] - 1], [head[0] + 2.5, head[1] - 1], [head[0] + 4.5, head[1] + 5], [head[0] - 4.5, head[1] + 5]], '#f2b84b');
      } else {
        for (let i = 0; i < 3; i++) box(g, x + 0.14, y + 0.44, 0.24, 0.3, 23.5 + i * 1.6, 1.4, '#fff', '#eeeae2', '#e2ddd3');
      }
      // The monitor faces the sitter, so we see its back.
      box(g, x + 0.94, y + 0.48, 0.12, 0.08, 23.5, 5, P.mon, P.monL, P.monL);
      box(g, x + 0.55, y + 0.56, 0.9, 0.07, 27, 22, P.mon, P.mon, P.monL);
      ellipse(g, ...pt(x + 1, y + 0.63, 38), 2.2, 1.6, mine ? '#9fb3d9' : CORAL);
      // Right of the monitor, clear of it: a mug or a sprout.
      if (mine) {
        box(g, x + 1.6, y + 0.3, 0.1, 0.1, 23.5, 6, '#fff', '#e7eef8', '#d3dcea');
      } else {
        box(g, x + 1.62, y + 0.24, 0.16, 0.16, 23.5, 5, P.pot, P.potL, P.potR);
        ellipse(g, ...pt(x + 1.7, y + 0.32, 32), 4, 3, P.leaf[0]);
        ellipse(g, ...pt(x + 1.66, y + 0.3, 35), 3, 2.4, P.leaf[2]);
      }
      // The name plate on the front panel.
      g.save(); onFaceY(g, x + 0.07, y + 0.86, 20);
      const name = mine ? (me || 'me') : 'Claude';
      g.font = '700 7px ' + FONT;
      const tw = Math.min(46, g.measureText(name).width + (mine ? 8 : 16));
      const u0 = 0.93 * HW - tw / 2;
      rrect(g, u0, 4, tw, 10, 2.5, mine ? '#2f3b52' : '#3a2a24');
      g.fillStyle = '#fff'; g.textBaseline = 'middle';
      if (!mine) { star(g, u0 + 6, 9, 2.6, CORAL, 1.3); }
      g.fillText(name.slice(0, 12), u0 + (mine ? 4 : 11), 9.4, tw - (mine ? 8 : 14));
      g.restore();
    },
    chair(g, p) {
      const { x, y } = p;
      ellipse(g, ...pt(x, y, 0), 7, 3.2, P.chairBase);
      line(g, pt(x, y, 1), pt(x, y, 11), P.chairBase, 2);
      box(g, x - 0.2, y - 0.2, 0.4, 0.4, 11, 3, P.chair, P.chairL, P.chairR);
    },
    chairback(g, p) {
      const { x, y } = p, f = p.face;
      const b = f === 'se' ? [x - 0.24, y - 0.2, 0.07, 0.4] : f === 'sw' ? [x - 0.2, y - 0.24, 0.4, 0.07]
        : f === 'ne' ? [x - 0.2, y + 0.17, 0.4, 0.07] : [x + 0.17, y - 0.2, 0.07, 0.4];
      const ht = f === 'ne' || f === 'nw' ? 9 : 13;
      box(g, b[0], b[1], b[2], b[3], 14, ht, P.chair, P.chairL, P.chairR);
    },
    printer(g, p) {
      const { x, y } = p;
      box(g, x + 0.15, y + 0.15, 0.7, 0.7, 0, 16, P.whiteL, P.whiteL, P.whiteR);
      box(g, x + 0.12, y + 0.12, 0.76, 0.76, 16, 10, P.white, P.whiteL, P.whiteR, 'rgba(0,0,0,.08)');
      box(g, x + 0.3, y + 0.2, 0.4, 0.3, 26, 1.2, '#fff', '#eee', '#ddd');
      ellipse(g, ...pt(x + 0.88, y + 0.7, 21), 1.3, 1, '#4ade80');
    },
    rack(g, p) {
      const { x, y } = p;
      box(g, x + 0.06, y + 0.06, 0.88, 0.88, 0, 90, P.rackTop, P.rackL, P.rackR, 'rgba(0,0,0,.25)');
      g.save(); rackFace(g, p);
      for (let v = 6; v < 86; v += 9) { g.fillStyle = P.rackLine; g.fillRect(2, v, 0.88 * HW - 4, 1); }
      g.fillStyle = 'rgba(0,0,0,.18)'; g.fillRect(0.88 * HW - 9, 4, 6, 80);
      g.restore();
    },
    kiosk(g, p) {
      const { x, y } = p;
      ellipse(g, ...pt(x + 0.5, y + 0.5), 13, 6, 'rgba(0,0,0,.08)');
      box(g, x + 0.28, y + 0.28, 0.44, 0.44, 0, 4, P.kioskL, P.kioskL, P.kioskR);
      box(g, x + 0.36, y + 0.36, 0.28, 0.28, 4, 46, P.kiosk, P.kioskL, P.kioskR);
      box(g, x + 0.26, y + 0.24, 0.5, 0.52, 50, 14, P.kioskTop, P.kioskL, P.kioskR, 'rgba(0,0,0,.12)');
      g.save(); onFaceY(g, x + 0.36, y + 0.64, 42); rrect(g, 1, 2, 0.28 * HW - 2, 30, 2, P.night ? '#a78bfa' : '#d9ccff'); g.restore();
    },
    wtable(g, p) {
      const { x, y } = p;
      box(g, x + 0.12, y + 0.15, 0.06, 0.7, 0, 18, P.deskL, P.deskL, P.deskR);
      box(g, x + 0.82, y + 0.15, 0.06, 0.7, 0, 18, P.deskL, P.deskL, P.deskR);
      box(g, x + 0.08, y + 0.1, 0.84, 0.8, 18, 3, P.white, P.whiteL, P.whiteR, 'rgba(0,0,0,.06)');
      box(g, x + 0.25, y + 0.3, 0.5, 0.3, 21, 1.5, '#c9ced6', '#b8bec8', '#aab0bb');
      poly(g, [pt(x + 0.25, y + 0.6, 22), pt(x + 0.75, y + 0.6, 22), pt(x + 0.75, y + 0.66, 38), pt(x + 0.25, y + 0.66, 38)], '#aeb5c0', 'rgba(0,0,0,.15)', 0.6);
      ellipse(g, ...pt(x + 0.5, y + 0.63, 30), 2, 1.6, '#e8eef6');
    },
    plant(g, p) { plant(g, p.x + 0.5, p.y + 0.5, p.kind, hash(p.id)); },
    sofa(g, p) {
      const { x, y } = p, se = p.face === 'se';
      // The armrest at the far end starts at the backrest's front, not inside it.
      if (se) {
        box(g, x + 0.12, y + 0.02, 0.76, 0.96, 0, 10, P.cushion, P.sofaL, P.sofaR);
        box(g, x + 0.04, y + 0.02, 0.24, 0.96, 10, 14, P.sofa, P.sofaL, P.sofaR);
        if (p.end === 'l') box(g, x + 0.28, y + 0.02, 0.6, 0.16, 10, 7, P.sofa, P.sofaL, P.sofaR);
      } else {
        box(g, x + 0.02, y + 0.12, 0.96, 0.76, 0, 10, P.cushion, P.sofaL, P.sofaR);
        box(g, x + 0.02, y + 0.04, 0.96, 0.24, 10, 14, P.sofa, P.sofaL, P.sofaR);
        if (p.end === 'l') box(g, x + 0.02, y + 0.28, 0.16, 0.6, 10, 7, P.sofa, P.sofaL, P.sofaR);
      }
    },
    sofaarm(g, p) {
      const { x, y } = p;
      if (p.face === 'se') box(g, x + 0.28, y + 0.84, 0.6, 0.16, 10, 7, P.sofa, P.sofaL, P.sofaR);
      else box(g, x + 0.84, y + 0.28, 0.16, 0.6, 10, 7, P.sofa, P.sofaL, P.sofaR);
    },
    ctable(g, p) {
      const { x, y } = p;
      box(g, x + 0.3, y + 0.3, 1.4, 1.4, 0, 8, P.tableR, P.tableL, P.tableR);
      box(g, x + 0.2, y + 0.2, 1.6, 1.6, 8, 3, P.table, P.tableL, P.tableR, 'rgba(0,0,0,.08)');
      // A magazine lies flat, so it goes first: a blue cover with its title,
      // a picture and a few lines of text. Then the rest, back to front.
      const mx = x + 0.7, my = y + 0.92, mz = 12.3;
      box(g, mx, my, 0.42, 0.3, 11, 1.3, '#7aa6e0', '#f4f1ea', '#e4dfd4');
      const onCover = (x0, y0, x1, y1, c) => poly(g, [pt(mx + x0, my + y0, mz), pt(mx + x1, my + y0, mz), pt(mx + x1, my + y1, mz), pt(mx + x0, my + y1, mz)], c);
      onCover(0.04, 0.03, 0.38, 0.08, 'rgba(255,255,255,.88)');
      onCover(0.04, 0.12, 0.2, 0.26, '#f2b84b');
      for (const ly of [0.14, 0.19, 0.24]) line(g, pt(mx + 0.24, my + ly, mz), pt(mx + 0.37, my + ly, mz), 'rgba(255,255,255,.75)', 0.7);
      box(g, x + 0.5, y + 0.45, 0.14, 0.14, 11, 6, '#fff', '#f1ece4', '#e1dbd0');
      plant(g, x + 1.35, y + 0.55, 'tiny', hash(p.id), 11);
      box(g, x + 1.25, y + 1.1, 0.14, 0.14, 11, 6, '#fff', '#f1ece4', '#e1dbd0');
    },
    counter(g, p) {
      const { x, y } = p;
      box(g, x + 0.04, y + 0.02, 0.62, 0.96, 0, 26, P.counter, P.counterL, P.counterR, 'rgba(0,0,0,.08)');
      if (p.machine) {
        box(g, x + 0.1, y + 0.18, 0.42, 0.56, 26, 26, P.machine, P.machineL, P.machine);
        box(g, x + 0.1, y + 0.18, 0.42, 0.56, 52, 4, shade(P.machine, 0.15), P.machineL, P.machine);
        ellipse(g, ...pt(x + 0.52, y + 0.4, 46), 1.4, 1.1, '#f87171');
        box(g, x + 0.46, y + 0.42, 0.1, 0.12, 26, 6, '#fff', '#f3eee6', '#e3ddd2');
      } else {
        for (let i = 0; i < 3; i++) box(g, x + 0.2 + i * 0.13, y + 0.3, 0.1, 0.1, 26, 6, '#fff', '#f3eee6', '#e3ddd2');
        plant(g, x + 0.36, y + 0.72, 'tiny', hash(p.id), 26);
      }
    },
    lamp(g, p) {
      const c = [p.x + 0.5, p.y + 0.5];
      ellipse(g, ...pt(c[0], c[1]), 6, 3, P.night ? '#5b6372' : '#9aa1ad');
      line(g, pt(c[0], c[1], 1), pt(c[0], c[1], 54), P.night ? '#6b7383' : '#8a919c', 1.6);
      const a = pt(c[0], c[1], 66);
      g.beginPath(); g.moveTo(a[0] - 6, a[1]); g.lineTo(a[0] + 6, a[1]); g.lineTo(a[0] + 9, a[1] + 13); g.lineTo(a[0] - 9, a[1] + 13); g.closePath();
      g.fillStyle = P.night ? '#ffd98a' : '#f6e7c8'; g.fill();
    },
    cooler(g, p) {
      const { x, y } = p;
      box(g, x + 0.28, y + 0.28, 0.44, 0.44, 0, 30, P.cooler, shade(P.cooler, -0.06), shade(P.cooler, -0.12), 'rgba(0,0,0,.08)');
      const c = pt(x + 0.5, y + 0.5, 30);
      g.fillStyle = P.water;
      g.beginPath(); g.ellipse(c[0], c[1] - 9, 7, 9, 0, 0, Math.PI * 2); g.fill();
      ellipse(g, c[0] - 2, c[1] - 12, 1.5, 3, 'rgba(255,255,255,.6)');
      box(g, x + 0.42, y + 0.68, 0.1, 0.06, 20, 3, '#5b8fd8', '#4a7bc2', '#3f6aa8');
    },
  };
  // rackFace: drawing on the side of a rack that faces the room.
  function rackFace(g, p) {
    if (p.front === 'y') onFaceY(g, p.x + 0.06, p.y + 0.94, 90);
    else { onFaceX(g, p.x + 0.94, p.y + 0.06, 90); g.transform(-1, 0, 0, 1, 0.88 * HW, 0); }
  }
  function table(g, p, top, l, r, lampOn) {
    const { x, y, w, d } = p;
    for (const [lx, ly] of [[0.18, 0.18], [w - 0.28, 0.18], [0.18, d - 0.28], [w - 0.28, d - 0.28]]) box(g, x + lx, y + ly, 0.1, 0.1, 0, 18, l, l, r);
    box(g, x + 0.1, y + 0.1, w - 0.2, d - 0.2, 18, 4, top, l, r, 'rgba(0,0,0,.08)');
    if (lampOn) { // open books (flat, so first) and a reading lamp
      for (const [bx, by] of [[1.2, 0.4], [1.25, 1.2]]) {
        poly(g, [pt(x + bx, y + by, 22.4), pt(x + bx + 0.42, y + by, 22.4), pt(x + bx + 0.42, y + by + 0.3, 22.4), pt(x + bx, y + by + 0.3, 22.4)], '#fffaf0', 'rgba(0,0,0,.12)', 0.5);
        line(g, pt(x + bx + 0.21, y + by, 22.6), pt(x + bx + 0.21, y + by + 0.3, 22.6), 'rgba(0,0,0,.18)', 0.6);
      }
      box(g, x + 0.9, y + 0.9, 0.2, 0.2, 22, 2, '#c9a46c', '#b8935b', '#a6834e');
      line(g, pt(x + 1, y + 1, 24), pt(x + 1, y + 1, 34), '#b8935b', 1.5);
      box(g, x + 0.78, y + 0.86, 0.44, 0.28, 33, 6, '#3f8f5a', '#367c4e', '#2e6b43');
    } else { // a laptop, notes and a cup on the meeting table
      box(g, x + 0.7, y + 0.6, 0.5, 0.34, 22, 1.4, '#c9ced6', '#b8bec8', '#aab0bb');
      poly(g, [pt(x + 0.7, y + 0.6, 23.4), pt(x + 1.2, y + 0.6, 23.4), pt(x + 1.2, y + 0.56, 38), pt(x + 0.7, y + 0.56, 38)], '#2d3442');
      for (const [nx, ny, c] of [[1.2, 1.25, '#fde68a'], [0.45, 1.3, '#bfdbfe']]) poly(g, [pt(x + nx, y + ny, 22.3), pt(x + nx + 0.3, y + ny, 22.3), pt(x + nx + 0.3, y + ny + 0.22, 22.3), pt(x + nx, y + ny + 0.22, 22.3)], c);
      box(g, x + 1.45, y + 0.5, 0.12, 0.12, 22, 5, '#fff', '#eee8de', '#ddd6ca');
    }
  }
  function books(g, wid, ht, seed) {
    const r = rng(seed);
    const rows = [[4, 18], [22, 37], [41, 56], [60, 74]];
    for (const [a, b] of rows) {
      rrect(g, 2, ht - b, wid - 4, b - a, 1, P.shelfIn);
      let u = 3.5;
      while (u < wid - 4.5) {
        const bw = 2.2 + r() * 2.4, bh = (b - a) * (0.6 + r() * 0.36);
        if (r() < 0.12) { u += bw * 0.8; continue; }
        g.fillStyle = P.books[(r() * P.books.length) | 0];
        g.fillRect(u, ht - a - bh, Math.min(bw, wid - 4 - u), bh);
        g.fillStyle = 'rgba(255,255,255,.22)';
        g.fillRect(u, ht - a - bh + 2, Math.min(bw, wid - 4 - u), 1);
        u += bw + 0.5;
      }
    }
  }
  function plant(g, cx, cy, kind, seed, z0 = 0) {
    const r = rng(seed);
    const small = kind === 'tiny' || kind === 'small';
    const pr = kind === 'tiny' ? 0.08 : small ? 0.14 : 0.2, ph = kind === 'tiny' ? 5 : small ? 10 : 15;
    if (!z0) ellipse(g, ...pt(cx, cy), pr * 48, pr * 22, 'rgba(0,0,0,.08)');
    box(g, cx - pr, cy - pr, pr * 2, pr * 2, z0, ph, P.soil, P.potL, P.potR);
    const top = pt(cx, cy, z0 + ph);
    if (kind === 'palm') {
      for (let i = 0; i < 7; i++) {
        const a = -Math.PI / 2 + (i - 3) * 0.45 + (r() - 0.5) * 0.2, len = 18 + r() * 8;
        g.beginPath(); g.moveTo(top[0], top[1] - 10);
        g.quadraticCurveTo(top[0] + Math.cos(a) * len * 0.7, top[1] - 10 + Math.sin(a) * len * 0.9 - 8, top[0] + Math.cos(a) * len, top[1] - 6 + Math.sin(a) * len * 0.5 + 6);
        g.strokeStyle = P.leaf[i % 3]; g.lineWidth = 4.2; g.lineCap = 'round'; g.stroke();
      }
      line(g, top, [top[0], top[1] - 11], shade(P.leaf[1], -0.2), 2.2);
    } else if (kind === 'tall') {
      for (let i = 0; i < 6; i++) {
        const dx = (i - 2.5) * 2.2, hgt = 26 + r() * 14;
        g.beginPath(); g.ellipse(top[0] + dx, top[1] - hgt / 2, 2.6, hgt / 2, (i - 2.5) * 0.12, 0, Math.PI * 2);
        g.fillStyle = P.leaf[i % 3]; g.fill();
      }
    } else {
      const n = kind === 'tiny' ? 4 : small ? 6 : 9, s = kind === 'tiny' ? 3.2 : small ? 5 : 7.5;
      for (let i = 0; i < n; i++) {
        const a = r() * Math.PI * 2, rr = r() * s * 0.9;
        ellipse(g, top[0] + Math.cos(a) * rr, top[1] - s - Math.abs(Math.sin(a)) * rr * 0.8, s * (0.7 + r() * 0.4), s * (0.6 + r() * 0.3), P.leaf[i % 3]);
      }
    }
  }
  function star(g, x, y, r, color, lw) { // Claude's spark: six rays
    g.save(); g.strokeStyle = color; g.lineWidth = lw || 1.2; g.lineCap = 'round';
    for (let i = 0; i < 6; i++) { const a = i * Math.PI / 3 + 0.26; g.beginPath(); g.moveTo(x, y); g.lineTo(x + Math.cos(a) * r, y + Math.sin(a) * r); g.stroke(); }
    g.restore();
  }

  // ---- the floor and the walls (one cached layer) --------------------------------------
  function paintBase(g, S) {
    // The slab: its shadow, then its two front faces.
    g.save();
    g.shadowColor = P.shadow; g.shadowBlur = 26 * S; g.shadowOffsetY = 12 * S;
    poly(g, [pt(0, 0), pt(GW, 0), pt(GW, GH, -SLAB), pt(0, GH, -SLAB)], P.slabL);
    g.restore();
    poly(g, [pt(0, GH), pt(GW, GH), pt(GW, GH, -SLAB), pt(0, GH, -SLAB)], P.slabL);
    poly(g, [pt(GW, 0), pt(GW, GH), pt(GW, GH, -SLAB), pt(GW, 0, -SLAB)], P.slabR);
    floorQuad(g, 0, 0, GW, GH, P.floor);
    for (let i = 0; i <= GW; i += 2) line(g, pt(i, 0), pt(i, GH), P.floorLine, 0.7);
    for (let j = 0; j <= GH; j += 2) line(g, pt(0, j), pt(GW, j), P.floorLine, 0.7);

    // Rooms: wood, tile, marble, raised floor, carpets, checkerboard, mint.
    const [lx0, ly0, lx1, ly1] = ZONES.library;
    floorQuad(g, lx0, ly0, lx1, ly1, P.wood);
    for (let y = ly0; y < ly1; y += 1 / 3) {
      line(g, pt(lx0, y), pt(lx1, y), P.woodLine, 0.8);
      const k = Math.round(y * 3);
      for (let x = lx0 + ((k * 0.37) % 1); x < lx1; x += 1.3) line(g, pt(x, y), pt(x, y + 1 / 3), P.woodLine, 0.8);
    }
    grid(g, ZONES.dev, P.dev, P.devLine, 1);
    grid(g, ZONES.elevator, P.marble, P.marbleLine, 1);
    grid(g, ZONES.server, P.raised, P.raisedLine, 1);
    for (let x = 14; x < 20; x++) for (let y = 4; y < 10; y++) if ((x + y) % 3 === 0) {
      for (let k = 0; k < 3; k++) line(g, pt(x + 0.3, y + 0.3 + k * 0.18), pt(x + 0.7, y + 0.3 + k * 0.18), P.raisedLine, 0.8);
    }
    carpet(g, ZONES.meeting, P.violet, P.violetEdge);
    const [cx0, cy0, cx1, cy1] = ZONES.lounge;
    for (let x = cx0; x < cx1; x++) for (let y = cy0; y < cy1; y++) floorQuad(g, x, y, x + 1, y + 1, (x + y) % 2 ? P.check1 : P.check2);
    carpet(g, ZONES.desk, P.blue, P.blueEdge);
    for (let x = 7.5; x < 13; x += 0.5) for (let y = 7.5; y < 11; y += 0.5) if ((x * 2 + y * 2) % 2 === 0) ellipse(g, ...pt(x, y), 0.9, 0.45, P.blueDot);
    const [sx0, sy0, sx1, sy1] = ZONES.search;
    for (let x = sx0; x < sx1; x++) for (let y = sy0; y < sy1; y++) floorQuad(g, x, y, x + 1, y + 1, (x + y) % 2 ? P.mint1 : P.mint2);
    grid(g, ZONES.search, null, P.mintLine, 1);
    // A round rug under the lounge's coffee table.
    g.save(); onFloor(g, 3, 13);
    ellipse(g, 0, 0, 52, 52, P.night ? 'rgba(255,255,255,.05)' : 'rgba(231,160,127,.18)', P.night ? 'rgba(255,255,255,.08)' : 'rgba(231,160,127,.35)', 2);
    g.restore();

    walls(g);
    floorLabel(g, 'desk', 10, 10.62);
    floorLabel(g, 'server', 17.05, 9.55);
    floorLabel(g, 'search', 14, 15.55);
    floorLabel(g, 'elevator', 17.1, 3.5);
  }
  function grid(g, z, fill, color, step) {
    const [x0, y0, x1, y1] = z;
    if (fill) floorQuad(g, x0, y0, x1, y1, fill);
    for (let x = x0; x <= x1; x += step) line(g, pt(x, y0), pt(x, y1), color, 0.8);
    for (let y = y0; y <= y1; y += step) line(g, pt(x0, y), pt(x1, y), color, 0.8);
  }
  function carpet(g, z, fill, edge) {
    const [x0, y0, x1, y1] = z;
    floorQuad(g, x0, y0, x1, y1, edge);
    floorQuad(g, x0 + 0.16, y0 + 0.16, x1 - 0.16, y1 - 0.16, fill);
  }
  function roomTitle(room) {
    return langOf() === 'ko' ? t('office_room_' + room) + ' · ' + ROOM_EN[room] : ROOM_EN[room];
  }
  function floorLabel(g, room, x, y) {
    g.save(); onFloor(g, x, y);
    g.font = '700 13px ' + FONT; g.fillStyle = P.label; g.textAlign = 'center'; g.textBaseline = 'middle';
    g.fillText(roomTitle(room), 0, 0);
    g.restore();
  }
  function sign(g, text, w) {
    g.font = '700 10px ' + FONT;
    const tw = Math.min(w, g.measureText(text).width + 16);
    rrect(g, -tw / 2, -9, tw, 18, 4, P.sign, P.signLine, 1);
    g.fillStyle = P.signText; g.textAlign = 'center'; g.textBaseline = 'middle';
    g.fillText(text, 0, 0.5, tw - 10);
  }
  function walls(g) {
    // Left wall (x=0) and right wall (y=0), each a thin block.
    box(g, -0.22, 0, 0.22, GH, 0, WALL, P.wallTop, P.wallL, P.wallL);
    box(g, 0, -0.22, GW, 0.22, 0, WALL, P.wallTop, P.wallR, P.wallR);
    box(g, -0.22, -0.22, 0.22, 0.22, 0, WALL, P.wallTop, P.wallL, P.wallR);
    poly(g, [pt(0, 0, 0), pt(GW, 0, 0), pt(GW, 0, 6), pt(0, 0, 6)], P.wallTrim);
    poly(g, [pt(0, 0, 0), pt(0, GH, 0), pt(0, GH, 6), pt(0, 0, 6)], shade(P.wallTrim, -0.04));

    // Windows over the dev room (right wall) and the lounge (left wall).
    for (const x0 of [6.5, 10.25]) { g.save(); onFaceY(g, x0, 0, 80); windowPane(g, 3.25 * HW, 50, hash('w' + x0)); g.restore(); }
    g.save(); onWallL(g, 13.9, 80); windowPane(g, 2.6 * HW, 50, 7); g.restore();
    // A painting in the lounge: hills and a sun.
    g.save(); onWallL(g, 15.7, 70);
    rrect(g, 0, 0, 0.9 * HW, 30, 2, P.frame, P.frameLine, 1);
    g.fillStyle = P.night ? '#25345a' : '#fdf0d5'; g.fillRect(3, 3, 0.9 * HW - 6, 24);
    ellipse(g, 19, 11, 4, 4, P.night ? '#f5e7a1' : '#f4a261');
    g.fillStyle = P.night ? '#3d6b55' : '#8fcf9b';
    g.beginPath(); g.moveTo(3, 27); g.quadraticCurveTo(10, 12, 18, 22); g.quadraticCurveTo(22, 16, 0.9 * HW - 3, 20); g.lineTo(0.9 * HW - 3, 27); g.closePath(); g.fill();
    g.restore();
    // The whiteboard in the meeting room (its todos are drawn live).
    g.save(); onWallL(g, 10.45, 76);
    rrect(g, 0, 0, 3.9 * HW, 52, 3, P.boardFrame);
    rrect(g, 3, 3, 3.9 * HW - 6, 44, 2, P.board);
    g.fillStyle = shade(P.boardFrame, -0.15); g.fillRect(14, 50, 3.9 * HW - 28, 3);
    for (const [c, u] of [['#ef4444', 30], ['#3b82f6', 38], ['#22c55e', 46]]) { g.fillStyle = c; g.fillRect(u, 48.6, 6, 1.8); }
    g.restore();
    // The elevator: frame, call buttons and the panel over the doors.
    g.save(); onFaceY(g, 15.75, 0, 76);
    rrect(g, 0, 0, 2.5 * HW, 76, 3, P.liftFrame);
    g.fillStyle = shade(P.liftFrame, -0.12); g.fillRect(6, 9, 2.5 * HW - 12, 67);
    rrect(g, 2.5 * HW / 2 - 15, -15, 30, 13, 3, P.panel);
    rrect(g, 2.5 * HW + 5, 34, 7, 14, 2, P.liftFrame);
    ellipse(g, 2.5 * HW + 8.5, 38, 1.8, 1.8, '#fbbf24');
    ellipse(g, 2.5 * HW + 8.5, 44, 1.8, 1.8, P.night ? '#475569' : '#e2e8f0');
    g.restore();
    // The clock's face (its hands are drawn live).
    g.save(); onFaceY(g, 14.75, 0, 66);
    ellipse(g, 0, 0, 10, 10, P.night ? '#e9e4da' : '#fff', P.night ? '#8b93a3' : '#7b8494', 1.6);
    for (let i = 0; i < 12; i++) { const a = i * Math.PI / 6; line(g, [Math.cos(a) * 7.5, Math.sin(a) * 7.5], [Math.cos(a) * 8.8, Math.sin(a) * 8.8], '#7b8494', 0.8); }
    g.restore();

    // Signs for the rooms along the walls.
    for (const [room, x] of [['library', 3], ['dev', 10]]) { g.save(); onFaceY(g, x, 0, 92); sign(g, roomTitle(room), 150); g.restore(); }
    for (const [room, y] of [['meeting', 8.5], ['lounge', 12.6]]) { g.save(); onWallL(g, y, 92); sign(g, roomTitle(room), 150); g.restore(); }
  }
  function windowPane(g, w, ht, seed) {
    rrect(g, 0, 0, w, ht, 2, P.frame, P.frameLine, 1);
    const sky = g.createLinearGradient(0, 3, 0, ht - 3);
    sky.addColorStop(0, P.sky1); sky.addColorStop(1, P.sky2);
    g.fillStyle = sky; g.fillRect(3, 3, w - 6, ht - 6);
    const r = rng(seed);
    g.save(); g.beginPath(); g.rect(3, 3, w - 6, ht - 6); g.clip();
    if (P.night) {
      for (let i = 0; i < 16; i++) ellipse(g, 4 + r() * (w - 8), 4 + r() * (ht * 0.7), 0.6, 0.6, 'rgba(255,255,255,' + (0.4 + r() * 0.5) + ')');
      if (seed === 7) { ellipse(g, w * 0.7, 14, 6, 6, '#f8efc4'); ellipse(g, w * 0.7 + 3, 12, 5.4, 5.4, P.sky1); }
      // City lights far away.
      for (let i = 0; i < 9; i++) { const bw = 6 + r() * 10, bh = 6 + r() * 14, bx = r() * w; g.fillStyle = '#18213d'; g.fillRect(bx, ht - 3 - bh, bw, bh); g.fillStyle = 'rgba(255,214,140,.7)'; g.fillRect(bx + 2, ht - bh, 1.4, 1.4); }
    } else {
      for (let i = 0; i < 3; i++) {
        const cx = 8 + r() * (w - 16), cy = 9 + r() * (ht * 0.45);
        ellipse(g, cx, cy, 7, 3.4, P.cloud); ellipse(g, cx + 5, cy - 2, 5, 3.4, P.cloud); ellipse(g, cx - 5, cy + 0.5, 4.5, 2.6, P.cloud);
      }
      for (let i = 0; i < 7; i++) { const bw = 6 + r() * 10, bh = 5 + r() * 12; g.fillStyle = 'rgba(150,180,210,.45)'; g.fillRect(r() * w, ht - 3 - bh, bw, bh); }
    }
    g.restore();
    g.fillStyle = P.frame;
    for (let u = w / 3; u < w - 2; u += w / 3) g.fillRect(u - 1, 3, 2, ht - 6);
    g.fillRect(3, ht * 0.55, w - 6, 2);
    g.fillStyle = shade(P.frame, -0.08); g.fillRect(-2, ht - 1, w + 4, 3);
  }

  // ---- partitions ----------------------------------------------------------------------
  function drawSeg(g, s) {
    const th = 0.06;
    const [x0, y0, w, d] = s.axis === 'x' ? [s.at - th, s.i, th * 2, 1] : [s.i, s.at - th, 1, th * 2];
    if (s.glass) {
      box(g, x0, y0, w, d, 0, 6, P.low, P.lowL, P.lowR);
      box(g, x0, y0, w, d, 6, 38, P.glass, P.glass, P.glass, P.glassLine);
      box(g, x0, y0, w, d, 44, 3, P.post, P.post, P.post);
    } else {
      box(g, x0, y0, w, d, 0, 22, P.lowR, P.lowL, P.lowR);
      box(g, x0, y0, w, d, 22, 2.5, P.low, P.low, P.low);
      box(g, x0, y0, w, d, 24.5, 12, P.glass, P.glass, P.glass, P.glassLine);
    }
    // Posts where the wall starts or stops.
    for (const k of [0, 1]) {
      if (!(k ? s.p1 : s.p0)) continue;
      const [px, py] = s.axis === 'x' ? [s.at - 0.07, s.i + k - 0.07] : [s.i + k - 0.07, s.at - 0.07];
      box(g, px, py, 0.14, 0.14, 0, s.glass ? 47 : 37, P.post, shade(P.post, -0.05), shade(P.post, -0.12));
    }
  }

  // ---- characters ------------------------------------------------------------------------
  function drawPerson(g, a, now) {
    const L = a.look, tt = now / 1000;
    const [wx, wy] = pt(a.x, a.y);
    const back = a.face === 'ne' || a.face === 'nw';
    const flip = a.face === 'sw' || a.face === 'nw';
    const walking = a.moving;
    const sit = !walking && a.body === 'sit';
    const arms = walking ? 'walk' : a.kind === 'me' ? (a.flash && a.flash.until > now ? 'type' : 'rest') : a.arms;
    g.save();
    g.translate(wx, wy);
    g.globalAlpha = a.alpha;
    // The ring and the shadow lie on what one stands or sits on: a sofa's
    // cushion is higher than the floor.
    const seat = sit && a.sofa ? -10 : 0;
    if (L.claude) ellipse(g, 0, seat, 14, 6, null, L.color, 1.4);
    ellipse(g, 0, seat + 0.5, 10.5, 4.4, 'rgba(0,0,0,.16)');
    if (flip) g.scale(-1, 1);
    const ph = a.phase;
    const bob = walking ? -Math.abs(Math.sin(ph)) * 1.8 : Math.sin(tt * 2.1 + L.seed) * 0.4;
    const hip = (sit ? -14 : -10) + bob;
    const top = hip - 14;
    const cloth = L.color, clothD = shade(L.color, -0.22), clothL = shade(L.color, 0.18);

    // Legs.
    if (sit) {
      for (const lx of [-3.2, 2.6]) { rrect(g, lx - 1, hip - 1, 6.5, 4.6, 2, P.pants); ellipse(g, lx + 5.5, hip + 2.6, 2.4, 2, P.shoe); }
    } else {
      for (const i of [0, 1]) {
        const s = walking ? Math.sin(ph + i * Math.PI) : 0;
        const lx = (i ? 2.9 : -2.9) + s * 2.4, lift = walking ? Math.max(0, -Math.cos(ph + i * Math.PI)) * 1.8 : 0;
        rrect(g, lx - 2.1, hip, 4.2, -hip - lift - 1, 1.8, P.pants);
        ellipse(g, lx + 0.6, -lift - 1, 2.8, 1.8, P.shoe);
      }
    }
    // Arms: the far arm before the body, the near one after.
    const hands = armPose(arms, hip, top, tt, ph, L);
    const arm = (sx, h2, col) => {
      g.beginPath(); g.moveTo(sx, top + 4); g.quadraticCurveTo((sx + h2[0]) / 2 + (h2[0] > sx ? -1 : 1), (top + 4 + h2[1]) / 2, h2[0], h2[1]);
      g.strokeStyle = col; g.lineWidth = 4; g.lineCap = 'round'; g.stroke();
      ellipse(g, h2[0], h2[1], 2.3, 2.3, P.skin);
    };
    const forward = arms === 'type' || arms === 'read' || arms === 'rest';
    if (!back || !forward) arm(-5.5, hands[0], clothD);
    if (back && forward) arm(5.5, hands[1], clothD);
    // Body.
    rrect(g, -7.6, top, 15.2, hip - top + 1.5, [6, 6, 4, 4], cloth);
    g.save(); g.beginPath(); g.roundRect(-7.6, top, 15.2, hip - top + 1.5, [6, 6, 4, 4]); g.clip();
    g.fillStyle = 'rgba(0,0,0,.10)'; g.fillRect(3, top, 6, hip - top + 2);
    g.fillStyle = clothL; g.fillRect(-7.6, top, 2.4, hip - top);
    g.restore();
    if (!back) {
      if (L.me) { g.strokeStyle = clothD; g.lineWidth = 0.9; g.beginPath(); g.moveTo(-3.5, hip - 3); g.lineTo(4.5, hip - 3); g.stroke(); line(g, [0.5, top + 2], [0, top + 7], '#fff', 0.8); line(g, [3, top + 2], [3.4, top + 7], '#fff', 0.8); }
      else {
        g.fillStyle = '#fff';
        g.beginPath(); g.moveTo(-2.4, top + 0.4); g.lineTo(1.5, top + 4.6); g.lineTo(4.8, top + 0.4); g.closePath(); g.fill();
        if (L.claude) star(g, 3.6, top + 8.5, 2.7, '#fff', 1.3);
      }
    }
    if (!back || !forward) arm(5.5, hands[1], cloth);
    // Things in the hands.
    const hb = hands[1];
    if (arms === 'read' && !back) {
      const bc = L.acc === 'book' ? '#1f9d8f' : P.books[L.seed % P.books.length];
      rrect(g, hb[0] - 9, hb[1] - 7, 12, 8.5, 1.2, bc);
      rrect(g, hb[0] - 8, hb[1] - 6.5, 10, 6.6, 1, '#fffaf0');
      line(g, [hb[0] - 3, hb[1] - 6.5], [hb[0] - 3, hb[1] + 0.1], 'rgba(0,0,0,.25)', 0.6);
    }
    if (arms === 'phone') rrect(g, hb[0] - 1.5, hb[1] - 4.5, 3.4, 6.4, 1, '#2b2f38');
    if (arms === 'coffee') {
      rrect(g, hb[0] - 1.5, hb[1] - 6, 5, 6, 1.2, '#fff');
      g.strokeStyle = '#fff'; g.lineWidth = 1; g.beginPath(); g.arc(hb[0] + 4, hb[1] - 3, 1.6, -1.4, 1.4); g.stroke();
      if (!a.moving) for (let i = 0; i < 2; i++) {
        const k = (tt * 0.8 + i * 0.5) % 1;
        g.globalAlpha = a.alpha * (1 - k) * 0.6;
        ellipse(g, hb[0] + 1 + Math.sin(tt * 3 + i) * 1.2, hb[1] - 8 - k * 8, 1.2, 1.6, P.night ? '#cbd5e1' : '#b8c2cf');
        g.globalAlpha = a.alpha;
      }
    }
    // Head.
    const hx = back ? 0 : 1.2, hy = top - 9.4, hr = 11.2;
    if (L.acc === 'hood') ellipse(g, hx - 2.5, hy + 5, 11.5, 9, clothD);
    if (back) {
      ellipse(g, hx, hy + 9, 3.6, 2.6, P.skin);
      ellipse(g, hx, hy, hr, hr, L.hair);
      ellipse(g, hx - 3, hy - 4, 5, 3, 'rgba(255,255,255,.08)');
    } else {
      ellipse(g, hx - 0.8, hy - 0.8, hr + 0.5, hr + 0.4, L.hair);
      ellipse(g, hx + 1.8, hy + 2.6, 9.2, 8.3, P.skin);
      // Bangs.
      g.fillStyle = L.hair;
      g.beginPath();
      g.moveTo(hx - 8, hy - 1);
      g.quadraticCurveTo(hx - 3, hy + 2.2, hx - 0.5, hy - 2.6);
      g.quadraticCurveTo(hx + 3, hy + 1.4, hx + 6, hy - 2.2);
      g.quadraticCurveTo(hx + 9, hy + 0.5, hx + 11, hy - 0.5);
      g.lineTo(hx + 10, hy - 8); g.lineTo(hx - 8, hy - 8); g.closePath(); g.fill();
      // Face.
      const blink = (tt + L.seed % 7) % 4.2 < 0.13;
      for (const ex of [hx + 1.4, hx + 7.4]) {
        if (blink) line(g, [ex - 1.6, hy + 3], [ex + 1.6, hy + 3], P.eye, 1.1);
        else { ellipse(g, ex, hy + 2.8, 1.5, 2.2, P.eye); ellipse(g, ex + 0.5, hy + 2, 0.55, 0.6, '#fff'); }
      }
      ellipse(g, hx - 1.2, hy + 6.4, 2.2, 1.2, P.blush);
      ellipse(g, hx + 10, hy + 6.4, 2, 1.2, P.blush);
      g.strokeStyle = P.eye; g.lineWidth = 0.9; g.beginPath();
      if (a.mood === 'oops') { g.moveTo(hx + 2.8, hy + 7.6); g.quadraticCurveTo(hx + 4.4, hy + 6.4, hx + 6, hy + 7.6); }
      else g.arc(hx + 4.4, hy + 5.8, 1.6, 0.15 * Math.PI, 0.85 * Math.PI);
      g.stroke();
    }
    accessory(g, L, hx, hy, hr, back, tt);
    // A drop of sweat when something failed.
    if (a.mood === 'oops' && !back) {
      g.fillStyle = '#7cc4ff';
      g.beginPath(); g.moveTo(hx + 12, hy - 7); g.quadraticCurveTo(hx + 15, hy - 2, hx + 12.2, hy - 1); g.quadraticCurveTo(hx + 9.6, hy - 2, hx + 12, hy - 7); g.fill();
    }
    g.restore();
  }
  // armPose: where the two hands are (far, near) for each pose.
  function armPose(arms, hip, top, tt, ph, L) {
    const down = [[-8.4, hip - 0.5], [8.6, hip - 0.5]];
    switch (arms) {
      case 'walk': { const s = Math.sin(ph); return [[-7.6 + s * 3, hip - 0.5], [7.8 - s * 3, hip - 0.5]]; }
      case 'type': return [[3.5, hip - 4 + Math.sin(tt * 19) * 1.1], [10, hip - 4.5 + Math.cos(tt * 17) * 1.1]];
      case 'rest': return [[3.5, hip - 4], [10, hip - 4.5]];
      case 'read': return [[5, hip - 6], [10.5, hip - 6.5]];
      case 'think': return [down[0], [7.5, top - 3]];
      case 'phone': return [down[0], [9.6, top - 8]];
      case 'wave': return [down[0], [12.5 + Math.sin(tt * 13) * 2, top - 13]];
      case 'ask': return [down[0], [9.5, top - 17]];
      case 'board': return [down[0], [15, top - 4]];
      case 'talk': return [down[0], [11.5, hip - 7 + Math.sin(tt * 5) * 2.2]];
      case 'coffee': return [down[0], [9.5, hip - 6]];
      default: return L.acc === 'book' ? [[5, hip - 6], [10.5, hip - 6.5]] : down;
    }
  }
  function accessory(g, L, hx, hy, hr, back, tt) {
    switch (L.acc) {
      case 'claude': // a curl of hair on top
        g.strokeStyle = L.hair; g.lineWidth = 2.2; g.lineCap = 'round';
        g.beginPath(); g.moveTo(hx - 1, hy - hr + 0.5); g.quadraticCurveTo(hx + 1, hy - hr - 8, hx + 5, hy - hr - 5); g.stroke();
        break;
      case 'cap': {
        const c = '#2f8a52';
        g.fillStyle = c; g.beginPath(); g.arc(hx - 0.5, hy - 2.2, hr + 1, Math.PI, Math.PI * 2); g.closePath(); g.fill();
        if (!back) ellipse(g, hx + 8.5, hy - 2.6, 7.5, 2.3, shade(c, -0.15));
        ellipse(g, hx - 0.5, hy - hr - 1.8, 1.6, 1.2, shade(c, 0.2));
        break;
      }
      case 'hardhat': {
        g.fillStyle = '#f2c230'; g.beginPath(); g.arc(hx, hy - 2.4, hr + 1.4, Math.PI, Math.PI * 2); g.closePath(); g.fill();
        ellipse(g, hx, hy - 2.4, hr + 4, 2.4, '#e0ae1e');
        g.fillStyle = 'rgba(255,255,255,.35)'; g.fillRect(hx - 1.2, hy - hr - 3.8, 2.4, hr);
        break;
      }
      case 'beanie': {
        g.fillStyle = '#f4d35e'; g.beginPath(); g.arc(hx, hy - 1.8, hr + 0.8, Math.PI, Math.PI * 2); g.closePath(); g.fill();
        rrect(g, hx - hr - 1, hy - 4.2, hr * 2 + 2, 4.4, 2, '#e9b949');
        ellipse(g, hx, hy - hr - 3.6, 3, 3, '#fff3c4');
        break;
      }
      case 'glasses':
        if (back) { line(g, [hx - hr, hy + 1], [hx + hr, hy + 1], '#3b3355', 1); break; }
        g.strokeStyle = '#3b3355'; g.lineWidth = 1.1;
        for (const ex of [hx + 1.4, hx + 7.4]) { g.beginPath(); g.arc(ex, hy + 2.8, 2.9, 0, Math.PI * 2); g.stroke(); }
        line(g, [hx + 4.3, hy + 2.6], [hx + 4.5, hy + 2.6], '#3b3355', 1.1);
        line(g, [hx - 1.5, hy + 2.4], [hx - 6, hy + 1], '#3b3355', 1);
        break;
      case 'headset':
        g.strokeStyle = '#2f3542'; g.lineWidth = 2.2;
        g.beginPath(); g.arc(hx, hy, hr + 0.8, Math.PI * 1.05, Math.PI * 1.95); g.stroke();
        ellipse(g, back ? hx + hr - 1 : hx - 6.5, hy + 1.5, 3.2, 3.6, '#2f3542');
        if (!back) { g.lineWidth = 1.2; g.beginPath(); g.moveTo(hx - 5, hy + 4); g.quadraticCurveTo(hx - 2, hy + 9.5, hx + 3, hy + 8.4); g.stroke(); ellipse(g, hx + 3.4, hy + 8.4, 1.2, 1.2, '#2f3542'); }
        break;
      case 'tag': // other agents: a little badge on the head
        ellipse(g, hx + (back ? 0 : -1), hy - hr - 1.2, 3, 2, shade(L.color, -0.1));
        break;
      default: break;
    }
  }

  // ---- actors --------------------------------------------------------------------------
  // An actor plays the events the server sent, one step at a time, waiting a
  // little in each room so even a 5 ms Read is seen; then it settles where
  // the snapshot says it is.
  function makeActor(s, how) {
    const a = {
      id: s.id, kind: s.kind, snap: s, look: lookFor(s), queue: [], path: [], cur: null, settled: '',
      x: LIFT_DOOR[0], y: LIFT_DOOR[1], face: 'sw', body: 'stand', arms: 'down', mood: '', phase: 0, moving: false,
      alpha: 1, fade: 0, wait: 0, spot: null, flash: null, gone: false, bornAt: performance.now(), dom: null,
    };
    actors.set(a.id, a);
    if (how === 'place') {
      const sp = claim(a, roomFor(a, s.room));
      a.x = sp.px; a.y = sp.py; a.face = sp.face; a.spot = sp;
      a.cur = { settle: true, act: actOf(s.state), tool: s.tool, detail: s.detail, git: s.git, room: s.room, arrived: true, until: 0 };
      a.settled = settleKey(s);
      pose(a);
    } else {
      // Newcomers come up in the elevator car and step out once the doors
      // open; several at once leave the car one after another. One who comes
      // while the doors are open fades in.
      const n = carMates(a), now = performance.now();
      a.x = carSlot(a); a.y = CAR_Y; a.car = 'in'; a.carAt = now + 450;
      if (lift.open > 0.02) { a.alpha = 0; a.fade = 1; }
      if (now >= lift.arrowUntil || lift.arrow === '▲') { lift.arrow = '▲'; lift.arrowUntil = Math.max(lift.arrowUntil, a.carAt); } // a ride's arrow stays
      a.queue.push({ act: 'arrive', flash: ['note', t('office_b_hello'), 2.6], dwell: 850 + 400 * n });
    }
    return a;
  }
  // inCar: someone in the elevator car, or stepping in.
  const inCar = (o) => o.car === 'in' || o.car === 'enter' || o.car === 'ride' || (o.car === 'out' && o.y < 0);
  // carMates: how many others are in the elevator car.
  function carMates(a) {
    let n = 0;
    for (const o of actors.values()) if (o !== a && inCar(o)) n++;
    return n;
  }
  // carSlot: the first place in the car nobody stands on or walks to.
  function carSlot(a) {
    const taken = new Set();
    for (const o of actors.values()) {
      if (o === a || !inCar(o)) continue;
      const p = o.car === 'enter' && o.path.length ? o.path[o.path.length - 1] : [o.x, o.y];
      let best = 0;
      CAR_X.forEach((x, i) => { if (Math.abs(x - p[0]) < Math.abs(CAR_X[best] - p[0])) best = i; });
      taken.add(best);
    }
    const i = CAR_X.findIndex((_, k) => !taken.has(k));
    return CAR_X[i >= 0 ? i : carMates(a) % CAR_X.length];
  }
  function dropActor(a) {
    release(a);
    if (a.dom) a.dom.wrap.remove();
    actors.delete(a.id);
  }
  const settleKey = (s) => s.room + '|' + s.state + '|' + (s.tool || '') + '|' + (s.detail || '');
  function actOf(state) {
    return { working: 'tool', thinking: 'think', talking: 'say', asking: 'ask', error: 'fail', idle: 'idle', reporting: 'report',
      leaving: 'leave', arriving: 'arrive' }[state] || 'idle';
  }
  // roomFor: the main actor's own desk is the "seat"; subagents in the seat
  // room stand by it, to report.
  function roomFor(a, room) { return ROOMS.includes(room) ? room : 'seat'; }

  function claim(a, room) {
    if (a.kind === 'me') return SEAT_ME;
    // Claude's own desk is nobody else's: the spot it leaves for it is free again.
    if (a.kind === 'claude' && room === 'seat' && !a.ghost) { release(a); return SEAT_MAIN; }
    if (a.spot && a.spot.room === room && (a.spot.by === a.id || a.spot.loose)) return a.spot;
    release(a);
    const list = (SPOTS[room] || []).filter((s) => !s.by || s.by === a.id);
    let pick = null;
    if (list.length) {
      // Claude likes the sofa in the lounge; everyone else takes a spot of
      // their own (the same one each time, when it is free).
      const pref = a.kind === 'claude' && room === 'lounge' ? list.filter((s) => s.sofa) : [];
      pick = pref[0] || list[a.kind === 'claude' ? 0 : hash(a.id + room) % list.length];
      pick.by = a.id;
      return pick;
    }
    // Every spot is taken: stand somewhere free in the room.
    const z = ZONES[room] || ZONES.seat;
    const r = rng(hash(a.id + room + Date.now()));
    for (let k = 0; k < 30; k++) {
      const x = z[0] + ((r() * (z[2] - z[0])) | 0), y = z[1] + ((r() * (z[3] - z[1])) | 0);
      if (!freeTile(x, y) || SPOT_TILES.has(x + ',' + y)) continue;
      const cx = (z[0] + z[2]) / 2, cy = (z[1] + z[3]) / 2;
      return spot(room, x, y, faceOf(cx - x - 0.5, cy - y - 0.5), 'stand', { loose: true });
    }
    return spot(room, z[0], z[1], 'se', 'stand', { loose: true });
  }
  function release(a) { if (a.spot && a.spot.by === a.id) a.spot.by = null; }
  function faceOf(dx, dy) {
    const sx = dx - dy, sy = dx + dy;
    if (sy >= 0) return sx >= 0 ? 'se' : 'sw';
    return sx >= 0 ? 'ne' : 'nw';
  }
  function goTo(a, room, door) {
    if (door) release(a);
    const sp = door ? DOOR : claim(a, room);
    a.spot = sp;
    if (Math.hypot(a.x - sp.px, a.y - sp.py) < 0.05) { a.path = []; return; }
    const path = findPath(Math.floor(a.x), Math.floor(a.y), sp.x, sp.y);
    a.path = (path || []).concat([[sp.px, sp.py]]);
    // Skip the first tile center when it lies behind us.
    if (a.path.length > 1 && Math.hypot(a.path[0][0] - a.x, a.path[0][1] - a.y) < 0.75) {
      const [p0, p1] = a.path;
      if ((p0[0] - a.x) * (p1[0] - p0[0]) + (p0[1] - a.y) * (p1[1] - p0[1]) < 0) a.path.shift();
    }
    // Out of the elevator car first, straight through the doors.
    if (a.y < 0) a.path.unshift([clamp(a.x, 16.5, 17.5), 0.45]);
  }
  function flash(a, kind, text, secs, now) {
    a.flash = { kind, text: text || '', until: now + secs * 1000 };
  }
  // stepOf turns an event into what its actor does.
  function stepOf(ev, a) {
    const d = ev.detail || '';
    const main = a.kind === 'claude';
    switch (ev.kind) {
      case 'tool': return { act: 'tool', room: ev.room || toolRoom(ev.tool), tool: ev.tool, detail: d, git: isGit(ev) };
      case 'think': return { act: 'think', room: main ? 'seat' : null, soft: true, detail: d };
      case 'say': return { act: 'say', room: main ? 'seat' : null, soft: true, flash: ['say', d, 6.5] };
      case 'fail': return { act: 'fail', flash: ['fail', d, 5], dwell: 1400, mood: 'oops' };
      case 'error': return { act: 'fail', flash: ['fail', d, 6], dwell: 1600, mood: 'oops' };
      case 'deny': return { act: 'deny', flash: ['deny', d || t('office_b_denied'), 3.5], dwell: 1000 };
      case 'compact': return { act: 'note', flash: ['note', '🗜 ' + t('office_b_compact'), 3.5], dwell: 900 };
      // Claude waits at its desk while its subagents work (they report there).
      case 'idle': return { act: 'idle', room: main ? (ev.room === 'seat' ? 'seat' : 'lounge') : null };
      case 'hire': return null; // the newcomer arrives on its own
      case 'report': return { act: 'report', room: 'seat', arriveFlash: ['report', '📋 ' + t('office_b_report'), 3.5], dwell: 2400 };
      case 'leave': return { act: 'leave', room: 'elevator', flash: ['leave', t('office_b_leave'), 8] };
      case 'ask': return { act: 'ask', room: 'desk', detail: d };
      case 'shift': return { act: 'note', flash: ['note', '🔁 ' + t('office_b_shift', { d }), 4.5], dwell: 600 };
      case 'clear': return { act: 'note', flash: ['note', '🧹 ' + t('office_b_clear'), 3.5], dwell: 600 };
      case 'end': return { act: 'note', flash: ['note', '🌙 ' + t('office_b_end'), 5], dwell: 600 };
      default: return null;
    }
  }
  function begin(a, s, now) {
    a.cur = s; s.arrived = false; a.settled = '';
    a.fast = a.queue.length > 3;
    if (s.flash) flash(a, s.flash[0], s.flash[1], s.flash[2], now);
    if (s.mood) a.mood = s.mood;
    else if (s.act === 'tool' || s.act === 'idle') a.mood = '';
    // A passing thought or remark does not send Claude back to its desk
    // when more work is already queued.
    if (s.soft && a.queue.length) s.room = null;
    if (s.act === 'leave') { a.leaving = true; goTo(a, 'elevator', true); if (!a.path.length) arrive(a, now); }
    else if (s.room && !(a.kind === 'me')) { goTo(a, roomFor(a, s.room)); if (!a.path.length) arrive(a, now); }
    else arrive(a, now);
  }
  function arrive(a, now) {
    const s = a.cur;
    if (!s || s.arrived) return;
    s.arrived = true;
    if (a.spot && !a.moving) a.face = a.spot.face;
    if (s.arriveFlash) flash(a, s.arriveFlash[0], s.arriveFlash[1], s.arriveFlash[2], now);
    let dwell = s.dwell ?? (a.fast ? 320 : 850);
    if (s.soft && a.queue.length) dwell = 260;
    if (s.settle) dwell = 0;
    if (s.act === 'leave') dwell = 250;
    s.until = now + dwell;
    pose(a);
  }
  function settle(a, now) {
    const s = a.snap;
    if (!s) return;
    const key = settleKey(s);
    if (a.settled === key) return;
    a.settled = key;
    a.fast = false;
    if (s.state !== 'error' && s.state !== 'working') a.mood = '';
    a.cur = { settle: true, act: actOf(s.state), tool: s.tool, detail: s.detail, git: s.git, room: s.room, arrived: false, until: 0 };
    if (a.kind === 'me') { arrive(a, now); return; }
    if (s.state === 'leaving') { a.leaving = true; goTo(a, 'elevator', true); }
    else { a.leaving = false; goTo(a, roomFor(a, s.room)); } // back after all (a handover can return)
    if (!a.path.length) arrive(a, now);
  }
  // pose decides how an actor stands, sits and what its hands do.
  function pose(a) {
    const s = a.cur || {};
    const sp = a.spot;
    const at = sp && Math.hypot(a.x - sp.px, a.y - sp.py) < 0.05;
    a.body = at && sp.pose === 'sit' ? 'sit' : 'stand';
    a.sofa = !!(at && sp.sofa); // what one sits on, not where one is going
    let arms = 'down';
    const room = sp ? sp.room : '';
    switch (s.act) {
      case 'tool':
        if (isAgentTool(s.tool)) arms = 'phone';
        else if (room === 'library' || room === 'meeting') arms = sp && sp.board ? 'board' : 'read';
        else if (room === 'desk') arms = 'ask';
        else if (room === 'lounge') arms = 'coffee';
        else if (room === 'elevator') arms = 'down';
        else arms = 'type';
        break;
      case 'think': arms = 'think'; break;
      case 'say': arms = 'talk'; break;
      case 'ask': arms = 'ask'; break;
      case 'fail': arms = 'think'; break;
      case 'report': arms = 'read'; break;
      case 'leave': arms = 'wave'; break;
      case 'idle': arms = room === 'lounge' ? 'coffee' : 'down'; break;
      default: arms = 'down';
    }
    a.arms = arms;
  }
  function walk(a, dt) {
    let rem = (a.fast ? 6.2 : a.leaving || (a.cur && a.cur.act === 'arrive') ? 2.8 : 3.6) * dt;
    while (rem > 0 && a.path.length) {
      const [nx, ny] = a.path[0];
      const dx = nx - a.x, dy = ny - a.y, d = Math.hypot(dx, dy);
      if (d < 1e-4) { a.path.shift(); continue; }
      a.face = faceOf(dx, dy);
      if (d <= rem) { a.x = nx; a.y = ny; a.path.shift(); rem -= d; } else { a.x += (dx / d) * rem; a.y += (dy / d) * rem; rem = 0; }
    }
    a.phase += dt * (a.fast ? 17 : 11);
    a.moving = a.path.length > 0;
  }
  function tick(a, dt, now) {
    if (a.wait > 0) { a.wait -= dt; return; }
    if (a.fade) {
      a.alpha = clamp(a.alpha + a.fade * dt * 2.4, 0, 1);
      if (a.alpha >= 1 && a.fade > 0) a.fade = 0;
      if (a.alpha <= 0 && a.fade < 0) { a.gone = true; return; }
    }
    // The elevator car: newcomers wait in it for the doors to open; people
    // leaving step in, turn round and are gone once the doors close.
    if (a.car === 'in') {
      if (lift.open < 0.9 || now < a.carAt) return;
      a.car = 'out';
    }
    if (a.car === 'enter') {
      walk(a, dt);
      if (!a.path.length) { a.moving = false; a.car = 'ride'; a.face = 'sw'; a.carAt = now; }
      return;
    }
    if (a.car === 'ride') {
      if (lift.open < 0.02 && now - a.carAt > 900) a.gone = true;
      else if (now - a.carAt > 8000 && !a.fade) a.fade = -1; // the doors are kept busy: just go
      return;
    }
    if (a.car === 'out' && a.y >= 0 && (a.y > 1.2 || !a.path.length)) a.car = null;
    if (a.path.length) {
      // A walk to where the snapshot says gives way to new work at once.
      if (a.cur && a.cur.settle && a.queue.length && !a.leaving) { a.path = []; a.moving = false; a.cur = null; }
      else {
        walk(a, dt);
        if (!a.path.length) { a.moving = false; if (a.spot) a.face = a.spot.face; arrive(a, now); }
        return;
      }
    }
    a.moving = false;
    if (a.cur && !a.cur.arrived) arrive(a, now);
    if (a.cur && now < a.cur.until) return;
    if (a.leaving && a.spot === DOOR && a.cur && a.cur.act === 'leave') {
      // At the doors: wait for them to open, then step into the car.
      if (lift.open < 0.9) return;
      a.car = 'enter';
      a.path = [[carSlot(a), CAR_Y]];
      return;
    }
    // Too much queued: skip ahead, keeping the latest few steps (and leaving).
    if (a.queue.length > 14) a.queue = a.queue.filter((s, i) => i >= a.queue.length - 8 || s.act === 'leave');
    // Claude stays at its desk while a subagent still comes to report there:
    // the walk over can take longer than the report does in the snapshot.
    if (a.kind === 'claude' && toLounge(a) && reportDue(now)) return;
    const next = a.queue.shift();
    if (next) begin(a, next, now);
    else settle(a, now);
  }
  // toLounge: what Claude does next is going to the lounge.
  function toLounge(a) {
    const s = a.queue[0];
    if (s) return s.act === 'idle' && s.room === 'lounge';
    return !!a.snap && a.snap.state === 'idle' && a.snap.room === 'lounge' && a.settled !== settleKey(a.snap);
  }
  // reportDue: a subagent is on its way to Claude's desk to report, or
  // reporting there.
  function reportDue(now) {
    for (const o of actors.values()) {
      if (o.kind !== 'agent' || o.gone) continue;
      if (o.cur && o.cur.act === 'report' && !(o.cur.arrived && now >= o.cur.until)) return true;
      if (o.queue.some((s) => s.act === 'report')) return true;
    }
    return false;
  }

  // ---- the elevator ----------------------------------------------------------------------
  const lift = { open: 0, arrow: '', arrowUntil: 0, floor: 0 };
  // liftWanted: the doors stay open while anyone waits in the car to step
  // out, walks out or in through them, walks up to leave, or has just
  // stepped in and turns round.
  function liftWanted(now) {
    for (const a of actors.values()) {
      if (a.wait > 0 || a.gone) continue;
      switch (a.car) {
        case 'in': if (now >= a.carAt) return true; break;
        case 'out': if (a.y < 0 || (a.path.length && a.y < 1.2)) return true; break;
        case 'enter': return true;
        case 'ride': if (now - a.carAt < 600) return true; break;
        default: if (a.leaving && Math.hypot(a.x - LIFT_DOOR[0], a.y - LIFT_DOOR[1]) < 2.2) return true;
      }
    }
    return false;
  }
  // The doors slide at the same pace whatever the frame rate.
  function moveLift(dt, now) {
    const step = 1.6 * dt;
    lift.open = clamp(lift.open + clamp((liftWanted(now) ? 1 : 0) - lift.open, -step, step), 0, 1);
  }

  // ---- reading the server's answer -------------------------------------------------------
  function apply(r) {
    if (r.now) skew = Date.parse(r.now) - Date.now();
    if (r.me && r.me !== me) { me = r.me; sprites.clear(); }
    if (Array.isArray(r.accounts)) acctOrder = r.accounts;
    building = r.building || null;
    settleHidden();
    // A vacated floor number went to another project: whether that floor's
    // card was open was about the old one.
    for (const f of (building && building.floors) || []) {
      const was = floorPaths.get(f.floor), now = (f.path || '').replace(/[\\/]+$/, '').toLowerCase();
      if (was !== undefined && was !== now) { openFloors.delete(f.floor); closedFloors.delete(f.floor); pend.delete('f:' + f.floor); }
      floorPaths.set(f.floor, now);
    }
    const v = r.view || null;
    const old = view;
    const now = performance.now();
    if (!v) {
      view = null;
      for (const a of [...actors.values()]) dropActor(a);
      log.length = 0;
      seq = r.seq || 0;
      renderAll();
      // A pinned session that is not there any more: follow the latest.
      if (!follow && pinned) { follow = true; pinned = ''; remember(); setTimeout(kick, 0); }
      return;
    }
    const fresh = r.reset || !old || old.id !== v.id;
    if (fresh) {
      if (old && old.floor !== v.floor) ride(old.floor, v.floor);
      view = v;
      for (const a of [...actors.values()]) dropActor(a);
      freeSpots();
      for (const s of cast(v)) if (s.state !== 'leaving') makeActor(s, 'place');
      if (!v.actors || !v.actors.some((s) => s.kind === 'me')) makeActor({ id: 'me', kind: 'me', label: me, room: 'desk', state: 'idle' }, 'place');
      // A remark made a moment ago is still in the air.
      if (v.say && v.say.text && serverNow() - Date.parse(v.say.t) < 6000) {
        const a = actors.get(v.say.actor);
        if (a) flash(a, a.kind === 'me' ? 'prompt' : 'say', v.say.text, 5, now);
      }
      log.length = 0;
      for (const ev of r.events || []) addLog(ev);
    } else {
      view = v;
      reconcile(v, now);
      for (const ev of r.events || []) {
        if (ev.seq && ev.seq <= seq) continue;
        play(ev, now);
        addLog(ev);
      }
    }
    if (typeof r.seq === 'number') seq = r.seq;
    lift.floor = v.floor;
    renderAll();
  }
  // reconcile: people who are new arrive by the elevator, people gone leave
  // by it, and a new account on the main seat is a change of shift.
  function reconcile(v, now) {
    const want = new Map(cast(v).map((s) => [s.id, s]));
    const main = actors.get('main'), smain = want.get('main');
    if (main && smain && (main.snap.account || '') !== (smain.account || '')) {
      // The old Claude goes home; the next account's Claude comes in.
      actors.delete('main');
      main.id = 'main~' + (++ghostN); main.ghost = true; main.kind = 'claude-old';
      actors.set(main.id, main);
      // Its spot goes with it, so it is freed when it walks off.
      if (main.spot && main.spot.by === 'main') main.spot.by = main.id;
      main.queue = [{ act: 'leave', room: 'elevator', flash: ['leave', t('office_b_shift_out'), 8] }];
      main.path = []; main.cur = null; main.leaving = true;
      const fresh = makeActor(smain, 'arrive');
      fresh.wait = 1.6;
      fresh.queue = [{ act: 'arrive', flash: ['note', t('office_b_shift_in'), 3.5] }];
    }
    for (const s of cast(v)) {
      const a = actors.get(s.id);
      // Someone gone already who is still listed as leaving stays gone; anyone
      // else new (or back, like Claude after a change of account) arrives.
      if (!a) { if (s.state !== 'leaving') makeActor(s, 'arrive'); continue; }
      a.snap = s;
    }
    for (const a of actors.values()) {
      if (want.has(a.id) || a.ghost || a.leaving || a.kind === 'me') continue;
      a.leaving = true;
      if (!a.queue.some((s) => s.act === 'leave')) a.queue.push({ act: 'leave', room: 'elevator', flash: ['leave', t('office_b_leave'), 8] });
    }
  }
  // cast: who is drawn. At most 12 subagents; the rest are counted ("+N").
  const MAX_AGENTS = 12;
  function cast(v) {
    let n = 0;
    return (v.actors || []).filter((s) => s.kind !== 'agent' || ++n <= MAX_AGENTS);
  }
  function moreAgents(v) {
    if (!v) return 0;
    const agents = (v.actors || []).filter((s) => s.kind === 'agent').length;
    return (v.more_agents || 0) + Math.max(0, agents - MAX_AGENTS);
  }
  function play(ev, now) {
    if (ev.kind === 'prompt') {
      const m = actors.get('me') || [...actors.values()].find((a) => a.kind === 'me');
      if (m) flash(m, 'prompt', ev.detail || '', 7, now);
      return;
    }
    if (ev.kind === 'ok') return;
    let a = actors.get(ev.actor);
    if (!a && ev.kind === 'hire' && [...actors.values()].filter((x) => x.kind === 'agent' && !x.leaving).length < MAX_AGENTS) {
      a = makeActor({ id: ev.actor, kind: 'agent', type: ev.type || '', label: ev.type || '', desc: ev.detail, room: 'elevator', state: 'arriving' }, 'arrive');
    }
    if (!a || a.ghost) return;
    const s = stepOf(ev, a);
    if (s) a.queue.push(s);
  }

  // ---- the log ----------------------------------------------------------------------------
  function who(id, ev) {
    const a = actors.get(id);
    if (a) return { name: nameOf(a.snap), color: a.look.color };
    if (id === 'me') return { name: t('office_me', { name: me || 'me' }), color: '#7d8fb8' };
    if (id === 'main') return { name: 'Claude', color: CORAL };
    const type = (ev && ev.type) || '';
    return { name: type || t('office_agent'), color: roleOf(type).color };
  }
  function logLine(ev) {
    const d = ev.detail || '';
    switch (ev.kind) {
      case 'think': return ['💭', d || t('office_l_think')];
      case 'say': return ['💬', d];
      case 'tool': return [ROOM_ICON[ev.room || toolRoom(ev.tool)] || '🔧', d || ev.tool, d ? ev.tool : ''];
      case 'fail': return ['❌', t('office_l_fail', { d: d || ev.tool || '' })];
      case 'deny': return ['✋', d || t('office_l_deny')];
      case 'prompt': return ['🙋', '“' + d + '”'];
      case 'error': return ['⚠️', d];
      case 'compact': return ['🗜', t('office_l_compact')];
      case 'idle': return ev.room === 'seat' ? ['🪑', t('office_l_wait')] : ['☕', t('office_l_idle')];
      case 'hire': return ['📞', t('office_l_hire', { type: ev.type || t('office_agent') }) + (d ? ' · ' + d : '')];
      case 'report': return ['📋', t('office_l_report')];
      case 'leave': return ['👋', t('office_l_leave')];
      case 'ask': return ['❓', d ? t('office_l_ask', { d }) : t('office_l_ask_plain')];
      case 'shift': return ['🔁', t('office_l_shift', { d })];
      case 'clear': return ['🧹', t('office_l_clear')];
      case 'end': return ['🌙', t('office_l_end')];
      default: return null;
    }
  }
  function addLog(ev) {
    if (ev.kind === 'ok' || !logLine(ev)) return;
    log.unshift(ev);
    if (log.length > 150) log.length = 150;
  }

  // ---- the camera -----------------------------------------------------------------------
  // insetL and insetR are what an open drawer covers of the stage: the
  // building is fitted to (and centred in) the rest, though never squeezed
  // into less than half the stage.
  const cam = { user: 1, px: 0, py: 0, z: 1, S: 1, w: 0, h: 0, dpr: 1, tx: 0, ty: 0, zoomingUntil: 0, insetL: 0, insetR: 0 };
  const camFree = () => Math.max(cam.w * 0.5, cam.w - cam.insetL - cam.insetR);
  const camMid = () => clamp(cam.insetL + (cam.w - cam.insetL - cam.insetR) / 2, cam.w * 0.25, cam.w * 0.75);
  function fitZ() {
    return Math.min(camFree() / (WORLD.r - WORLD.l), cam.h / (WORLD.b - WORLD.t));
  }
  function updateCam() {
    const f = fitZ();
    cam.z = clamp(f * cam.user, 0.25, 3);
    cam.S = cam.z * cam.dpr;
    const wcx = (WORLD.l + WORLD.r) / 2, wcy = (WORLD.t + WORLD.b) / 2;
    cam.tx = Math.round((camMid() + cam.px - wcx * cam.z) * cam.dpr);
    cam.ty = Math.round((cam.h / 2 + cam.py - wcy * cam.z) * cam.dpr);
  }
  const toScreen = (wx, wy) => [(wx * cam.S + cam.tx) / cam.dpr, (wy * cam.S + cam.ty) / cam.dpr];
  const toWorld = (sx, sy) => [(sx * cam.dpr - cam.tx) / cam.S, (sy * cam.dpr - cam.ty) / cam.S];
  function zoomAt(f, sx, sy) {
    const before = toWorld(sx, sy);
    const z0 = fitZ();
    cam.user = clamp(cam.user * f, 0.6, 3 / Math.max(0.1, z0));
    updateCam();
    const after = toScreen(...before);
    cam.px += sx - after[0]; cam.py += sy - after[1];
    cam.zoomingUntil = performance.now() + 180;
    updateCam();
  }
  function resetCam() { cam.user = 1; cam.px = 0; cam.py = 0; updateCam(); }
  function centerOn(a) {
    const [sx, sy] = toScreen(...pt(a.x, a.y, 20));
    cam.px += camMid() - sx; cam.py += cam.h / 2 - sy;
    updateCam();
  }

  // ---- sprites -------------------------------------------------------------------------------
  // Cached drawings, redrawn when the zoom settles, the theme flips or the
  // language changes.
  const sprites = new Map();
  function spriteKey() { return themeKey + '|' + langOf(); }
  function getSprite(id, bounds, paint) {
    let sp = sprites.get(id);
    const zooming = performance.now() < cam.zoomingUntil;
    const want = bounds.cap ? Math.min(cam.S, bounds.cap) : cam.S;
    if (sp && sp.key === spriteKey() && (sp.S === want || zooming)) return sp;
    const c = document.createElement('canvas');
    const w = Math.ceil((bounds.r - bounds.l) * want) + 2, ht = Math.ceil((bounds.b - bounds.t) * want) + 2;
    c.width = Math.max(1, w); c.height = Math.max(1, ht);
    const g = c.getContext('2d');
    g.setTransform(want, 0, 0, want, -bounds.l * want, -bounds.t * want);
    paint(g, want);
    sp = { c, S: want, key: spriteKey(), l: bounds.l, t: bounds.t };
    sprites.set(id, sp);
    return sp;
  }
  function blit(g, sp) {
    g.setTransform(1, 0, 0, 1, 0, 0);
    const k = cam.S / sp.S;
    g.drawImage(sp.c, Math.round(cam.tx + sp.l * cam.S), Math.round(cam.ty + sp.t * cam.S), Math.round(sp.c.width * k), Math.round(sp.c.height * k));
    g.setTransform(cam.S, 0, 0, cam.S, cam.tx, cam.ty);
  }
  function propBounds(p) {
    const w = p.w || 0.6, d = p.d || 0.6, x = p.w ? p.x : p.x - 0.3, y = p.d ? p.y : p.y - 0.3;
    return { l: (x - (y + d)) * HW - 14, r: (x + w - y) * HW + 14, t: (x + y) * HH - p.ht - 22, b: (x + w + y + d) * HH + 10 };
  }

  // ---- live parts ----------------------------------------------------------------------------
  function busyIn(room) {
    for (const a of actors.values()) if (a.spot && a.spot.room === room && !a.moving && a.cur && (a.cur.act === 'tool' || (a.snap && a.snap.state === 'working'))) return true;
    return false;
  }
  function deskUser(key) {
    for (const a of actors.values()) if (a.spot && a.spot.desk === key && !a.moving && a.alpha > 0.5) return a;
    return null;
  }
  const LIVE = {
    desk(g, p, now) {
      const a = deskUser(p.x + ',' + p.y);
      const on = !!a;
      const typing = a && (a.arms === 'type');
      g.save(); onFaceY(g, p.x + 0.12, p.y + 0.28, 51);
      const w = 0.76 * HW - 3;
      g.beginPath(); g.rect(1.5, 1.5, w, 21); g.clip();
      if (on) {
        g.fillStyle = P.screen; g.fillRect(1.5, 1.5, w, 21);
        const scroll = typing ? (now / 1000) * 9 : 0;
        for (let i = -1; i < 9; i++) {
          const row = Math.floor(scroll / 3) + i, y = 3 + i * 3 - (scroll % 3);
          const rr = rng(hash(p.id) + row * 7919);
          let u = 3 + (rr() * 3 | 0) * 2.5;
          const n = 1 + (rr() * 3 | 0);
          for (let k = 0; k < n && u < w - 2; k++) { const lw = 3 + rr() * 8; g.fillStyle = P.code[(rr() * P.code.length) | 0]; g.fillRect(u, y, Math.min(lw, w - u), 1.4); u += lw + 1.5; }
        }
        if (P.night) { g.fillStyle = 'rgba(125,211,252,.10)'; g.fillRect(1.5, 1.5, w, 21); }
      } else if ((now / 600 | 0) % 2 === 0) {
        g.fillStyle = 'rgba(255,255,255,.35)'; g.fillRect(4, 5, 1.2, 2.6);
      }
      g.restore();
      if (on && P.night) glow(g, pt(p.x + 0.5, p.y + 0.4, 38), 34, 'rgba(110,190,255,.20)');
    },
    rack(g, p, now) {
      const fast = busyIn('server');
      const tt = now / 1000;
      g.save(); rackFace(g, p);
      for (let row = 0; row < 9; row++) {
        for (let k = 0; k < 3; k++) {
          const seed = hash(p.id + row + ':' + k);
          const rate = fast ? 5 + (seed % 5) : 0.6 + (seed % 7) / 6;
          const on = Math.sin(tt * rate * Math.PI + seed) > (fast ? -0.2 : 0.3);
          g.fillStyle = on ? P.led[seed % P.led.length] : 'rgba(255,255,255,.08)';
          g.fillRect(4 + k * 3.2, 9 + row * 9 - 5, 1.8, 1.8);
        }
      }
      g.restore();
      if (P.night) glow(g, p.front === 'y' ? pt(p.x + 0.5, p.y + 1, 40) : pt(p.x + 1, p.y + 0.5, 40), 26, fast ? 'rgba(74,222,128,.14)' : 'rgba(96,165,250,.08)');
    },
    kiosk(g, p, now) {
      const on = deskNear(p.x - 1, p.y);
      const tt = now / 1000;
      const k = on ? 0.35 + Math.sin(tt * 4) * 0.15 : P.night ? 0.12 : 0;
      if (k > 0) poly(g, [pt(p.x + 0.26, p.y + 0.24, 64), pt(p.x + 0.76, p.y + 0.24, 64), pt(p.x + 0.76, p.y + 0.76, 64), pt(p.x + 0.26, p.y + 0.76, 64)], 'rgba(255,255,255,' + k + ')');
      if (P.night) glow(g, pt(p.x + 0.5, p.y + 0.5, 60), 30, 'rgba(127,216,255,.16)');
    },
    globe(g, p, now) {
      const c = pt(p.x + 0.5, p.y + 0.5);
      const spin = deskNear(p.x - 1, p.y) ? now / 900 : now / 6000;
      ellipse(g, c[0], c[1], 9, 4.2, P.brass);
      line(g, [c[0], c[1] - 1], [c[0], c[1] - 24], P.brass, 2);
      const cy = c[1] - 38, r = 13;
      const grad = g.createRadialGradient(c[0] - 4, cy - 4, 2, c[0], cy, r);
      grad.addColorStop(0, shade(P.globe, 0.35)); grad.addColorStop(1, P.globe);
      ellipse(g, c[0], cy, r, r, grad);
      g.save(); g.beginPath(); g.arc(c[0], cy, r, 0, Math.PI * 2); g.clip();
      for (let i = 0; i < 5; i++) {
        const u = ((i * 0.37 + spin) % 1) * 2 - 1, lat = [-6, 3, 7, -2, 1][i];
        ellipse(g, c[0] + u * r * 1.2, cy + lat, 4.5 * (1 - Math.abs(u) * 0.5), 3.4, P.land);
      }
      g.restore();
      g.strokeStyle = P.brass; g.lineWidth = 1.3; g.beginPath(); g.arc(c[0], cy, r + 2.5, Math.PI * 0.6, Math.PI * 2.1); g.stroke();
    },
    counter(g, p, now) {
      if (!p.machine) return;
      const busy = [...actors.values()].some((a) => a.spot && a.spot.room === 'lounge' && !a.moving);
      if (!busy) return;
      const base = pt(p.x + 0.3, p.y + 0.45, 58), tt = now / 1000;
      for (let i = 0; i < 3; i++) {
        const k = (tt * 0.6 + i / 3) % 1;
        g.globalAlpha = (1 - k) * 0.55;
        ellipse(g, base[0] + Math.sin(tt * 2 + i * 2) * 2.5, base[1] - k * 16, 2.4 + k * 2, 2.4 + k * 2, P.night ? '#9aa4b2' : '#ffffff');
      }
      g.globalAlpha = 1;
    },
    lamp(g, p) { if (P.night) glow(g, pt(p.x + 0.5, p.y + 0.5, 56), 60, 'rgba(255,210,130,.22)'); },
    rtable(g, p) { if (P.night) glow(g, pt(p.x + 1, p.y + 1, 32), 46, 'rgba(255,210,130,.18)'); },
    udesk(g, p) {
      if (!P.night) return;
      if (p.who === 'me') glow(g, pt(p.x + 0.31, p.y + 0.57, 38), 40, 'rgba(255,210,130,.22)');
      glow(g, pt(p.x + 1, p.y + 0.5, 34), 36, 'rgba(110,190,255,.14)');
    },
  };
  function deskNear(x, y) {
    for (const a of actors.values()) if (a.spot && a.spot.x === x && a.spot.y === y && !a.moving) return true;
    return false;
  }
  function glow(g, c, r, color) {
    const grad = g.createRadialGradient(c[0], c[1], 0, c[0], c[1], r);
    grad.addColorStop(0, color); grad.addColorStop(1, 'rgba(0,0,0,0)');
    g.save(); g.globalCompositeOperation = 'lighter'; g.fillStyle = grad;
    g.fillRect(c[0] - r, c[1] - r, r * 2, r * 2); g.restore();
  }
  // The elevator car behind the doors: its back and left walls, its floor,
  // a warm light and whoever is in it (drawn clipped to the open doors).
  const CAR = { x0: 15.75 + 6 / HW, x1: 15.75 + (2.5 * HW - 6) / HW, back: -0.95, ht: 67 };
  function drawCar(g, now) {
    const { x0, x1, back, ht } = CAR;
    poly(g, [pt(x0, back, 0), pt(x1, back, 0), pt(x1, back, ht), pt(x0, back, ht)], P.liftIn);
    poly(g, [pt(x0, back, 0), pt(x0, 0, 0), pt(x0, 0, ht), pt(x0, back, ht)], shade(P.liftIn, -0.1));
    floorQuad(g, x0, back, x1, 0, shade(P.liftIn, -0.16));
    poly(g, [pt(x0, back, 0), pt(x1, back, 0), pt(x1, back, ht), pt(x0, back, ht)], 'rgba(255,220,150,.22)');
    const riders = [...actors.values()].filter((a) => a.y < 0).sort((p, q) => (p.x + p.y) - (q.x + q.y));
    for (const a of riders) drawPerson(g, a, now);
  }
  function drawWallLive(g, now) {
    // Elevator doors and the floor panel over them; between the doors, the car.
    const world = g.getTransform();
    g.save(); onFaceY(g, 15.75, 0, 76);
    const L = 6, R = 2.5 * HW - 6, mid = (L + R) / 2, half = (R - L) / 2, o = lift.open * half * 0.92;
    g.fillStyle = P.liftIn; g.fillRect(L, 9, R - L, 67);
    if (lift.open > 0.02) {
      g.save();
      g.beginPath(); g.rect(mid - o, 9, o * 2, 67); g.clip();
      g.setTransform(world);
      drawCar(g, now);
      g.restore();
    }
    g.fillStyle = P.lift;
    g.fillRect(L, 9, half - o, 67); g.fillRect(mid + o, 9, half - o, 67);
    g.fillStyle = P.liftLine;
    g.fillRect(mid - o - 0.6, 9, 1.2, 67); g.fillRect(mid + o - 0.6, 9, 1.2, 67);
    g.fillStyle = 'rgba(255,255,255,.18)'; g.fillRect(L + 4, 12, 2, 60); g.fillRect(mid + o + 4, 12, 2, 60);
    // The floor number, with an arrow while riding.
    const riding = now < lift.arrowUntil;
    g.font = '700 9px Consolas, "Cascadia Mono", monospace';
    g.fillStyle = P.digit; g.textAlign = 'center'; g.textBaseline = 'middle';
    const n = lift.floor ? lift.floor + 'F' : '–';
    g.fillText((riding && (now / 250 | 0) % 2 ? lift.arrow + ' ' : '') + n, 2.5 * HW / 2, -8.2);
    g.restore();
    // The clock shows the time here.
    const d = new Date();
    g.save(); onFaceY(g, 14.75, 0, 66);
    const hA = ((d.getHours() % 12) + d.getMinutes() / 60) / 12 * Math.PI * 2 - Math.PI / 2;
    const mA = (d.getMinutes() + d.getSeconds() / 60) / 60 * Math.PI * 2 - Math.PI / 2;
    line(g, [0, 0], [Math.cos(hA) * 4.6, Math.sin(hA) * 4.6], '#3b4250', 1.5);
    line(g, [0, 0], [Math.cos(mA) * 7, Math.sin(mA) * 7], '#3b4250', 1);
    ellipse(g, 0, 0, 0.9, 0.9, '#e8590c');
    g.restore();
    // The todo list on the whiteboard.
    const todos = (view && view.todos) || [];
    g.save(); onWallL(g, 10.45, 76);
    g.font = '700 6.5px ' + FONT; g.textBaseline = 'middle'; g.textAlign = 'left';
    g.fillStyle = P.ink2; g.fillText('TODO', 7, 9);
    // A long list shows five rows around the step at hand, from the first
    // one not done if it fits; the header says how many are done in all, and
    // how many open ones are above the rows.
    const open = todos.findIndex((td) => td.status !== 'completed');
    let at = todos.findIndex((td) => td.status === 'in_progress');
    if (at < 0) at = open;
    const from = Math.max(0, Math.min(at - 1, Math.max(open, at - 4), todos.length - 5));
    if (todos.length > 5) {
      const above = todos.slice(0, from).filter((td) => td.status !== 'completed').length;
      g.fillStyle = P.inkDone;
      g.fillText(todos.filter((td) => td.status === 'completed').length + '/' + todos.length + (above ? '  ↑' + above : ''),
        7 + g.measureText('TODO').width + 4, 9);
    }
    g.font = '6px ' + FONT;
    todos.slice(from, from + 5).forEach((td, i) => {
      const y = 17 + i * 6.4, done = td.status === 'completed', cur = td.status === 'in_progress';
      g.strokeStyle = done ? P.inkDone : P.ink; g.lineWidth = 0.7; g.strokeRect(7, y - 2, 4, 4);
      if (done) line(g, [7.6, y], [9.6, y + 1.5], P.inkDone, 0.9);
      if (cur) { g.fillStyle = P.ink2; g.fillRect(7.9, y - 1.1, 2.2, 2.2); }
      g.fillStyle = done ? P.inkDone : P.ink;
      g.fillText(td.text || '', 14, y + 0.2, 3.9 * HW - 22);
      if (done) line(g, [14, y + 0.2], [14 + Math.min(g.measureText(td.text || '').width, 3.9 * HW - 22), y + 0.2], P.inkDone, 0.5);
    });
    const below = todos.length - from - 5;
    if (below > 0) { g.fillStyle = P.inkDone; g.fillText('+' + below, 14, 17 + 5 * 6.4); }
    g.restore();
  }

  // ---- the frame -----------------------------------------------------------------------------
  let canvas = null, ctx = null, raf = 0, lastTs = 0, lastDraw = 0, lastPanel = 0;
  const statics = [];
  function buildStatics() {
    statics.length = 0;
    for (const p of props) statics.push({ depth: p.depth, p });
    for (const s of segs) statics.push({ depth: s.depth, s });
  }
  buildStatics();
  function frame(ts) {
    raf = 0;
    if (!running()) return;
    try {
      // Dragged to a screen with another scale: the canvas follows (its CSS
      // size, which the resize observer watches, stays the same).
      if ((window.devicePixelRatio || 1) !== cam.dpr) resize();
      const now = performance.now();
      const dt = Math.min(0.1, (now - (lastTs || now)) / 1000);
      lastTs = now;
      for (const a of [...actors.values()]) { tick(a, dt, now); if (a.gone) dropActor(a); }
      const was = lift.open;
      moveLift(dt, now);
      const moving = lift.open !== was || [...actors.values()].some((a) => a.moving || a.fade);
      if (moving || now - lastDraw > 30) { draw(now); lastDraw = now; }
      if (now - lastPanel > 1000) { lastPanel = now; renderPanel(); renderSide(); }
    } finally {
      // Whatever one frame trips over, the office keeps going.
      if (running()) raf = requestAnimationFrame(frame);
    }
  }
  function draw(now) {
    if (!ctx || !cam.w) return;
    const g = ctx;
    g.setTransform(1, 0, 0, 1, 0, 0);
    g.clearRect(0, 0, canvas.width, canvas.height);
    const base = getSprite('base', Object.assign({ cap: 2.2 }, WORLD), (bg, S) => paintBase(bg, S));
    blit(g, base);
    g.setTransform(cam.S, 0, 0, cam.S, cam.tx, cam.ty);
    drawWallLive(g, now);
    const list = statics.slice();
    for (const a of actors.values()) if (a.y >= 0) list.push({ depth: a.x + a.y + 0.01, a }); // the car's riders are drawn with it
    list.sort((p, q) => p.depth - q.depth);
    for (const it of list) {
      if (it.a) { drawPerson(g, it.a, now); continue; }
      if (it.s) { drawSeg(g, it.s); continue; }
      const p = it.p;
      if (PAINT[p.type]) blit(g, getSprite(p.id + (p.type === 'udesk' ? me : ''), propBounds(p), (pg) => PAINT[p.type](pg, p)));
      if (LIVE[p.type]) LIVE[p.type](g, p, now);
    }
    placeDom(now);
  }

  // ---- bubbles and name tags (DOM over the canvas) --------------------------------------------
  function bubbleFor(a, now) {
    if (a.flash && a.flash.until > now) return a.flash;
    const s = a.cur;
    const sn = a.snap || {};
    const act = s ? s.act : '';
    if (a.kind === 'me') return null;
    if (act === 'think' || (s && s.settle && sn.state === 'thinking')) {
      const since = sn.state === 'thinking' && sn.since ? Date.parse(sn.since) : 0;
      return { kind: 'think', text: t('office_b_think'), sub: since ? t('office_b_secs', { n: Math.max(0, Math.round((serverNow() - since) / 1000)) }) : '' };
    }
    if (act === 'ask' || (s && s.settle && sn.state === 'asking')) return { kind: 'ask', text: (s && s.detail) || sn.detail || t('office_b_ask') };
    if (act === 'tool' && s.tool) return { kind: 'tool', icon: ROOM_ICON[sp(a)] || '🔧', text: s.detail || s.tool, sub: s.detail ? s.tool : '', git: s.git || (s.settle && isGit(s)) };
    if (s && s.settle && sn.state === 'reporting') return { kind: 'report', text: '📋 ' + t('office_b_report') };
    if (s && s.settle && sn.state === 'talking' && view && view.say && view.say.actor === a.id && view.say.text) return { kind: 'say', text: view.say.text };
    if (s && s.settle && sn.state === 'error' && (sn.detail || sn.tool)) return { kind: 'fail', text: sn.detail || t('office_l_fail', { d: sn.tool }) };
    return null;
  }
  const sp = (a) => (a.spot ? a.spot.room : '');
  function domFor(a) {
    if (a.dom) return a.dom;
    const tag = h('div', { class: 'of-tag' });
    const dot = h('i', { class: 'of-tagdot' });
    dot.style.background = a.look.color;
    const name = h('span', { text: nameOf(a.snap) });
    tag.append(dot, name);
    const bubble = h('div', { class: 'of-bubble' });
    const inner = h('div', { class: 'of-actor-in' }, bubble, tag);
    const wrap = h('div', { class: 'of-actor' + (a.kind === 'claude' ? ' main' : '') }, inner);
    els.layer.append(wrap);
    a.dom = { wrap, inner, tag, dot, name, bubble, key: '', pos: '', nameKey: '', lift: 0 };
    return a.dom;
  }
  function setBubble(d, b) {
    const key = b ? [b.kind, b.text, b.sub, b.icon, b.git].join('|') : '';
    if (key === d.key) return;
    d.key = key;
    if (!b) { d.bubble.className = 'of-bubble'; d.bubble.replaceChildren(); d.bw = d.bh = 0; return; }
    d.bubble.className = 'of-bubble on ' + b.kind + (b.git ? ' git' : '');
    const kids = [];
    if (b.kind === 'think') kids.push(h('span', { class: 'of-b-main' }, '💭 ' + b.text, h('span', { class: 'of-dots' }, h('i'), h('i'), h('i'))), b.sub ? h('span', { class: 'of-b-sub', text: b.sub }) : null);
    else if (b.kind === 'tool') kids.push(h('span', { class: 'of-b-main' }, h('span', { class: 'of-b-icon', text: b.icon }), b.git ? h('span', { class: 'of-git', text: 'git' }) : null, h('span', { class: 'of-b-text', text: b.text })), b.sub ? h('span', { class: 'of-b-sub', text: b.sub }) : null);
    else if (b.kind === 'ask') kids.push(h('span', { class: 'of-q', text: '?' }), h('span', { class: 'of-b-text', text: b.text }));
    else if (b.kind === 'fail') kids.push(h('span', { class: 'of-sweat' }), h('span', { class: 'of-b-text', text: b.text }));
    else kids.push(h('span', { class: 'of-b-text', text: b.text }));
    d.bubble.replaceChildren(...kids.filter(Boolean));
    d.bw = d.bubble.offsetWidth; d.bh = d.bubble.offsetHeight;
  }
  // unstack keeps name tags and bubbles from covering one another: the user
  // and Claude keep theirs in place, then whoever is nearest the front; the
  // others move up (a thin line still points at their head), but not far.
  function unstack() {
    const list = [...actors.values()].filter((a) => a.dom && a.dom.at && !a.dom.wrap.hidden).sort((p, q) => rank(p) - rank(q) || (q.x + q.y) - (p.x + p.y));
    const placed = [];
    for (const a of list) {
      const d = a.dom;
      if (!d.th || d.tagFor !== d.nameKey) { d.th = d.tag.offsetHeight || 18; d.tw = d.tag.offsetWidth || 60; d.tagFor = d.nameKey; }
      const half = Math.max(d.tw, d.bw || 0) / 2;
      const r = { l: d.at[0] - half, r: d.at[0] + half, t: d.at[1] - d.th - (d.bh ? d.bh + 5 : 0), b: d.at[1] };
      let lift = 0;
      for (let pass = 0, moved = true; moved && pass < 10; pass++) {
        moved = false;
        for (const o of placed) {
          if (r.l < o.r + 2 && r.r > o.l - 2 && r.t < o.b + 2 && r.b > o.t - 2) { const dy = r.b - o.t + 3; r.t -= dy; r.b -= dy; lift += dy; moved = true; }
        }
      }
      // In a crowd, the ones behind go without a tag rather than float away.
      const crowded = lift > 76 && a.kind === 'agent';
      if (crowded !== !!d.crowded) { d.crowded = crowded; d.inner.classList.toggle('crowded', crowded); }
      if (crowded) lift = 0;
      else placed.push(r);
      lift = Math.round(lift);
      if (d.lift !== lift) {
        d.lift = lift;
        d.inner.style.transform = lift ? 'translate(-50%,' + -lift + 'px)' : '';
        d.inner.style.setProperty('--lift', lift + 'px');
        d.inner.classList.toggle('lifted', lift > 0);
      }
    }
  }
  function placeDom(now) {
    for (const a of actors.values()) {
      const d = domFor(a);
      const nk = nameOf(a.snap) + '|' + a.look.color;
      if (nk !== d.nameKey) { d.nameKey = nk; d.name.textContent = nameOf(a.snap); d.dot.style.background = a.look.color; }
      const head = a.body === 'sit' && !a.moving ? 56 : 52;
      const [sx, sy] = toScreen(...pt(a.x, a.y, head));
      const visible = a.alpha > 0.25 && !(a.y < 0 && lift.open < 0.5) && sx > -150 && sy > -100 && sx < cam.w + 150 && sy < cam.h + 260;
      const pos = visible ? Math.round(sx) + ',' + Math.round(sy) + ',' + Math.round((a.x + a.y) * 10) : 'off';
      if (pos !== d.pos) {
        d.pos = pos;
        d.wrap.hidden = !visible;
        if (visible) { d.wrap.style.transform = 'translate(' + Math.round(sx) + 'px,' + Math.round(sy) + 'px)'; d.wrap.style.zIndex = String(10 + Math.round((a.x + a.y) * 10)); }
      }
      d.at = [sx, sy];
      setBubble(d, visible ? bubbleFor(a, now) : null);
    }
    unstack();
    // "+N more" by the elevator when not everyone could be drawn.
    const more = moreAgents(view);
    if (els.more) {
      els.more.hidden = !more;
      if (more) {
        els.more.textContent = t('office_more_agents', { n: more });
        const [mx, my] = toScreen(...pt(17.2, 2.4, 30));
        els.more.style.transform = 'translate(' + Math.round(mx) + 'px,' + Math.round(my) + 'px)';
      }
    }
  }

  // ---- panels ------------------------------------------------------------------------------------
  function renderAll() {
    if (!root) return;
    renderStage();
    renderSide();
    renderPanel();
  }
  function floorBadge(n, extra) {
    return h('span', { class: 'of-fb fb' + (Math.abs(n || 0) % 8) + (extra ? ' ' + extra : ''), text: (n || '?') + 'F' });
  }
  function renderStage() {
    const v = view;
    els.title.hidden = !v;
    if (v) {
      els.title.replaceChildren(floorBadge(v.floor, 'big'),
        h('div', { class: 'of-title-text' }, h('b', { text: baseName(v.path) || '—', title: v.path || '' }),
          h('span', { text: v.title || t('office_untitled', { id: short(v.id) }), title: v.title || '' })));
    }
    const live = v && v.live && !notice;
    els.live.className = 'of-live pill ' + (live ? 'ok' : 'off');
    els.live.replaceChildren(h('i'), live ? 'LIVE' : t('office_history'));
    els.live.hidden = !v;
    let msg = null;
    if (notice === 'off') msg = ['🔒', t('office_off_title'), t('office_off')];
    else if (notice === 'gone') msg = ['🚪', t('office_gone_title'), t('office_gone')];
    else if (notice === 'expired') msg = ['⌛', '', t('expired')];
    else if (!v && loaded && allHidden()) msg = ['🙈', t('office_all_hidden_title'), t('office_all_hidden'), 'unhide'];
    else if (!v && loaded) msg = ['🏢', t('office_empty_title'), t('office_empty_body')];
    els.notice.hidden = !msg;
    // Drawn again only when it says something else: a rebuild under the
    // pointer would swallow a click on its button.
    const nk = msg ? msg.join('|') : '';
    if (msg && nk !== noticeKey) {
      fill(els.notice, h('div', { class: 'of-notice-card' }, h('div', { class: 'of-notice-art', text: msg[0] }),
        msg[1] ? h('h3', { text: msg[1] }) : null, h('p', { class: 'muted', text: msg[2] }),
        msg[3] === 'unhide' ? h('div', { class: 'actions of-notice-act' }, h('button', { class: 'btn primary', type: 'button', text: t('office_unhide_all'), onclick: unhideAll })) : null));
    }
    noticeKey = nk;
    els.retry.hidden = notice !== 'retry';
  }
  let noticeKey = '';
  // ---- what the user hid ----------------------------------------------------------------------
  // Hidden is a blacklist kept by the server: a new conversation shows by
  // default, and follow mode skips what is hidden. A floor hidden hides its
  // new conversations too.
  const offS = (s) => { const p = pend.get('s:' + s.id); return p ? p.hidden : !!s.hidden; };
  const offF = (f) => { const p = pend.get('f:' + f.floor); return p ? p.hidden : !!f.hidden; };
  const sessOn = (f, s) => !offF(f) && !offS(s);
  // A floor is in the guide while it shows and has a conversation that does.
  const floorOn = (f) => !offF(f) && (f.sessions || []).some((s) => !offS(s));
  // hiddenIn: how many of a floor's conversations are hidden one by one,
  // counting those past the 40 listed as the server does.
  function hiddenIn(f) {
    const ss = f.sessions || [];
    const listedBefore = ss.filter((s) => s.hidden).length;
    const beyond = Math.max(0, (f.count || ss.length) - (typeof f.shown === 'number' ? f.shown : ss.length) - listedBefore);
    return beyond + ss.filter((s) => offS(s)).length;
  }
  function allHidden() {
    const fs = (building && building.floors) || [];
    return fs.length > 0 && !fs.some(floorOn);
  }
  // settleHidden drops the changes the building shows now, and old ones.
  function settleHidden() {
    if (!pend.size) return;
    const at = new Map();
    for (const f of (building && building.floors) || []) {
      at.set('f:' + f.floor, !!f.hidden);
      for (const s of f.sessions || []) at.set('s:' + s.id, !!s.hidden);
    }
    for (const [k, p] of pend) if (Date.now() - p.at > PEND_MS || at.get(k) === p.hidden) pend.delete(k);
  }
  // setHidden sends changes ({key, session or floor, hidden}) to the server,
  // shown at once; a failure puts them back as they were.
  function setHidden(changes, body) {
    const at = Date.now();
    for (const c of changes) pend.set(c.key, { hidden: c.hidden, at });
    renderSide(); renderStage();
    const req = body || { changes: changes.map((c) => (c.session ? { session: c.session, hidden: c.hidden } : { floor: c.floor, hidden: c.hidden })) };
    Promise.resolve(opts.hide ? opts.hide(req) : null).catch((e) => {
      for (const c of changes) if (pend.get(c.key) && pend.get(c.key).at === at) pend.delete(c.key);
      renderSide(); renderStage();
      if (opts.error) opts.error(e);
    });
  }
  function toggleSession(f, s) {
    if (sessOn(f, s)) {
      setHidden([{ key: 's:' + s.id, session: s.id, hidden: true }]);
      // The one pinned is put away: follow the latest again.
      if (!follow && view && view.id === s.id) setFollow(true);
      return;
    }
    const ch = [{ key: 's:' + s.id, session: s.id, hidden: false }];
    if (offF(f)) {
      // One conversation of a hidden floor: the floor shows again with just it.
      ch.push({ key: 'f:' + f.floor, floor: f.path, hidden: false });
      for (const o of f.sessions || []) if (o.id !== s.id && !offS(o)) ch.push({ key: 's:' + o.id, session: o.id, hidden: true });
    }
    setHidden(ch);
  }
  // toggleFloor: clearing the box hides the project, future conversations
  // too; ticking it (from clear or mixed) shows the whole floor again.
  function toggleFloor(f, state) {
    if (state === 'on') {
      setHidden([{ key: 'f:' + f.floor, floor: f.path, hidden: true }]);
      if (!follow && view && view.floor === f.floor) setFollow(true);
      return;
    }
    const ch = [{ key: 'f:' + f.floor, floor: f.path, hidden: false }];
    for (const s of f.sessions || []) if (offS(s)) ch.push({ key: 's:' + s.id, session: s.id, hidden: false });
    setHidden(ch);
  }
  function unhideAll() {
    const ch = [];
    for (const f of (building && building.floors) || []) {
      if (offF(f)) ch.push({ key: 'f:' + f.floor, hidden: false });
      for (const s of f.sessions || []) if (offS(s)) ch.push({ key: 's:' + s.id, hidden: false });
    }
    hiddenOpen = false;
    setHidden(ch, { all: true, hidden: false });
  }

  // ---- the floor guide --------------------------------------------------------------------------
  let sideKey = '';
  function renderSide() {
    const b = building || { floors: [], total: 0 };
    // Only what is drawn counts (the times through their "ago" texts, every
    // 30 s): a busy floor's times change with every poll, and a rebuild
    // loses a click under way and the scroll of a long session list.
    const drawn = (b.floors || []).map((f) => [f.floor, f.name, f.path, f.live, f.busy, f.count, f.shown, f.hidden, Date.parse(f.last) > Date.now() - 3 * 86400e3,
      (f.sessions || []).map((s) => [s.id, s.title, s.live, s.status, s.account, s.hidden])]);
    const key = JSON.stringify([drawn, view && view.id, view && view.floor, pinned, follow, [...openFloors], [...closedFloors], quietOpen, hiddenOpen,
      [...pend].map(([k, p]) => k + p.hidden), langOf(), Math.floor(Date.now() / 30000)]);
    if (key === sideKey) return;
    sideKey = key;
    const scrolled = new Map([...els.floors.querySelectorAll('.of-sess')].map((el) => [el.dataset.floor, el.scrollTop]));
    const focus = focusSpot();
    const floors = (b.floors || []).slice().sort((p, q) => q.floor - p.floor);
    const inView = (f) => view && f.floor === view.floor;
    // The recent and quiet floors are split among those that show.
    const on = floors.filter(floorOn);
    const recent = (f) => f.live || (Date.now() - Date.parse(f.last)) < 3 * 86400e3 || inView(f);
    let shownF = on.filter(recent);
    if (!shownF.length) shownF = on.slice().sort((p, q) => Date.parse(q.last) - Date.parse(p.last)).slice(0, 5).sort((p, q) => q.floor - p.floor);
    const quietN = on.filter((f) => !shownF.includes(f)).length;
    const off = floors.filter((f) => !floorOn(f));
    const hiddenN = off.length + on.reduce((n, f) => n + hiddenIn(f), 0);
    if (!hiddenN) hiddenOpen = false;
    // While nothing shows the hidden are listed, since the office then
    // points here to tick something again; they stay listed as the first
    // comes back, for the next.
    const allOff = !on.length && floors.length > 0;
    if (allOff) hiddenOpen = true;
    // What is hidden is listed (dimmed) only then, when asked for, or when
    // it is the conversation in view.
    const list = floors.filter((f) => (quietOpen ? on.includes(f) : shownF.includes(f)) || (hiddenOpen && off.includes(f)) || inView(f));
    const follower = h('label', { class: 'of-follow', 'data-key': 'follow', title: hiddenN ? t('office_follow') + ' · ' + t('office_follow_tip') : t('office_follow') },
      h('input', { type: 'checkbox', checked: follow, onchange: (e) => setFollow(e.target.checked) }),
      h('span', { text: t('office_follow') }));
    fill(els.floors,
      follower,
      list.length ? null : h('p', { class: 'muted small of-pad', text: t('office_no_floors') }),
      ...list.map((f) => floorCard(f)),
      quietN > 0 ? h('button', { class: 'of-quiet', type: 'button', 'data-key': 'quiet', 'aria-expanded': String(quietOpen), title: t('office_quiet_tip'),
        text: quietOpen ? t('office_quiet_less') : t(quietN === 1 ? 'office_quiet_more1' : 'office_quiet_more', { n: quietN }), onclick: () => { quietOpen = !quietOpen; renderSide(); } }) : null,
      // Folding the hidden away means nothing while nothing else shows.
      hiddenN > 0 ? h('div', { class: 'of-hidden-foot' },
        allOff ? null : h('button', { class: 'of-quiet', type: 'button', 'data-key': 'hidden', 'aria-expanded': String(hiddenOpen), title: t('office_show_tip'),
          text: hiddenOpen ? t('office_hidden_less') : t(hiddenN === 1 ? 'office_hidden_more1' : 'office_hidden_more', { n: hiddenN }),
          onclick: () => { hiddenOpen = !hiddenOpen; renderSide(); } }),
        hiddenOpen ? h('button', { class: 'of-quiet strong', type: 'button', 'data-key': 'unhide', text: t('office_unhide_all'), onclick: unhideAll }) : null) : null);
    for (const el of els.floors.querySelectorAll('.of-sess')) {
      if (scrolled.has(el.dataset.floor)) el.scrollTop = scrolled.get(el.dataset.floor);
    }
    restoreFocus(focus);
  }
  // focusSpot and restoreFocus keep the keyboard where it was across a
  // rebuild of the guide: the same control of the same row, or, when that
  // row went away (hidden), the row that took its place.
  function focusSpot() {
    const a = document.activeElement;
    if (!a || a === els.floors || !els.floors.contains(a)) return null;
    const row = a.closest('[data-key]');
    if (!row) return null;
    return { key: row.dataset.key, sel: a.tagName === 'INPUT' ? 'input' : 'button', at: [...els.floors.querySelectorAll('[data-key]')].indexOf(row) };
  }
  function restoreFocus(spot) {
    if (!spot) return;
    const rows = [...els.floors.querySelectorAll('[data-key]')];
    const pick = (row, sel) => (row.matches(sel) ? row : row.querySelector(sel)) || (row.matches('input,button') ? row : row.querySelector('input,button'));
    let row = rows.find((r) => r.dataset.key === spot.key);
    let el = row && pick(row, spot.sel);
    let moved = false;
    if (!el && rows.length) {
      row = rows[clamp(spot.at, 0, rows.length - 1)];
      el = pick(row, 'input');
      moved = true;
    }
    if (!el) return;
    // The list keeps its scroll: a redraw (every 30 s at least) must not
    // pull it back to a box clicked long ago. Only a row that took the
    // place of one gone is brought into sight.
    el.focus({ preventScroll: true });
    if (moved) el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }
  function floorCard(f) {
    const cur = view && view.floor === f.floor;
    const open = (openFloors.has(f.floor) || cur) && !closedFloors.has(f.floor);
    const n = f.count || (f.sessions || []).length;
    const fOff = offF(f), hid = hiddenIn(f);
    const state = fOff ? 'off' : hid ? 'mixed' : 'on';
    const parts = [t(n === 1 ? 'office_floor_session1' : 'office_floor_sessions', { n })];
    if (fOff) parts.push(t('office_floor_off'));
    else if (hid) parts.push(t('office_hidden_n', { n: hid }));
    parts.push(f.busy || f.live ? t('office_active_now') : ago(f.last));
    const name = f.name || baseName(f.path);
    const chk = h('input', { type: 'checkbox', class: 'of-chk', checked: state === 'on', 'aria-label': t('office_show_floor', { name }),
      title: t('office_show_tip'), onchange: () => toggleFloor(f, state) });
    chk.indeterminate = state === 'mixed'; // a property only: h() sets attributes
    if (state === 'mixed') chk.setAttribute('aria-checked', 'mixed');
    // The conversations listed: those that show, all of them while the
    // hidden ones are listed, and the one in view in any case.
    const ss = (f.sessions || []).filter((s) => hiddenOpen || sessOn(f, s) || (view && view.id === s.id));
    return h('div', { class: 'of-floor' + (cur ? ' cur' : '') + (open ? ' open' : '') + (floorOn(f) ? '' : ' off') },
      h('div', { class: 'of-floor-head', 'data-key': 'f:' + f.floor },
        chk,
        h('button', { class: 'of-floor-btn', type: 'button', title: f.path || '', 'aria-expanded': String(!!open), onclick: () => clickFloor(f, cur, open) },
          floorBadge(f.floor),
          h('span', { class: 'of-fname' }, h('b', { text: name }), h('span', { class: 'of-fsub', text: parts.join(' · '), title: parts.join(' · ') })),
          h('span', { class: 'of-dot' + (f.live ? ' on' : '') + (f.busy ? ' busy' : '') }))),
      open && ss.length ? h('div', { class: 'of-sess', 'data-floor': String(f.floor) }, ss.map((s) => sessionRow(f, s))) : null);
  }
  function sessionRow(f, s) {
    const viewing = view && view.id === s.id;
    const on = sessOn(f, s);
    const title = s.title || t('office_untitled', { id: short(s.id) });
    const acc = s.account ? h('span', { class: 'of-acc', title: s.account }) : null;
    if (acc) acc.style.background = accountColor(s.account);
    return h('div', { class: 'of-srow' + (viewing ? ' on' : '') + (on ? '' : ' off'), 'data-key': 's:' + s.id },
      h('input', { type: 'checkbox', class: 'of-chk', checked: on, 'aria-label': t('office_show_s', { name: title }), title: t('office_show_tip'),
        onchange: () => toggleSession(f, s) }),
      h('button', { class: 'of-sbtn', type: 'button', title: title + (s.account ? ' · ' + s.account : ''), 'aria-current': viewing ? 'true' : null, onclick: () => pin(s.id) },
        h('span', { class: 'of-spin', text: viewing ? (follow ? '▶' : '📌') : '' }),
        h('span', { class: 'of-stitle', text: title }),
        acc,
        s.live ? h('span', { class: 'of-dot on' + (s.status === 'busy' ? ' busy' : '') }) : h('span', { class: 'of-sago', text: ago(s.last) })));
  }
  function clickFloor(f, cur, open) {
    if (cur) {
      if (open) { closedFloors.add(f.floor); openFloors.delete(f.floor); } else { closedFloors.delete(f.floor); openFloors.add(f.floor); }
      renderSide();
      return;
    }
    closedFloors.delete(f.floor);
    // Among the conversations that show (a hidden floor: any of them).
    const all = (f.sessions || []).slice();
    const shown = all.filter((s) => sessOn(f, s));
    const ss = shown.length ? shown : all;
    const best = ss.find((s) => s.live && s.status === 'busy') || ss.find((s) => s.live) || ss.sort((p, q) => Date.parse(q.last) - Date.parse(p.last))[0];
    if (best) pin(best.id);
  }
  function pin(id) {
    if (!id || (pinned === id && !follow)) return;
    pinned = id; follow = false; remember();
    renderSide();
    kick();
  }
  function setFollow(on) {
    follow = on;
    pinned = on ? '' : (view ? view.id : '');
    remember();
    renderSide();
    kick();
  }

  const STATE_PILL = { working: 'info', thinking: 'violet', talking: 'info', asking: 'warn', error: 'bad', idle: 'off', reporting: 'ok', leaving: 'off', arriving: 'ok' };
  let panelKey = '', peopleKey = '', logKey = '', peopleMeta = [];
  function renderPanel() {
    const v = view;
    const key = JSON.stringify([v && [v.id, v.path, v.account, v.account_index, v.run_state, v.status, v.live, v.source, v.window, v.run_id, v.todos], langOf()]);
    if (key !== panelKey) { panelKey = key; renderFacts(v); }
    // The list is rebuilt when someone changes; the time each has been at it
    // is brought up to date in place (a rebuild every second loses clicks).
    const pk = JSON.stringify([v && v.actors, v && v.more_agents, v && v.say, [...actors.keys()], langOf()]);
    if (pk !== peopleKey) { peopleKey = pk; renderPeople(v); }
    else for (const [el, a] of peopleMeta) { const tx = metaText(a); if (el.textContent !== tx) el.textContent = tx; el.hidden = !tx; }
    const lk = [log.length, log.length && log[0].seq, log.length && log[0].t, langOf()].join('|');
    if (lk !== logKey) { logKey = lk; renderLog(); }
  }
  function renderFacts(v) {
    // The session's facts.
    if (!v) { els.facts.replaceChildren(h('p', { class: 'muted small', text: t('office_empty_title') })); }
    else {
      const row = (label, ...val) => h('div', { class: 'of-fact' }, h('span', { class: 'of-fact-k', text: label }), h('span', { class: 'of-fact-v' }, ...val));
      const acc = h('span', { class: 'of-acc' }); acc.style.background = accountColor(v.account, v.account_index);
      const state = v.run_state ? t('run_' + v.run_state) + (v.status ? ' · ' + t('cl_' + v.status) : '')
        : v.status ? (v.live ? t('cl_' + v.status) : t('office_history')) : v.live ? 'LIVE' : t('office_history');
      fill(els.facts,
        row(t('office_f_project'), h('span', { class: 'mono', text: v.path || '—', title: v.path || '' })),
        row(t('office_f_session'), h('span', { class: 'mono', text: short(v.id), title: v.id })),
        row(t('office_f_account'), v.account ? acc : null, v.account || t('office_no_account')),
        row(t('office_f_state'), state),
        row(t('office_f_source'), t('office_src_' + (v.source || 'cli')) === 'office_src_' + v.source ? v.source : t('office_src_' + (v.source || 'cli'))),
        v.window && v.run_id && opts.focus ? h('div', { class: 'actions start of-fact-act' },
          h('button', { class: 'btn small-btn', text: t('show_window'), title: t('show_window_tip'), onclick: () => opts.focus(v.run_id) })) : null);
    }
    // To-dos from the whiteboard.
    const todos = (v && v.todos) || [];
    els.todoCard.hidden = !todos.length;
    els.todos.replaceChildren(...todos.slice(0, 12).map((td) => h('li', { class: 'of-todo ' + (td.status || '') },
      h('span', { class: 'of-todo-box', text: td.status === 'completed' ? '✓' : td.status === 'in_progress' ? '•' : '' }), h('span', { text: td.text || '' }))));
  }
  function renderPeople(v) {
    const people = [...actors.values()].filter((a) => a.snap && !a.ghost).sort((p, q) => order(p) - order(q));
    const more = moreAgents(v);
    const working = people.filter((a) => a.kind !== 'me').length + more;
    els.peopleN.textContent = v ? t('office_people_n', { n: working }) : '';
    peopleMeta = [];
    fill(els.people, ...people.map((a) => personRow(a)),
      more ? h('div', { class: 'muted small of-pad', text: t('office_more_agents', { n: more }) }) : null,
      people.length ? null : h('p', { class: 'muted small', text: t('office_people_empty') }));
  }
  function renderLog() {
    els.log.replaceChildren(...(log.length ? log.slice(0, 80).map((ev) => logRow(ev)) : [h('p', { class: 'muted small', text: t('office_log_empty') })]));
  }
  function rank(a) { return a.kind === 'me' ? 0 : a.kind === 'claude' ? 1 : 2; }
  function order(a) { return a.kind === 'claude' ? 0 : a.kind === 'me' ? 2 : 1; } // in the people list
  function avatar(a) {
    const av = h('span', { class: 'of-av', text: a.kind === 'claude' ? '✳' : a.kind === 'me' ? '🙂' : (a.snap.label || a.snap.type || '?').slice(0, 1).toUpperCase() });
    av.style.background = a.look.color;
    return av;
  }
  function personRow(a) {
    const s = a.snap;
    const st = s.state || 'idle';
    const room = s.room || '';
    // Claude, its turn over, waits at its desk for its subagents.
    const waits = st === 'idle' && a.kind === 'claude' && room === 'seat' && !!view &&
      (view.actors || []).some((x) => x.kind === 'agent' && x.state !== 'leaving');
    let doing;
    if (st === 'working' && s.tool) doing = (ROOM_ICON[room] || '🔧') + ' ' + t('office_room_' + room) + ' · ' + (s.detail || s.tool);
    else if (st === 'thinking') doing = '💭 ' + t('office_b_think') + '…';
    else if (st === 'asking') doing = '❓ ' + (s.detail || t('office_b_ask'));
    else if (st === 'talking' && view && view.say && view.say.actor === s.id) doing = '💬 ' + view.say.text;
    else if (st === 'error') doing = '⚠️ ' + (s.detail || (s.tool ? t('office_l_fail', { d: s.tool }) : t('office_st_error')));
    else if (st === 'idle') doing = a.kind === 'me' ? '💻 ' + t('office_room_desk') : waits ? '🪑 ' + t('office_wait') : room === 'seat' ? '🪑 ' + t('office_room_seat') : '☕ ' + t('office_room_lounge');
    else doing = (ROOM_ICON[room] || '') + ' ' + (room ? t('office_room_' + room) : '');
    const meta = h('div', { class: 'of-person-meta', text: metaText(a) });
    meta.hidden = !meta.textContent;
    peopleMeta.push([meta, a]);
    const canFocus = a.kind === 'claude' && view && view.window && view.run_id && opts.focus;
    return h('div', { class: 'of-person', title: s.desc || '', onclick: () => { centerOn(a); hilite(a); } },
      avatar(a),
      h('div', { class: 'of-person-body' },
        h('div', { class: 'of-person-head' },
          h('b', { text: nameOf(s) }),
          a.kind === 'agent' && s.type && s.type !== s.label ? h('span', { class: 'muted small', text: s.type }) : null,
          a.kind !== 'me' ? h('span', { class: 'pill ' + (STATE_PILL[st] || 'off'), text: t(waits ? 'office_st_waiting' : 'office_st_' + st) }) : null),
        s.desc && a.kind === 'agent' ? h('div', { class: 'of-person-desc', text: s.desc }) : null,
        h('div', { class: 'of-person-doing', text: doing }),
        meta),
      canFocus ? h('button', { class: 'btn ghost small-btn', text: '↗', title: t('show_window_tip'), onclick: (e) => { e.stopPropagation(); opts.focus(view.run_id); } }) : null);
  }
  // metaText: how many tools someone used and how long they have been at it.
  function metaText(a) {
    const s = a.snap || {};
    const since = s.since ? span(serverNow() - Date.parse(s.since)) : '';
    return [s.tools != null && a.kind !== 'me' ? t(s.tools === 1 ? 'office_tools_n1' : 'office_tools_n', { n: s.tools }) : '', since].filter(Boolean).join(' · ');
  }
  function hilite(a) { if (a.dom) { a.dom.wrap.classList.remove('hi'); void a.dom.wrap.offsetWidth; a.dom.wrap.classList.add('hi'); } }
  function logRow(ev) {
    const w = who(ev.actor, ev);
    const [icon, text, sub] = logLine(ev);
    const dot = h('i', { class: 'of-logdot' }); dot.style.background = w.color;
    return h('div', { class: 'of-log-row ' + ev.kind },
      h('span', { class: 'of-log-t', text: clockOf(ev.t) }),
      h('span', { class: 'of-log-who' }, dot, h('b', { text: w.name })),
      h('span', { class: 'of-log-x' }, h('span', { class: 'of-log-i', text: icon }), ' ', text, sub ? h('span', { class: 'muted', text: ' · ' + sub }) : null));
  }
  function legend() {
    return ROOMS.map((r) => h('div', { class: 'of-leg' }, h('span', { class: 'of-leg-i', text: ROOM_ICON[r] }),
      h('span', {}, h('b', { text: t('office_room_' + r) }), ' ', h('span', { class: 'muted', text: t('office_room_' + r + '_d') }))));
  }
  // fill replaces an element's children, skipping the empty ones.
  function fill(el, ...kids) { el.replaceChildren(...kids.flat().filter((k) => k !== null && k !== undefined && k !== false)); }
  function baseName(p) { return (p || '').replace(/[\\/]+$/, '').split(/[\\/]/).pop() || p || ''; }

  // ---- the long poll ----------------------------------------------------------------------
  let gen = 0, ctrl = null, loaded = false, fails = 0, looping = false;
  const running = () => shown && !destroyed && !document.hidden;
  function kick() {
    gen++;
    if (ctrl) { try { ctrl.abort(); } catch (_) { /* gone */ } ctrl = null; }
    if (running()) poll(gen, true);
  }
  async function poll(my, quick) {
    let wait = !quick;
    looping = true;
    try { await pollLoop(my, wait); } finally { if (my === gen) looping = false; }
  }
  async function pollLoop(my, wait) {
    while (my === gen && running()) {
      ctrl = typeof AbortController === 'function' ? new AbortController() : null;
      const q = 'office?session=' + encodeURIComponent(follow ? '' : pinned) + '&since=' + seq + '&wait=' + (wait ? 1 : 0);
      try {
        const r = await opts.api(q, undefined, ctrl ? ctrl.signal : undefined);
        if (my !== gen) return;
        loaded = true; fails = 0;
        if (notice) { notice = ''; }
        apply(r);
        // The pinned session was /cleared: follow its new id.
        if (!follow && r.view && pinned && r.view.id !== pinned) { pinned = r.view.id; remember(); }
        wait = true;
      } catch (e) {
        if (my !== gen || (e && e.name === 'AbortError')) return;
        let ms;
        if (e.status === 404 && e.code === 'office_off') { notice = 'off'; ms = 4000; }
        else if (e.status === 403) { notice = 'expired'; renderStage(); return; }
        else if (e.status === 400 && pinned) { pinned = ''; follow = true; remember(); ms = 0; }
        else if (!e.status) { // the server is gone, or not there yet
          fails++;
          notice = opts.standalone && fails >= 2 ? 'gone' : 'retry';
          ms = Math.min(15000, 600 * 2 ** Math.min(fails, 5));
        } else { fails++; ms = Math.min(15000, 1000 * 2 ** Math.min(fails, 4)); }
        renderStage();
        await new Promise((res) => setTimeout(res, ms));
        wait = false;
      }
    }
  }

  // ---- building the DOM -------------------------------------------------------------------
  function build(container) {
    const btn = (label, title, on) => h('button', { class: 'of-zbtn', type: 'button', text: label, title, 'aria-label': title, onclick: on });
    canvas = h('canvas', { class: 'of-canvas', 'aria-hidden': 'true' });
    ctx = canvas.getContext('2d');
    els.layer = h('div', { class: 'of-layer' });
    els.more = h('div', { class: 'of-more-tag' }); els.more.hidden = true;
    els.layer.append(els.more);
    els.title = h('div', { class: 'of-title' });
    els.live = h('span', { class: 'of-live pill off' });
    els.pop = opts.popout && !opts.standalone ? h('button', { class: 'btn of-pop', title: t('office_popout_tip'), onclick: () => opts.popout() }, '↗ ', h('span', { text: t('office_popout') })) : null;
    // The buttons that open a side column over the stage, when it is out of
    // the grid (folded, or no room for it).
    const drawerBtn = (k, icon) => {
      const b = h('button', { class: 'btn of-tbtn', type: 'button', 'aria-controls': k === 'l' ? 'of-side' : 'of-panel', 'aria-expanded': 'false',
        onclick: () => openDrawer(k, !lay[k + 'Open']) }, icon + ' ', h('span'));
      b.hidden = true;
      return b;
    };
    els.guideBtn = drawerBtn('l', '🏢');
    els.panelBtn = drawerBtn('r', '☰');
    els.lift = h('div', { class: 'of-lift', 'aria-live': 'polite' });
    els.notice = h('div', { class: 'of-notice' }); els.notice.hidden = true;
    els.retry = h('div', { class: 'of-retry pill warn' }); els.retry.hidden = true;
    els.zoom = h('div', { class: 'of-zoom' },
      btn('+', t('office_zoom_in'), () => zoomAt(1.25, camMid(), cam.h / 2)),
      btn('−', t('office_zoom_out'), () => zoomAt(0.8, camMid(), cam.h / 2)),
      btn('⟲', t('office_zoom_reset'), () => resetCam()));
    els.hint = h('div', { class: 'of-hint' });
    // The bar over the stage: the title gives way to the buttons on the right.
    const bar = h('div', { class: 'of-bar' }, h('div', { class: 'of-topleft' }, els.guideBtn, els.title),
      h('div', { class: 'of-topright' }, els.panelBtn, els.live, els.pop));
    els.stage = h('div', { class: 'of-stage' }, canvas, els.layer, bar, els.lift, els.zoom, els.hint, els.retry, els.notice);
    els.floors = h('div', { class: 'of-floors' });
    els.sideHead = h('div', { class: 'of-side-head' });
    els.side = h('aside', { class: 'of-side card', id: 'of-side', tabindex: '-1' }, els.sideHead, els.floors);
    els.facts = h('div', { class: 'of-facts' });
    els.todos = h('ul', { class: 'of-todos' });
    els.todoTitle = h('h3');
    els.todoCard = h('section', { class: 'card of-card' }, els.todoTitle, els.todos);
    els.peopleN = h('span', { class: 'muted small' });
    els.peopleTitle = h('h3');
    els.people = h('div', { class: 'of-people' });
    els.logTitle = h('h3');
    els.log = h('div', { class: 'of-log' });
    els.legTitle = h('h3');
    els.legend = h('div', { class: 'of-legend' });
    els.panel = h('div', { class: 'of-panel', id: 'of-panel', tabindex: '-1' },
      h('section', { class: 'card of-card' }, els.facts),
      els.todoCard,
      h('section', { class: 'card of-card' }, h('div', { class: 'of-card-head' }, els.peopleTitle, els.peopleN), els.people),
      h('section', { class: 'card of-card of-logcard' }, els.logTitle, els.log),
      h('section', { class: 'card of-card' }, els.legTitle, els.legend));
    const split = (k) => h('div', { class: 'of-split ' + k, role: 'separator', tabindex: '0', 'aria-orientation': 'vertical',
      'aria-controls': k === 'l' ? 'of-side' : 'of-panel', 'aria-valuemin': '0' });
    els.splitL = split('l');
    els.splitR = split('r');
    root = h('div', { class: 'office' + (opts.standalone ? ' standalone' : '') }, els.side, els.splitL, els.stage, els.splitR, els.panel);
    container.append(root);
    texts();
    wire();
    wireSplit(els.splitL, 'l');
    wireSplit(els.splitR, 'r');
    root.addEventListener('keydown', onKey);
    new ResizeObserver(() => resize()).observe(els.stage);
    new ResizeObserver(() => place()).observe(root);
  }
  // texts fills in what depends only on the language.
  let textLang = '';
  function texts() {
    textLang = langOf();
    fill(els.sideHead, h('span', { class: 'of-side-title' }, '🏢 ', h('b', { text: t('office_floor_guide') })), langOf() === 'ko' ? h('span', { class: 'of-side-en', text: 'FLOOR GUIDE' }) : null);
    els.hint.textContent = t('office_hint');
    els.todoTitle.textContent = t('office_todos');
    els.peopleTitle.textContent = t('office_people');
    els.logTitle.textContent = t('office_log');
    els.legTitle.textContent = t('office_legend');
    els.legend.replaceChildren(...legend());
    els.retry.textContent = t('office_reconnect');
    if (els.pop) { els.pop.title = t('office_popout_tip'); els.pop.lastChild.textContent = t('office_popout'); }
    els.guideBtn.lastChild.textContent = t('office_guide_btn');
    els.guideBtn.title = t('office_guide_tip');
    els.panelBtn.lastChild.textContent = t('office_panel_btn');
    els.panelBtn.title = t('office_panel_tip');
    for (const [el, k] of [[els.splitL, 'office_resize_side'], [els.splitR, 'office_resize_panel']]) {
      el.setAttribute('aria-label', t(k));
      el.title = t('office_resize_tip');
    }
    const [zi, zo, zr] = els.zoom.children;
    for (const [b, k] of [[zi, 'office_zoom_in'], [zo, 'office_zoom_out'], [zr, 'office_zoom_reset']]) { b.title = t(k); b.setAttribute('aria-label', t(k)); }
    for (const a of actors.values()) if (a.dom) a.dom.nameKey = '';
    sprites.clear(); sideKey = ''; panelKey = ''; peopleKey = ''; logKey = ''; noticeKey = '';
  }

  // ---- the layout: floor guide | stage | panel --------------------------------------------------
  // The side columns sit beside the stage, each behind a splitter in a 12 px
  // gutter. When the window narrows they give way toward their minimum
  // first; only then does the panel (and in a very narrow window the floor
  // guide too) leave the grid, to open over the stage from a button. Nothing
  // ever stacks under the stage, so the page never scrolls. A folded column
  // is out of the grid by choice; its splitter stays at the edge to bring it
  // back. Widths and folds are kept per kind of window in config.json: the
  // page's own storage is new every launch (its address changes).
  const GUT = 12;
  // The floor guide's default leaves room beside a floor's show-in-the-office
  // box for its longest second line ("5 sessions · active now", and the
  // Korean, which is wider).
  const PANES = { l: { min: 190, def: 244, max: 420 }, r: { min: 240, def: 304, max: 560 } };
  const STAGE_MIN = 460; // with both columns beside it
  const STAGE_MIN_ALONE = 360; // with one: below this it goes too
  const FOLD_AT = 60; // dragged this far under its minimum, a column folds
  // last is the column the user sized last ('l', 'r' or ''): when the two
  // do not fit, the other one gives way, so the width just set stays.
  const pref = { l: 0, r: 0, lFold: false, rFold: false, last: '' }; // widths 0: the default
  const lay = { W: 0, l: PANES.l.def, r: PANES.r.def, lIn: true, rIn: true, lOpen: false, rOpen: false };
  let splitDrag = null;
  const prefW = (k) => clamp(pref[k] || PANES[k].def, PANES[k].min, PANES[k].max);
  const edge = (k, inGrid) => (inGrid ? lay[k] + GUT : pref[k + 'Fold'] ? GUT : 0); // what a side takes of the width
  function loadLayout() {
    const p = (opts.layout && opts.layout()) || {};
    pref.l = Math.max(0, Math.round(+p.left || 0));
    pref.r = Math.max(0, Math.round(+p.right || 0));
    pref.lFold = !!p.left_folded;
    pref.rFold = !!p.right_folded;
    pref.last = p.last === 'l' || p.last === 'r' ? p.last : '';
  }
  // place fits the columns to the office's width. active is the column
  // being resized: the other gives way first. Without one (the window
  // changed, or a redraw), the column being dragged, else the one sized
  // last, keeps its width; before any is, the floor guide does, as the
  // panel is also the first to leave the grid.
  function place(active) {
    if (!root) return;
    const W = root.clientWidth;
    if (!W) return; // not on screen
    const lead = active || (splitDrag && splitDrag.k) || pref.last || 'l';
    let l = prefW('l'), r = prefW('r');
    let lIn = !pref.lFold, rIn = !pref.rFold;
    const stageW = () => W - (lIn ? l + GUT : pref.lFold ? GUT : 0) - (rIn ? r + GUT : pref.rFold ? GUT : 0);
    const shrink = (want) => {
      let need = want - stageW();
      if (need <= 0) return true;
      const room = { l: lIn ? l - PANES.l.min : 0, r: rIn ? r - PANES.r.min : 0 };
      for (const k of lead === 'l' ? ['r', 'l'] : ['l', 'r']) {
        const give = Math.min(need, room[k]);
        if (k === 'l') l -= give; else r -= give;
        need -= give;
      }
      return need < 0.5;
    };
    if (lIn && rIn && !shrink(STAGE_MIN)) { rIn = false; l = prefW('l'); r = prefW('r'); } // the panel goes first
    if (lIn !== rIn && !shrink(STAGE_MIN) && stageW() < STAGE_MIN_ALONE) {
      if (lIn) lIn = false; else rIn = false;
    }
    lay.W = W; lay.l = Math.round(l); lay.r = Math.round(r); lay.lIn = lIn; lay.rIn = rIn;
    if (lIn) lay.lOpen = false;
    if (rIn) lay.rOpen = false;
    const cols = [], areas = [];
    if (lIn) { cols.push(lay.l + 'px', GUT + 'px'); areas.push('side', 'ls'); } else if (pref.lFold) { cols.push(GUT + 'px'); areas.push('ls'); }
    cols.push('minmax(0, 1fr)'); areas.push('stage');
    if (rIn) { cols.push(GUT + 'px', lay.r + 'px'); areas.push('rs', 'panel'); } else if (pref.rFold) { cols.push(GUT + 'px'); areas.push('rs'); }
    root.style.gridTemplateColumns = cols.join(' ');
    root.style.gridTemplateAreas = '"' + areas.join(' ') + '"';
    // A drawer is as wide as the column would be.
    root.style.setProperty('--of-l', prefW('l') + 'px');
    root.style.setProperty('--of-r', prefW('r') + 'px');
    for (const k of ['l', 'r']) {
      const inGrid = lay[k + 'In'];
      root.classList.toggle(k + '-out', !inGrid);
      root.classList.toggle(k + '-fold', !!pref[k + 'Fold']);
      root.classList.toggle(k + '-open', !inGrid && lay[k + 'Open']);
      const b = k === 'l' ? els.guideBtn : els.panelBtn;
      b.hidden = inGrid;
      b.setAttribute('aria-expanded', String(!inGrid && lay[k + 'Open']));
      const sp = k === 'l' ? els.splitL : els.splitR;
      sp.setAttribute('aria-valuemax', String(maxFit(k)));
      sp.setAttribute('aria-valuenow', String(inGrid ? lay[k] : 0));
      sp.setAttribute('aria-valuetext', inGrid ? lay[k] + 'px' : t('office_folded'));
    }
    // An open drawer covers part of the stage: the building moves aside.
    const sw = W - edge('l', lIn) - edge('r', rIn);
    const covers = (k) => (!lay[k + 'In'] && lay[k + 'Open'] ? Math.min(prefW(k), sw - 56) + 16 : 0);
    const il = covers('l'), ir = covers('r');
    if (il !== cam.insetL || ir !== cam.insetR) {
      cam.insetL = il; cam.insetR = ir;
      updateCam();
      cam.zoomingUntil = performance.now() + 200;
      if (running()) draw(performance.now());
    }
  }
  // maxFit is the widest a column can be with the stage still STAGE_MIN wide
  // (and the other column at its minimum).
  function maxFit(k) {
    const o = k === 'l' ? 'r' : 'l';
    const other = lay[o + 'In'] ? PANES[o].min + GUT : pref[o + 'Fold'] ? GUT : 0;
    return clamp(lay.W - STAGE_MIN - GUT - other, PANES[k].min, PANES[k].max);
  }
  function setWidth(k, w) {
    pref.last = k;
    if (w < PANES[k].min - FOLD_AT) pref[k + 'Fold'] = true;
    else { pref[k + 'Fold'] = false; pref[k] = clamp(Math.round(w), PANES[k].min, maxFit(k)); }
    place(k);
  }
  function setFold(k, on) {
    pref[k + 'Fold'] = on;
    place();
    saveLayout();
  }
  let saveTimer = 0;
  function saveLayout() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      if (!opts.saveLayout) return;
      Promise.resolve(opts.saveLayout({ left: pref.l, right: pref.r, left_folded: pref.lFold, right_folded: pref.rFold, last: pref.last })).catch(() => { /* kept for this window */ });
    }, 400);
  }
  function openDrawer(k, on) {
    if (on) lay[(k === 'l' ? 'r' : 'l') + 'Open'] = false; // one at a time
    lay[k + 'Open'] = on;
    place();
    if (on) (k === 'l' ? els.side : els.panel).focus({ preventScroll: true });
  }
  // wireSplit: a splitter drags (the pointer), steps (←/→, Shift for more,
  // Home/End), resets (double-click) and folds (Enter), as the WAI-ARIA
  // window splitter does.
  function wireSplit(el, k) {
    const sign = k === 'l' ? 1 : -1; // the panel grows as its splitter goes left
    let raf2 = 0, want = 0;
    el.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      e.preventDefault();
      el.focus({ preventScroll: true });
      el.setPointerCapture(e.pointerId);
      splitDrag = { k, x: e.clientX, start: lay[k + 'In'] ? lay[k] : 0, was: Object.assign({}, pref), id: e.pointerId };
      root.classList.add('resizing');
      el.classList.add('on');
    });
    el.addEventListener('pointermove', (e) => {
      if (!splitDrag || splitDrag.k !== k) return;
      want = splitDrag.start + (e.clientX - splitDrag.x) * sign;
      if (!raf2) raf2 = requestAnimationFrame(() => { raf2 = 0; if (splitDrag && splitDrag.k === k) setWidth(k, want); });
    });
    const end = () => {
      if (!splitDrag || splitDrag.k !== k) return;
      splitDrag = null;
      root.classList.remove('resizing');
      el.classList.remove('on');
      saveLayout();
    };
    el.addEventListener('pointerup', end);
    el.addEventListener('lostpointercapture', end);
    // Back to its default width, which the other column then makes room for.
    el.addEventListener('dblclick', () => { pref[k] = 0; pref[k + 'Fold'] = false; pref.last = k; place(); saveLayout(); });
    el.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); setFold(k, !pref[k + 'Fold']); return; }
      const cur = lay[k + 'In'] ? lay[k] : 0;
      const step = e.shiftKey ? 64 : 16;
      let w;
      if (e.key === 'ArrowRight') w = cur + step * sign;
      else if (e.key === 'ArrowLeft') w = cur - step * sign;
      else if (e.key === 'Home') w = PANES[k].min;
      else if (e.key === 'End') w = maxFit(k);
      else return;
      e.preventDefault();
      pref[k + 'Fold'] = false;
      pref[k] = clamp(Math.round(w), PANES[k].min, maxFit(k));
      pref.last = k;
      place(k);
      saveLayout();
    });
  }
  // Esc puts back a drag under way, or closes the drawer it was pressed in.
  function onKey(e) {
    if (e.key !== 'Escape') return;
    if (splitDrag) {
      const d = splitDrag;
      Object.assign(pref, d.was);
      splitDrag = null;
      root.classList.remove('resizing');
      for (const sp of [els.splitL, els.splitR]) { sp.classList.remove('on'); try { sp.releasePointerCapture(d.id); } catch (_) { /* released */ } }
      place();
      e.preventDefault();
      return;
    }
    for (const k of ['l', 'r']) {
      const pane = k === 'l' ? els.side : els.panel, b = k === 'l' ? els.guideBtn : els.panelBtn;
      if (lay[k + 'Open'] && !lay[k + 'In'] && (pane.contains(e.target) || e.target === b)) {
        openDrawer(k, false);
        b.focus();
        e.preventDefault();
        return;
      }
    }
  }
  function wire() {
    let drag = null;
    canvas.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      drag = { x: e.clientX, y: e.clientY, px: cam.px, py: cam.py, moved: false, id: e.pointerId };
      canvas.setPointerCapture(e.pointerId);
    });
    canvas.addEventListener('pointermove', (e) => {
      if (drag) {
        const dx = e.clientX - drag.x, dy = e.clientY - drag.y;
        if (Math.abs(dx) + Math.abs(dy) > 4) drag.moved = true;
        if (drag.moved) { cam.px = drag.px + dx; cam.py = drag.py + dy; updateCam(); els.stage.classList.add('dragging'); }
        return;
      }
      const a = hit(e);
      canvas.classList.toggle('pointer', !!(a && canFocusActor(a)));
      canvas.title = a ? nameOf(a.snap) + (canFocusActor(a) ? ' — ' + t('office_click_focus') : '') : '';
    });
    const end = (e) => {
      if (!drag) return;
      const was = drag; drag = null;
      els.stage.classList.remove('dragging');
      try { canvas.releasePointerCapture(e.pointerId); } catch (_) { /* released */ }
      if (!was.moved && e.type === 'pointerup') {
        const a = hit(e);
        if (a) { hilite(a); if (canFocusActor(a)) opts.focus(view.run_id); }
      }
    };
    canvas.addEventListener('pointerup', end);
    canvas.addEventListener('pointercancel', end);
    canvas.addEventListener('wheel', (e) => {
      e.preventDefault();
      const r = canvas.getBoundingClientRect();
      zoomAt(Math.exp(-e.deltaY * 0.0015), e.clientX - r.left, e.clientY - r.top);
    }, { passive: false });
    canvas.addEventListener('dblclick', () => resetCam());
    document.addEventListener('visibilitychange', () => { if (destroyed) return; if (running()) start(); else stop(); });
  }
  function canFocusActor(a) { return a.kind === 'claude' && !a.ghost && view && view.window && view.run_id && opts.focus; }
  function hit(e) {
    const r = canvas.getBoundingClientRect();
    const [wx, wy] = toWorld(e.clientX - r.left, e.clientY - r.top);
    let best = null;
    for (const a of actors.values()) {
      if (a.alpha < 0.5 || (a.y < 0 && lift.open < 0.5)) continue; // behind closed elevator doors
      const [ax, ay] = pt(a.x, a.y);
      if (wx > ax - 12 && wx < ax + 12 && wy > ay - 50 && wy < ay + 4 && (!best || a.x + a.y > best.x + best.y)) best = a;
    }
    return best;
  }
  function theme() {
    const night = document.documentElement.dataset.theme === 'dark';
    P = night ? NIGHT : DAY;
    themeKey = night ? 'night' : 'day';
    sprites.clear();
  }
  function resize() {
    if (!els.stage) return;
    const r = els.stage.getBoundingClientRect();
    cam.w = Math.max(1, Math.round(r.width)); cam.h = Math.max(1, Math.round(r.height));
    const dpr = cam.dpr = window.devicePixelRatio || 1; // the camera draws at the canvas's own scale
    canvas.width = Math.round(cam.w * dpr); canvas.height = Math.round(cam.h * dpr);
    canvas.style.width = cam.w + 'px'; canvas.style.height = cam.h + 'px';
    updateCam();
    cam.zoomingUntil = performance.now() + 200;
    fitHeight();
    if (running()) draw(performance.now());
  }
  // fitHeight: in the tab, the office fills the window below the tabs, and
  // no more: the page never scrolls (the columns scroll inside).
  function fitHeight() {
    if (!root || opts.standalone) return;
    const top = root.getBoundingClientRect().top + window.scrollY;
    const hgt = Math.max(260, Math.round(window.innerHeight - top - 14));
    root.style.setProperty('--of-h', hgt + 'px');
  }
  function ride(from, to) {
    const up = to > from;
    lift.arrow = up ? '▲' : '▼'; lift.arrowUntil = performance.now() + 1800;
    els.lift.textContent = t('office_lift', { dir: up ? '▲' : '▼', n: to });
    els.lift.classList.remove('on'); void els.lift.offsetWidth; els.lift.classList.add('on');
    els.stage.classList.remove('riding'); void els.stage.offsetWidth; els.stage.classList.add('riding');
  }
  function start() {
    if (!raf) { lastTs = 0; raf = requestAnimationFrame(frame); }
    if (!looping) kick();
  }
  function stop() {
    gen++; looping = false;
    if (ctrl) { try { ctrl.abort(); } catch (_) { /* gone */ } ctrl = null; }
    if (raf) { cancelAnimationFrame(raf); raf = 0; }
  }

  window.BatonOffice = {
    mount(container, o) {
      opts = o || {};
      h = opts.h || window.h;
      t = opts.t || ((k) => k);
      if (!opts.api && typeof window.api === 'function') opts.api = window.api;
      destroyed = false;
      theme();
      loadLayout();
      build(container);
      window.addEventListener('resize', fitHeight);
    },
    show() {
      if (!root) return;
      const was = shown;
      shown = true;
      if (textLang !== langOf()) { texts(); renderAll(); }
      // Banners above may have come or gone since the last redraw.
      fitHeight();
      place();
      if (!was) resize();
      if (running()) start();
    },
    hide() {
      shown = false;
      stop();
    },
    destroy() {
      destroyed = true; shown = false;
      stop();
      window.removeEventListener('resize', fitHeight);
      for (const a of [...actors.values()]) dropActor(a);
      if (root) root.remove();
      root = null; els = {}; sprites.clear();
    },
  };
})();
