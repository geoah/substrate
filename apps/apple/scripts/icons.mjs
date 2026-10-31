// Draws the Substrate family's app icons: Substrate, Substrate Sync and
// Substrate Chat. Apps built on a substrate elsewhere copy this file and add
// their own entry to APPS (and a glyph to GLYPHS), so their icons share the
// frame.
//
// Every icon shares one frame: the app's glyph on a pale gradient, and a band
// across the bottom, a deeper shade of the background, that carries the
// substrate infinity mark in the app's colour. Glyphs are close shades of the
// app's colour with a light frosting. Each app has three iOS appearances:
// light, dark and tinted (greyscale, which iOS colours).
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

// Bernoulli lemniscate with taller lobes, unit half-width.
function infinityPath(k = 1.5, n = 160) {
  let d = '';
  for (let i = 0; i <= n; i++) {
    const t = (i / n) * 2 * Math.PI, s = Math.sin(t), c = Math.cos(t), q = 1 + s * s;
    d += (i ? 'L' : 'M') + (c / q).toFixed(4) + ',' + ((k * s * c) / q).toFixed(4);
  }
  return d + 'Z';
}
const INF = infinityPath();

// Per app: pale background (top, bottom) and the glyph's light and deep colour.
export const APPS = {
  substrate: { name: 'Substrate', bg: ['#f5f9ff', '#d9e7ff'], hi: '#86bbff', lo: '#2d5fe6', glyph: 'strata' },
  sync: { name: 'Substrate Sync', bg: ['#f2fcf8', '#d3f2e5'], hi: '#72e2bb', lo: '#0f936d', glyph: 'cycle' },
  chat: { name: 'Substrate Chat', bg: ['#f8f5ff', '#e4dcfd'], hi: '#bba2ff', lo: '#6a3fe0', glyph: 'chat' },
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
// "spark" is a small mark drawn on top in a contrasting colour.
const GLYPHS = {
  // Three layers: the substrate.
  strata: () => [['back', rhombus(560)], ['mid', rhombus(440)], ['front', rhombus(320)]],
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
  // A speech bubble with a spark: the agent you talk to.
  chat: () => [['back', rect(392, 236, 370, 260, 110)],
    ['front', 'M358,278 L638,278 C710,278 758,326 758,398 L758,518 C758,590 710,638 638,638 L456,638 L352,712 L374,638 L358,638 C286,638 238,590 238,518 L238,398 C238,326 286,278 358,278 Z'],
    ['spark', spark(484, 458, 104)], ['spark', spark(604, 382, 42)]],
};

function drawGlyph(name, mode, p, id) {
  const shades = mode === 'light'
    ? { front: [mix(p.lo, '#ffffff', 0.1), p.lo], mid: [mix(p.lo, p.hi, 0.36), mix(p.lo, p.hi, 0.24)], back: [mix(p.lo, p.hi, 0.6), mix(p.lo, p.hi, 0.48)], rim: 0.35, spark: '#ffffff' }
    : mode === 'dark'
      ? { front: [mix(p.hi, '#ffffff', 0.1), mix(p.hi, p.lo, 0.35)], mid: [mix(p.hi, p.lo, 0.4), mix(p.hi, p.lo, 0.55)], back: [mix(p.hi, p.lo, 0.62), mix(p.hi, p.lo, 0.78)], rim: 0.3, spark: p.lo }
      : { front: ['#f0f0f0', '#cfcfcf'], mid: ['#bdbdbd', '#a6a6a6'], back: ['#999999', '#828282'], rim: 0.35, spark: '#3a3a3e' };
  let defs = '', body = '';
  for (const lv of ['front', 'mid', 'back']) defs += `<linearGradient id="${id}${lv}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${shades[lv][0]}"/><stop offset="1" stop-color="${shades[lv][1]}"/></linearGradient>`;
  defs += `<filter id="${id}sh" x="-30%" y="-30%" width="160%" height="170%"><feGaussianBlur in="SourceAlpha" stdDeviation="24"/><feOffset dy="20" result="b"/><feFlood flood-color="${mode === 'light' ? p.lo : '#000000'}" flood-opacity="${mode === 'light' ? 0.13 : 0.4}"/><feComposite in2="b" operator="in" result="s"/><feMerge><feMergeNode in="s"/><feMergeNode in="SourceGraphic"/></feMerge></filter>`;
  for (const [lv, d, w] of GLYPHS[name]()) {
    if (lv === 'spark') { body += `<path d="${d}" fill="${shades.spark}"/>`; continue; }
    const op = 1;
    if (w) { body += `<path d="${d}" fill="none" stroke="url(#${id}${lv})" stroke-width="${w}" stroke-linecap="round" opacity="${op}"/>`; continue; }
    body += `<path d="${d}" fill="url(#${id}${lv})" stroke="url(#${id}${lv})" stroke-width="10" stroke-linejoin="round" opacity="${op}"/>` +
      `<path d="${d}" fill="none" stroke="#ffffff" stroke-opacity="${shades.rim}" stroke-width="3" stroke-linejoin="round"/>`;
  }
  return `<defs>${defs}</defs><g filter="url(#${id}sh)">${body}</g>`;
}

let uid = 0;

/**
 * One icon as SVG. mode: light | dark | tinted. Full bleed and square, as
 * Xcode wants it; mask: true rounds it like iOS does (previews and favicons).
 */
export function iconSVG(app, mode = 'light', { size = 1024, mask = false } = {}) {
  const a = APPS[app], id = 'i' + uid++;
  let hi = a.hi, lo = a.lo, bg0 = a.bg[0], bg1 = a.bg[1], glow = a.hi;
  // The band is a deeper shade of the background, the infinity in the app's colour.
  let band0 = mix(a.bg[1], a.hi, 0.35), band1 = mix(a.bg[1], a.hi, 0.55), ink0 = a.lo, ink1 = mix(a.lo, '#000000', 0.25), edge = 0.7;
  if (mode === 'dark' || mode === 'tinted') {
    bg0 = '#2a2a30'; bg1 = '#0b0b0d';
    band0 = mix('#1d1d22', a.lo, 0.22); band1 = mix('#101013', a.lo, 0.18); ink0 = a.hi; ink1 = a.lo; edge = 0.12;
  }
  if (mode === 'tinted') {
    glow = '#ffffff'; hi = '#f2f2f2'; lo = '#9a9a9a';
    band0 = '#2b2b2f'; band1 = '#1c1c1f'; ink0 = '#f2f2f2'; ink1 = '#9a9a9a';
  }
  const p = { hi, lo };
  const defs = [
    `<linearGradient id="${id}bg" x1="0" y1="0" x2="0.35" y2="1"><stop offset="0" stop-color="${bg0}"/><stop offset="1" stop-color="${bg1}"/></linearGradient>`,
    `<radialGradient id="${id}gw" cx=".5" cy=".3" r=".62"><stop offset="0" stop-color="${glow}" stop-opacity="${mode === 'light' ? 0.35 : 0.22}"/><stop offset="1" stop-color="${glow}" stop-opacity="0"/></radialGradient>`,
    `<linearGradient id="${id}bd" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${band0}"/><stop offset="1" stop-color="${band1}"/></linearGradient>`,
    `<linearGradient id="${id}in" gradientUnits="userSpaceOnUse" x1="0" y1="800" x2="0" y2="890"><stop offset="0" stop-color="${ink0}"/><stop offset="1" stop-color="${ink1}"/></linearGradient>`,
  ];
  if (mask) defs.push(`<clipPath id="${id}m"><rect width="1024" height="1024" rx="230"/></clipPath>`);
  const glyph = `<g transform="translate(512 352) scale(0.86) translate(-512 -470)">${drawGlyph(a.glyph, mode, { hi: a.hi, lo: a.lo }, id + 'g')}</g>`;
  const band = `<rect x="-20" y="690" width="1064" height="400" fill="url(#${id}bd)"/><rect x="-20" y="690" width="1064" height="3" fill="#fff" opacity="${edge}"/>`;
  const inf = `<path d="${INF}" transform="translate(512 845) scale(72)" fill="none" stroke="url(#${id}in)" stroke-width="0.30" stroke-linejoin="round"/>`;
  const body = `<rect width="1024" height="1024" fill="url(#${id}bg)"/><rect width="1024" height="1024" fill="url(#${id}gw)"/>${glyph}${band}${inf}`;
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
  const html = path.join(os.tmpdir(), `icon-${process.pid}-${uid++}.html`);
  fs.writeFileSync(html, `<html><body style="margin:0;background:${transparent ? 'transparent' : '#000'}">${svg.replace(/width="\d+" height="\d+"/, `width="${px}" height="${px}"`)}</body></html>`);
  const args = ['--headless', '--no-sandbox', '--hide-scrollbars', `--screenshot=${path.resolve(out)}`, `--window-size=${px},${px}`, `file://${html}`];
  if (transparent) args.splice(3, 0, '--default-background-color=00000000');
  execFileSync(chrome(), args, { stdio: 'ignore' });
  fs.rmSync(html);
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

// CLI:
//   node icons.mjs svg <out-dir> [app ...]          light, dark and tinted SVGs
//   node icons.mjs appiconset <app> <dir> [--mac]   an Xcode AppIcon set
//   node icons.mjs favicon <app> <file.svg>         the rounded light icon as SVG
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
  } else {
    console.error('usage: node icons.mjs svg <out-dir> [app ...] | appiconset <app> <dir> [--mac] | favicon <app> <file.svg>');
    process.exit(2);
  }
}
