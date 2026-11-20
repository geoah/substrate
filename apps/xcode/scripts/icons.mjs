// Draws the Substrate family's app icons: Substrate, Substrate Sync,
// Substrate Chat, Substrate Workouts, Substrate Tasks, Substrate Healthspan
// and the template's. Apps built on a
// substrate elsewhere copy this file and add
// their own entry to APPS (and a glyph to GLYPHS), so their icons share the
// frame.
//
// Every icon shares one frame: the app's glyph on a pale gradient, and a band
// across the bottom, a deeper shade of the background, that carries the
// substrate mark, three stacked layers, in the app's colour. Glyphs are close shades of the
// app's colour with a light frosting. Each app has three iOS appearances:
// light, dark and tinted (greyscale, which iOS colours); an app on Android
// has an adaptive icon made of the same parts (below).
//
// PNGs come from headless Chrome: set CHROME to a Chrome or
// chrome-headless-shell binary, or have Chrome or Playwright's headless shell
// installed. Usage is at the bottom of the file.

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';

const r = (n) => +(+n).toFixed(1);
const mix = (h1, h2, t) => {
  const x = parseInt(h1.slice(1), 16), y = parseInt(h2.slice(1), 16);
  return '#' + [16, 8, 0].map((sh) => Math.round(((x >> sh) & 255) * (1 - t) + ((y >> sh) & 255) * t).toString(16).padStart(2, '0')).join('');
};

// Per app: pale background (top, bottom) and the glyph's light and deep colour.
export const APPS = {
  substrate: { name: 'Substrate', bg: ['#f5f9ff', '#d9e7ff'], hi: '#86bbff', lo: '#2d5fe6', glyph: 'console' },
  sync: { name: 'Substrate Sync', bg: ['#f2fcf8', '#d3f2e5'], hi: '#72e2bb', lo: '#0f936d', glyph: 'cycle' },
  chat: { name: 'Substrate Chat', bg: ['#f8f5ff', '#e4dcfd'], hi: '#bba2ff', lo: '#6a3fe0', glyph: 'chat' },
  workouts: { name: 'Substrate Workouts', bg: ['#fff8f2', '#ffdfcc'], hi: '#ffab72', lo: '#e3471f', glyph: 'dumbbell' },
  tasks: { name: 'Substrate Tasks', bg: ['#fff5f7', '#ffdbe4'], hi: '#ff9bb2', lo: '#d92c58', glyph: 'checklist' },
  healthspan: { name: 'Substrate Healthspan', bg: ['#f7fcf0', '#e0f2c9'], hi: '#a9de6c', lo: '#4a9416', glyph: 'pulse' },
  // What `mise run apps:new` copies: grey, so an app nobody has given its own
  // colour yet looks unfinished rather than like Tasks.
  template: { name: 'Substrate Template', bg: ['#f7f8fa', '#e1e5ea'], hi: '#aab4c0', lo: '#5f6b7a', glyph: 'box' },
};

// Glyphs are drawn around (512, 470), about 560 units across. Each part has a
// level: front, mid or back. Levels are close shades of the app's colour,
// drawn as frosted glass: a soft top-to-bottom tint, a thin light rim, and
// one soft shadow under the whole glyph.
const rhombus = (y, hw = 250, hh = 125) => `M${512 - hw},${y} L512,${y - hh} L${512 + hw},${y} L512,${y + hh} Z`;
const rect = (x, y, w, h, rr) => `M${x + rr},${y} H${x + w - rr} A${rr},${rr} 0 0 1 ${x + w},${y + rr} V${y + h - rr} A${rr},${rr} 0 0 1 ${x + w - rr},${y + h} H${x + rr} A${rr},${rr} 0 0 1 ${x},${y + h - rr} V${y + rr} A${rr},${rr} 0 0 1 ${x + rr},${y} Z`;
const circle = (cx, cy, rr) => `M${cx - rr},${cy} A${rr},${rr} 0 1 0 ${cx + rr},${cy} A${rr},${rr} 0 1 0 ${cx - rr},${cy} Z`;
const spark = (cx, cy, s) => `M${cx},${cy - s} C${cx + s * 0.12},${cy - s * 0.12} ${cx + s * 0.12},${cy - s * 0.12} ${cx + s},${cy} C${cx + s * 0.12},${cy + s * 0.12} ${cx + s * 0.12},${cy + s * 0.12} ${cx},${cy + s} C${cx - s * 0.12},${cy + s * 0.12} ${cx - s * 0.12},${cy + s * 0.12} ${cx - s},${cy} C${cx - s * 0.12},${cy - s * 0.12} ${cx - s * 0.12},${cy - s * 0.12} ${cx},${cy - s} Z`;
// Parts: [level, path] fills, or [level, path, strokeWidth] strokes. Level
// "spark" is a small mark drawn on top in a contrasting colour, filled or
// stroked.
const GLYPHS = {
  // A window of records: the console, where everything in a substrate is seen.
  console: () => [['back', rect(232, 246, 560, 448, 72)],
    ...[370, 470, 570].flatMap((y, i) => [['mid', circle(326, y, 36)], ['front', rect(390, y - 34, i < 2 ? 340 : 240, 68, 34)]])],
  // Two arrows chasing round a core: records carried in.
  cycle() {
    const cx = 512, cy = 470, R = 210, at = (a, rr = R) => [cx + rr * Math.cos((a * Math.PI) / 180), cy + rr * Math.sin((a * Math.PI) / 180)];
    const parts = [['back', circle(cx, cy, 88)]];
    for (const [a0, a1] of [[200, 318], [20, 138]]) {
      const [x0, y0] = at(a0), [x1, y1] = at(a1), [hx, hy] = at(a1 + 18), n0 = at(a1, R - 74), n1 = at(a1, R + 74);
      parts.push(['front', `M${r(x0)},${r(y0)} A${R},${R} 0 0 1 ${r(x1)},${r(y1)}`, 72]);
      parts.push(['front', `M${r(n0[0])},${r(n0[1])} L${r(hx)},${r(hy)} L${r(n1[0])},${r(n1[1])} Z`]);
    }
    return parts;
  },
  // A dumbbell: two plates a side on a bar.
  dumbbell: () => [['back', rect(300, 446, 424, 48, 12)], ['mid', rect(166, 370, 74, 200, 24)], ['mid', rect(784, 370, 74, 200, 24)],
    ['front', rect(250, 300, 124, 340, 28)], ['front', rect(650, 300, 124, 340, 28)]],
  // A speech bubble with a spark: the agent you talk to.
  chat: () => [['back', rect(392, 236, 370, 260, 110)],
    ['front', 'M358,278 L638,278 C710,278 758,326 758,398 L758,518 C758,590 710,638 638,638 L456,638 L352,712 L374,638 L358,638 C286,638 238,590 238,518 L238,398 C238,326 286,278 358,278 Z'],
    ['spark', spark(484, 458, 104)], ['spark', spark(604, 382, 42)]],
  // A card of three rows, the first ticked: the list of things to do. Each
  // row's mark sits clear of its line, and the tick is a spark, so it still
  // reads as ticked at 29 px.
  checklist: () => [['back', rect(252, 214, 520, 520, 104)],
    ['front', rect(510, 330, 202, 60, 30)], ['front', rect(510, 462, 202, 60, 30)], ['front', rect(510, 594, 202, 60, 30)],
    ['front', circle(386, 492, 32)], ['front', circle(386, 624, 32)],
    ['spark', 'M332,364 L370,402 L440,322', 70]],
  // A heart with a pulse across it: the body's measures over time.
  pulse: () => [['front', 'M512,712 C330,598 236,500 236,388 C236,300 302,236 384,236 C440,236 486,264 512,306 C538,264 584,236 640,236 C722,236 788,300 788,388 C788,500 694,598 512,712 Z'],
    ['spark', 'M300,468 L414,468 L456,384 L520,566 L568,420 L600,468 L724,468', 46]],
  // A lidded box with a label: any app's things, until it draws its own.
  box: () => [['back', rect(276, 330, 472, 380, 64)], ['mid', rect(240, 240, 544, 130, 52)],
    ['front', rect(420, 450, 184, 64, 32)]],
};

// Each level's gradient (top, bottom), the rim's opacity and the spark's colour, by appearance.
const glyphShades = (mode, p) => mode === 'light'
  ? { front: [mix(p.lo, '#ffffff', 0.1), p.lo], mid: [mix(p.lo, p.hi, 0.36), mix(p.lo, p.hi, 0.24)], back: [mix(p.lo, p.hi, 0.6), mix(p.lo, p.hi, 0.48)], rim: 0.35, spark: '#ffffff' }
  : mode === 'dark'
    ? { front: [mix(p.hi, '#ffffff', 0.1), mix(p.hi, p.lo, 0.35)], mid: [mix(p.hi, p.lo, 0.4), mix(p.hi, p.lo, 0.55)], back: [mix(p.hi, p.lo, 0.62), mix(p.hi, p.lo, 0.78)], rim: 0.3, spark: p.lo }
    : { front: ['#f0f0f0', '#cfcfcf'], mid: ['#bdbdbd', '#a6a6a6'], back: ['#999999', '#828282'], rim: 0.35, spark: '#3a3a3e' };

function drawGlyph(name, mode, p, id) {
  const shades = glyphShades(mode, p);
  let defs = '', body = '';
  for (const lv of ['front', 'mid', 'back']) defs += `<linearGradient id="${id}${lv}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${shades[lv][0]}"/><stop offset="1" stop-color="${shades[lv][1]}"/></linearGradient>`;
  defs += `<filter id="${id}sh" x="-30%" y="-30%" width="160%" height="170%"><feGaussianBlur in="SourceAlpha" stdDeviation="24"/><feOffset dy="20" result="b"/><feFlood flood-color="${mode === 'light' ? p.lo : '#000000'}" flood-opacity="${mode === 'light' ? 0.13 : 0.4}"/><feComposite in2="b" operator="in" result="s"/><feMerge><feMergeNode in="s"/><feMergeNode in="SourceGraphic"/></feMerge></filter>`;
  for (const [lv, d, w] of GLYPHS[name]()) {
    if (lv === 'spark') { body += w ? `<path d="${d}" fill="none" stroke="${shades.spark}" stroke-width="${w}" stroke-linecap="round" stroke-linejoin="round"/>` : `<path d="${d}" fill="${shades.spark}"/>`; continue; }
    const op = 1;
    if (w) { body += `<path d="${d}" fill="none" stroke="url(#${id}${lv})" stroke-width="${w}" stroke-linecap="round" opacity="${op}"/>`; continue; }
    body += `<path d="${d}" fill="url(#${id}${lv})" stroke="url(#${id}${lv})" stroke-width="10" stroke-linejoin="round" opacity="${op}"/>` +
      `<path d="${d}" fill="none" stroke="#ffffff" stroke-opacity="${shades.rim}" stroke-width="3" stroke-linejoin="round"/>`;
  }
  return `<defs>${defs}</defs><g filter="url(#${id}sh)">${body}</g>`;
}

// The substrate mark in the band: three stacked layers in the band's ink,
// each cut away where the one above it sits, so the band shows between them.
function bandMark(id) {
  const layers = [805, 845, 885].map((y) => rhombus(y, 90, 45));
  const cut = (n, ls) => `<mask id="${id}${n}" maskUnits="userSpaceOnUse" x="0" y="0" width="1024" height="1024"><rect width="1024" height="1024" fill="#fff"/>${ls.map((d) => `<path d="${d}" fill="#000" stroke="#000" stroke-width="14" stroke-linejoin="round"/>`).join('')}</mask>`;
  return {
    defs: cut('c1', layers.slice(0, 1)) + cut('c2', layers.slice(0, 2)),
    body: `<path d="${layers[2]}" fill="url(#${id}in)" opacity="0.55" mask="url(#${id}c2)"/>` +
      `<path d="${layers[1]}" fill="url(#${id}in)" opacity="0.78" mask="url(#${id}c1)"/>` +
      `<path d="${layers[0]}" fill="url(#${id}in)"/>`,
  };
}

let uid = 0;

/**
 * One icon as SVG. mode: light | dark | tinted. Full bleed and square, as
 * Xcode wants it; mask: true rounds it like iOS does (previews and favicons).
 */
export function iconSVG(app, mode = 'light', { size = 1024, mask = false } = {}) {
  const a = APPS[app], id = 'i' + uid++;
  let bg0 = a.bg[0], bg1 = a.bg[1], glow = a.hi;
  // The band is a deeper shade of the background, the mark in the app's colour.
  let band0 = mix(a.bg[1], a.hi, 0.35), band1 = mix(a.bg[1], a.hi, 0.55), ink0 = a.lo, ink1 = mix(a.lo, '#000000', 0.25), edge = 0.7;
  if (mode === 'dark' || mode === 'tinted') {
    bg0 = '#2a2a30'; bg1 = '#0b0b0d';
    band0 = mix('#1d1d22', a.lo, 0.22); band1 = mix('#101013', a.lo, 0.18); ink0 = a.hi; ink1 = a.lo; edge = 0.12;
  }
  if (mode === 'tinted') {
    glow = '#ffffff';
    band0 = '#2b2b2f'; band1 = '#1c1c1f'; ink0 = '#f2f2f2'; ink1 = '#9a9a9a';
  }
  const defs = [
    `<linearGradient id="${id}bg" x1="0" y1="0" x2="0.35" y2="1"><stop offset="0" stop-color="${bg0}"/><stop offset="1" stop-color="${bg1}"/></linearGradient>`,
    `<radialGradient id="${id}gw" cx=".5" cy=".3" r=".62"><stop offset="0" stop-color="${glow}" stop-opacity="${mode === 'light' ? 0.35 : 0.22}"/><stop offset="1" stop-color="${glow}" stop-opacity="0"/></radialGradient>`,
    `<linearGradient id="${id}bd" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${band0}"/><stop offset="1" stop-color="${band1}"/></linearGradient>`,
    `<linearGradient id="${id}in" gradientUnits="userSpaceOnUse" x1="0" y1="760" x2="0" y2="930"><stop offset="0" stop-color="${ink0}"/><stop offset="1" stop-color="${ink1}"/></linearGradient>`,
  ];
  const mark = bandMark(id);
  defs.push(mark.defs);
  if (mask) defs.push(`<clipPath id="${id}m"><rect width="1024" height="1024" rx="230"/></clipPath>`);
  const glyph = `<g transform="translate(512 352) scale(0.86) translate(-512 -470)">${drawGlyph(a.glyph, mode, { hi: a.hi, lo: a.lo }, id + 'g')}</g>`;
  const band = `<rect x="-20" y="690" width="1064" height="400" fill="url(#${id}bd)"/><rect x="-20" y="690" width="1064" height="3" fill="#fff" opacity="${edge}"/>`;
  const body = `<rect width="1024" height="1024" fill="url(#${id}bg)"/><rect width="1024" height="1024" fill="url(#${id}gw)"/>${glyph}${band}${mark.body}`;
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 1024 1024"><defs>${defs.join('')}</defs>${mask ? `<g clip-path="url(#${id}m)">${body}</g>` : body}</svg>`;
}

/** The macOS tile: the icon inset to 824 px with rounded corners and a drop shadow. */
export function macSVG(app, size) {
  const inner = iconSVG(app, 'light', { size: 824, mask: true });
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 1024 1024"><defs><filter id="ms" x="-10%" y="-10%" width="120%" height="125%"><feDropShadow dx="0" dy="10" stdDeviation="12" flood-opacity=".3"/></filter></defs><g filter="url(#ms)" transform="translate(100 100)">${inner}</g></svg>`;
}

function chrome() {
  const candidates = [process.env.CHROME,
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    ...(() => { const d = path.join(os.homedir(), '.cache/ms-playwright'); try { return fs.readdirSync(d).filter((n) => n.startsWith('chromium_headless_shell')).sort().reverse().map((n) => path.join(d, n, 'chrome-headless-shell-linux64/chrome-headless-shell')); } catch { return []; } })(),
    '/usr/bin/chromium', '/usr/bin/google-chrome'];
  const hit = candidates.find((c) => c && fs.existsSync(c));
  if (!hit) throw new Error('no Chrome found: set CHROME to a Chrome or chrome-headless-shell binary');
  return hit;
}

export function renderPNG(svg, px, out, { transparent = false } = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'icon-'));
  const html = path.join(dir, 'icon.html');
  fs.writeFileSync(html, `<html><body style="margin:0;background:${transparent ? 'transparent' : '#000'}">${svg.replace(/width="\d+" height="\d+"/, `width="${px}" height="${px}"`)}</body></html>`);
  const args = ['--headless', '--no-sandbox', '--hide-scrollbars', `--screenshot=${path.resolve(out)}`, `--window-size=${px},${px}`, `file://${html}`];
  if (transparent) args.splice(3, 0, '--default-background-color=00000000');
  execFileSync(chrome(), args, { stdio: 'ignore' });
  fs.rmSync(dir, { recursive: true });
}

/** Writes an Xcode AppIcon set: light, dark and tinted iOS icons, and with mac the macOS sizes. */
export function writeAppIconSet(app, dir, { mac = false } = {}) {
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  const images = [];
  for (const mode of ['light', 'dark', 'tinted']) {
    const file = mode === 'light' ? 'icon-1024.png' : `icon-1024-${mode}.png`;
    renderPNG(iconSVG(app, mode), 1024, path.join(dir, file));
    const img = { filename: file, idiom: 'universal', platform: 'ios', size: '1024x1024' };
    if (mode !== 'light') img.appearances = [{ appearance: 'luminosity', value: mode }];
    images.push(img);
  }
  if (mac) {
    for (const pt of [16, 32, 128, 256, 512]) for (const scale of [1, 2]) {
      const file = `mac-${pt}@${scale}x.png`;
      renderPNG(macSVG(app, pt * scale), pt * scale, path.join(dir, file), { transparent: true });
      images.push({ filename: file, idiom: 'mac', scale: `${scale}x`, size: `${pt}x${pt}` });
    }
  }
  fs.writeFileSync(path.join(dir, 'Contents.json'), JSON.stringify({ images, info: { author: 'xcode', version: 1 } }, null, 2) + '\n');
}

// --- Android: the adaptive icon -------------------------------------------
//
// An adaptive icon is three 108 dp layers the launcher masks to its own
// shape (a circle, a squircle, a rounded square) inside the inner 72 dp, and
// may move for parallax: a background, a foreground, and a monochrome layer
// Android 13 and later tint for themed icons. The 1024 px frame maps onto
// that inner 72 dp, so the masked icon is the light iOS icon: the background
// layer is the pale gradient, its glow and the band, full bleed; the
// foreground is the glyph and the mark, which sit inside the 66 dp safe
// zone every mask keeps. The monochrome layer is the glyph's silhouette (its
// back level faint, the rest whole) and the mark. All three are vector
// drawables, written from one model of shapes that previewSVG draws too, so
// a preview is what the XML holds. A vector drawable has no blur, so the
// glyph's soft shadow is three widening silhouettes at a low opacity.

// The model: a group { g: {tx, ty, s, px, py}, kids } maps a point p to
// (p - pivot) * s + pivot + t, as a vector drawable's group does; a shape
// { d, fill, stroke, sw, cap, join } paints with a colour '#rrggbb', a
// translucent one { color, a }, or a gradient { lin: [x1, y1, x2, y2] } or
// { rad: [cx, cy, r] } with stops [[offset, colour, alpha]], in the shape's
// own coordinates.
const VIEW = 1536; // 108 dp, in units of the 1024 px frame, which is 72 dp
const INSET = (VIEW - 1024) / 2;

// The bounding box of a path in the subset the glyphs use (M L H V C A Z,
// absolute): SVG's objectBoundingBox, which a level's gradient spans.
function bbox(d) {
  const t = d.match(/[MLHVCAZ]|-?\d*\.?\d+(?:e-?\d+)?/gi);
  let i = 0, x = 0, y = 0, cmd = '';
  const xs = [], ys = [], at = (px, py) => { xs.push(px); ys.push(py); };
  const n = () => +t[i++];
  while (i < t.length) {
    if (/[A-Z]/i.test(t[i])) cmd = t[i++];
    if (cmd === 'Z') continue;
    if (cmd === 'M' || cmd === 'L') { x = n(); y = n(); at(x, y); }
    else if (cmd === 'H') { x = n(); at(x, y); }
    else if (cmd === 'V') { y = n(); at(x, y); }
    else if (cmd === 'C') {
      const [x1, y1, x2, y2, x3, y3] = [n(), n(), n(), n(), n(), n()];
      for (let k = 0; k <= 32; k++) { const u = k / 32, v = 1 - u; at(v * v * v * x + 3 * v * v * u * x1 + 3 * v * u * u * x2 + u * u * u * x3, v * v * v * y + 3 * v * v * u * y1 + 3 * v * u * u * y2 + u * u * u * y3); }
      x = x3; y = y3;
    } else if (cmd === 'A') {
      // Circular arcs only (rx = ry, no rotation), sampled from the centre.
      const rr = n(); n(); n(); const large = n(), sweep = n(), x1 = n(), y1 = n();
      const mx = (x + x1) / 2, my = (y + y1) / 2, dx = x1 - x, dy = y1 - y, q = Math.hypot(dx, dy) / 2;
      const h = Math.sqrt(Math.max(0, rr * rr - q * q)) * (large === sweep ? -1 : 1);
      const cx = mx - (h * dy) / (2 * q || 1), cy = my + (h * dx) / (2 * q || 1);
      let a0 = Math.atan2(y - cy, x - cx), a1 = Math.atan2(y1 - cy, x1 - cx);
      if (sweep && a1 < a0) a1 += 2 * Math.PI;
      if (!sweep && a1 > a0) a1 -= 2 * Math.PI;
      for (let k = 0; k <= 64; k++) { const a = a0 + ((a1 - a0) * k) / 64; at(cx + rr * Math.cos(a), cy + rr * Math.sin(a)); }
      x = x1; y = y1;
    } else throw new Error(`bbox: no ${cmd} in ${d.slice(0, 40)}`);
  }
  return [Math.min(...xs), Math.min(...ys), Math.max(...xs), Math.max(...ys)];
}

const vertical = (d, [c0, c1]) => { const [, y0, , y1] = bbox(d); return { lin: [0, r(y0), 0, r(y1)], stops: [[0, c0, 1], [1, c1, 1]] }; };
const ALL = { tx: 0, ty: 0, s: 1, px: 0, py: 0 };
const group = (g, kids) => ({ g: { ...ALL, ...g }, kids });
// The glyph's place in the frame: translate(512 352) scale(0.86) translate(-512 -470).
const glyphGroup = (kids) => group({ px: 512, py: 470, s: 0.86, ty: 352 - 470 }, kids);
// The glyph and the mark, drawn a little smaller about the centre than
// on iOS: a circle cuts the corners a rounded square keeps, and the glyph's
// top corners must stay inside the 66 dp safe zone. The mark stays in
// the band.
const FIT = { px: 512, py: 512, s: 0.88 };

function adaptiveLayers(app) {
  const a = APPS[app], p = { hi: a.hi, lo: a.lo }, sh = glyphShades('light', p), parts = GLYPHS[a.glyph]();
  const band0 = mix(a.bg[1], a.hi, 0.35), band1 = mix(a.bg[1], a.hi, 0.55);
  const background = group({ tx: INSET, ty: INSET }, [
    { d: `M${-INSET},${-INSET} H${1024 + INSET} V${1024 + INSET} H${-INSET} Z`, fill: { lin: [0, 0, 358.4, 1024], stops: [[0, a.bg[0], 1], [1, a.bg[1], 1]] } },
    { d: `M${-INSET},${-INSET} H${1024 + INSET} V${1024 + INSET} H${-INSET} Z`, fill: { rad: [512, 307.2, 634.9], stops: [[0, a.hi, 0.35], [1, a.hi, 0]] } },
    { d: `M${-INSET},690 H${1024 + INSET} V${1024 + INSET} H${-INSET} Z`, fill: { lin: [0, 690, 0, 1090], stops: [[0, band0, 1], [1, band1, 1]] } },
    { d: `M${-INSET},690 H${1024 + INSET} V693 H${-INSET} Z`, fill: { color: '#ffffff', a: 0.7 } },
  ]);
  const shadow = [];
  for (const [extra, alpha] of [[44, 0.04], [22, 0.045], [0, 0.05]]) {
    for (const [lv, d, w] of parts) {
      const width = (w || (lv === 'spark' ? 0 : 10)) + extra;
      shadow.push({ d, fill: w ? null : { color: p.lo, a: alpha }, stroke: width ? { color: p.lo, a: alpha } : null, sw: width, cap: 'round', join: 'round' });
    }
  }
  const glyph = [group({ ty: 20 }, shadow)];
  for (const [lv, d, w] of parts) {
    if (lv === 'spark') { glyph.push(w ? { d, stroke: sh.spark, sw: w, cap: 'round', join: 'round' } : { d, fill: sh.spark }); continue; }
    const paint = vertical(d, sh[lv]);
    if (w) { glyph.push({ d, stroke: paint, sw: w, cap: 'round' }); continue; }
    glyph.push({ d, fill: paint, stroke: paint, sw: 10, join: 'round' }, { d, stroke: { color: '#ffffff', a: sh.rim }, sw: 3, join: 'round' });
  }
  // The mark as bandMark draws it, top layer solid and the two under it
  // lighter. A vector drawable has no masks, so each layer is cut from the
  // one below by an outline in the band's colour where it sits.
  const layers = [805, 845, 885].map((y) => rhombus(y, 90, 45));
  const ink = (alpha) => ({ lin: [0, 760, 0, 930], stops: [[0, a.lo, alpha], [1, mix(a.lo, '#000000', 0.25), alpha]] });
  const cut = mix(band0, band1, 0.39);
  const mark = [
    { d: layers[2], fill: ink(0.55) },
    { d: layers[1], stroke: cut, sw: 14, join: 'round' },
    { d: layers[1], fill: ink(0.78) },
    { d: layers[0], stroke: cut, sw: 14, join: 'round' },
    { d: layers[0], fill: ink(1) },
  ];
  const foreground = group({ tx: INSET, ty: INSET }, [group(FIT, [glyphGroup(glyph), ...mark])]);
  // The silhouette: whatever the system tints it, the back level reads as the
  // card behind the marks, and a spark is cut back to the tint like the rest.
  const mono = [];
  for (const [lv, d, w] of parts) {
    const paint = lv === 'back' ? { color: '#ffffff', a: 0.42 } : '#ffffff';
    mono.push(w ? { d, stroke: paint, sw: w, cap: 'round', join: 'round' } : { d, fill: paint, stroke: paint, sw: 10, join: 'round' });
  }
  const monoMark = [
    { d: layers[2], fill: { color: '#ffffff', a: 0.55 } },
    { d: layers[1], fill: { color: '#ffffff', a: 0.78 } },
    { d: layers[0], fill: '#ffffff' },
  ];
  const monochrome = group({ tx: INSET, ty: INSET }, [group(FIT, [glyphGroup(mono), ...monoMark])]);
  return { background, foreground, monochrome };
}

const argb = (color, alpha = 1) => '#' + Math.round(alpha * 255).toString(16).padStart(2, '0').toUpperCase() + color.slice(1).toUpperCase();
const paintOf = (pt) => (typeof pt === 'string' ? { color: pt, a: 1 } : pt);

function vectorXML(node, pad = '    ') {
  if (node.g) {
    const { tx, ty, s, px, py } = node.g;
    const attrs = [px && `android:pivotX="${px}"`, py && `android:pivotY="${py}"`, s !== 1 && `android:scaleX="${s}" android:scaleY="${s}"`, tx && `android:translateX="${tx}"`, ty && `android:translateY="${ty}"`].filter(Boolean).join(' ');
    return `${pad}<group${attrs ? ' ' + attrs : ''}>\n${node.kids.map((k) => vectorXML(k, pad + '    ')).join('')}${pad}</group>\n`;
  }
  const attrs = [`android:pathData="${node.d}"`], inner = [];
  for (const [key, pt] of [['fillColor', node.fill], ['strokeColor', node.stroke]]) {
    if (!pt) continue;
    const q = paintOf(pt);
    if (q.color) { attrs.push(`android:${key}="${argb(q.color, q.a)}"`); continue; }
    const geo = q.lin
      ? `android:type="linear" android:startX="${q.lin[0]}" android:startY="${q.lin[1]}" android:endX="${q.lin[2]}" android:endY="${q.lin[3]}"`
      : `android:type="radial" android:centerX="${q.rad[0]}" android:centerY="${q.rad[1]}" android:gradientRadius="${q.rad[2]}"`;
    inner.push(`${pad}    <aapt:attr name="android:${key}">\n${pad}        <gradient ${geo}>\n${q.stops.map(([o, c, al]) => `${pad}            <item android:offset="${o}" android:color="${argb(c, al)}" />\n`).join('')}${pad}        </gradient>\n${pad}    </aapt:attr>\n`);
  }
  if (node.stroke) {
    attrs.push(`android:strokeWidth="${node.sw}"`);
    if (node.cap) attrs.push(`android:strokeLineCap="${node.cap}"`);
    if (node.join) attrs.push(`android:strokeLineJoin="${node.join}"`);
  }
  return inner.length ? `${pad}<path ${attrs.join(' ')}>\n${inner.join('')}${pad}</path>\n` : `${pad}<path ${attrs.join(' ')} />\n`;
}

function vectorDrawable(node) {
  return `<?xml version="1.0" encoding="utf-8"?>\n<!-- Generated by apps/xcode/scripts/icons.mjs; do not edit. -->\n<vector xmlns:android="http://schemas.android.com/apk/res/android" xmlns:aapt="http://schemas.android.com/aapt" android:width="108dp" android:height="108dp" android:viewportWidth="${VIEW}" android:viewportHeight="${VIEW}">\n${vectorXML(node)}</vector>\n`;
}

function modelSVG(node, ids) {
  if (node.g) {
    const { tx, ty, s, px, py } = node.g;
    return `<g transform="translate(${tx + px} ${ty + py}) scale(${s}) translate(${-px} ${-py})">${node.kids.map((k) => modelSVG(k, ids)).join('')}</g>`;
  }
  const paint = (pt) => {
    if (!pt) return ['none', 1];
    const q = paintOf(pt);
    if (q.color) return [q.color, q.a ?? 1];
    const id = 'm' + ids.length, stops = q.stops.map(([o, c, al]) => `<stop offset="${o}" stop-color="${c}" stop-opacity="${al}"/>`).join('');
    ids.push(q.lin
      ? `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="${q.lin[0]}" y1="${q.lin[1]}" x2="${q.lin[2]}" y2="${q.lin[3]}">${stops}</linearGradient>`
      : `<radialGradient id="${id}" gradientUnits="userSpaceOnUse" cx="${q.rad[0]}" cy="${q.rad[1]}" r="${q.rad[2]}">${stops}</radialGradient>`);
    return [`url(#${id})`, 1];
  };
  const [f, fa] = paint(node.fill), [st, sa] = paint(node.stroke);
  return `<path d="${node.d}" fill="${f}" fill-opacity="${fa}" stroke="${st}" stroke-opacity="${sa}" stroke-width="${node.stroke ? node.sw : 0}" stroke-linecap="${node.cap || 'butt'}" stroke-linejoin="${node.join || 'miter'}"/>`;
}

// The launchers' masks, on the 108 dp canvas in frame units: the 72 dp
// viewport is [INSET, INSET + 1024].
const MASKS = {
  circle: `M${INSET},${VIEW / 2} A512,512 0 1 0 ${INSET + 1024},${VIEW / 2} A512,512 0 1 0 ${INSET},${VIEW / 2} Z`,
  squircle: (() => { let d = ''; for (let k = 0; k <= 180; k++) { const t = (k / 180) * 2 * Math.PI, c = Math.cos(t), s = Math.sin(t); d += (k ? 'L' : 'M') + r(VIEW / 2 + 512 * Math.sign(c) * Math.abs(c) ** 0.5) + ',' + r(VIEW / 2 + 512 * Math.sign(s) * Math.abs(s) ** 0.5); } return d + 'Z'; })(),
  rounded: rect(INSET, INSET, 1024, 1024, 180),
};

/**
 * A preview of the adaptive icon as a launcher draws it: the layers under a
 * mask, or themed (the monochrome layer in one tint on another). Its viewBox
 * is the masked 72 dp.
 */
export function adaptivePreviewSVG(app, { mask = 'circle', themed = null, size = 512, safeZone = false } = {}) {
  const L = adaptiveLayers(app), ids = [];
  let body;
  if (themed) {
    body = `<path d="${MASKS[mask]}" fill="${themed.bg}"/>${modelSVG(L.monochrome, ids).replaceAll('#ffffff', themed.ink)}`;
  } else body = modelSVG(L.background, ids) + modelSVG(L.foreground, ids);
  const clip = `<clipPath id="mask"><path d="${MASKS[mask]}"/></clipPath>`;
  if (safeZone) {
    // The whole 108 dp, the 72 dp viewport and the 66 dp safe zone drawn over it.
    const z = `<rect x="${INSET}" y="${INSET}" width="1024" height="1024" fill="none" stroke="#000" stroke-opacity=".5" stroke-width="4" stroke-dasharray="16 12"/><circle cx="${VIEW / 2}" cy="${VIEW / 2}" r="${(1024 * 66) / 72 / 2}" fill="none" stroke="#000" stroke-opacity=".7" stroke-width="5"/>`;
    return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${VIEW} ${VIEW}"><defs>${ids.join('')}</defs>${body}${z}</svg>`;
  }
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="${INSET - 40} ${INSET - 40} 1104 1104"><defs>${ids.join('')}${clip}</defs><g clip-path="url(#mask)">${body}</g></svg>`;
}

/** Writes an app's adaptive icon into an Android res folder: the launcher's and the round one, over three vector layers. */
export function writeAdaptiveIcon(app, res) {
  const L = adaptiveLayers(app);
  for (const dir of fs.readdirSync(res)) if (/^mipmap-/.test(dir)) fs.rmSync(path.join(res, dir), { recursive: true });
  fs.mkdirSync(path.join(res, 'mipmap-anydpi'), { recursive: true });
  fs.mkdirSync(path.join(res, 'drawable'), { recursive: true });
  for (const layer of ['background', 'foreground', 'monochrome']) fs.writeFileSync(path.join(res, 'drawable', `ic_launcher_${layer}.xml`), vectorDrawable(L[layer]));
  const icon = `<?xml version="1.0" encoding="utf-8"?>\n<!-- Generated by apps/xcode/scripts/icons.mjs; do not edit. -->\n<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n    <background android:drawable="@drawable/ic_launcher_background" />\n    <foreground android:drawable="@drawable/ic_launcher_foreground" />\n    <monochrome android:drawable="@drawable/ic_launcher_monochrome" />\n</adaptive-icon>\n`;
  for (const name of ['ic_launcher', 'ic_launcher_round']) fs.writeFileSync(path.join(res, 'mipmap-anydpi', `${name}.xml`), icon);
}

/** The previews docs show: each mask, themed light and dark, and the layers over the safe zone. */
export function writeAdaptivePreviews(app, out) {
  fs.mkdirSync(out, { recursive: true });
  for (const mask of Object.keys(MASKS)) renderPNG(adaptivePreviewSVG(app, { mask }), 512, path.join(out, `icon-${mask}.png`), { transparent: true });
  renderPNG(adaptivePreviewSVG(app, { themed: { bg: '#d8e2f3', ink: '#21395c' } }), 512, path.join(out, 'icon-themed-light.png'), { transparent: true });
  renderPNG(adaptivePreviewSVG(app, { themed: { bg: '#24304a', ink: '#c3d5f5' } }), 512, path.join(out, 'icon-themed-dark.png'), { transparent: true });
  renderPNG(adaptivePreviewSVG(app, { safeZone: true }), 512, path.join(out, 'icon-safe-zone.png'), { transparent: true });
}

// CLI:
//   node icons.mjs svg <out-dir> [app ...]          light, dark and tinted SVGs
//   node icons.mjs appiconset <app> <dir> [--mac]   an Xcode AppIcon set
//   node icons.mjs favicon <app> <file.svg>         the rounded light icon as SVG
//   node icons.mjs adaptive <app> <res-dir>         an Android adaptive icon, its layers as vector drawables
//   node icons.mjs adaptive-previews <app> <dir>    that icon under each mask, themed, and over its safe zone, as PNGs
if (import.meta.url === `file://${process.argv[1]}`) {
  const [cmd, ...rest] = process.argv.slice(2);
  if (cmd === 'svg') {
    const [out, ...apps] = rest;
    fs.mkdirSync(out, { recursive: true });
    for (const app of apps.length ? apps : Object.keys(APPS)) for (const mode of ['light', 'dark', 'tinted']) fs.writeFileSync(path.join(out, `${app}-${mode}.svg`), iconSVG(app, mode) + '\n');
  } else if (cmd === 'appiconset') {
    writeAppIconSet(rest[0], rest[1], { mac: rest.includes('--mac') });
  } else if (cmd === 'favicon') {
    fs.writeFileSync(rest[1], iconSVG(rest[0], 'light', { size: 32, mask: true }).replace('<svg ', `<svg role="img" aria-label="${APPS[rest[0]].name}" `) + '\n');
  } else if (cmd === 'adaptive') {
    writeAdaptiveIcon(rest[0], rest[1]);
  } else if (cmd === 'adaptive-previews') {
    writeAdaptivePreviews(rest[0], rest[1]);
  } else {
    console.error('usage: node icons.mjs svg <out-dir> [app ...] | appiconset <app> <dir> [--mac] | favicon <app> <file.svg> | adaptive <app> <res-dir> | adaptive-previews <app> <dir>');
    process.exit(2);
  }
}
