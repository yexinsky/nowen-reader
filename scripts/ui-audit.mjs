/**
 * 通用页面审计探针(只读)
 *
 * 用途:打开一个前端路由,静态量测「溢出 / 裂图 / 死链接 / 乱码 / 过小字号 / 被裁切 /
 * 相邻重复文本 / 固定层是否出屏」,可选点一个按钮并记录点击前后差异。不写任何数据,
 * 不点击 destructive 按钮。配合 docs/ui-audit/REPORT.md 复现界面问题。
 *
 * 用法:
 *   node scripts/ui-audit.mjs <path> [w] [h] [waitMs] [outPrefix] [clickText]
 *     path      例如 /jm  /books  /settings?tab=jm-source
 *     w,h       视口尺寸,默认 1440x900;手机用 390x844(配合 NAV_DPR=2)
 *     waitMs    导航后等待毫秒,默认 4000
 *     outPrefix 截图前缀(默认 audit),输出 <前缀>.png 与 <前缀>-full.png
 *     clickText 可选:点击标签含该文字的按钮/链接,记录点击前后差异
 *
 * 环境变量(未设置则用默认值,凭据不入库):
 *   NAV_BASE    站点地址,默认 http://127.0.0.1:3000
 *   NAV_COOKIE  nowen 会话 cookie 的**值**(不要带 "nowen_session=" 前缀)
 *   NAV_JM_TOKEN  JM 会话 token(对应 X-JM-Token 头)
 *   NAV_JM_USER   JM 用户信息 JSON 字符串(localStorage.jm_user_info)
 *   NAV_CHROME  Chrome 可执行文件路径,默认按平台探测
 *   NAV_DPR     设备像素比,默认桌面 1;手机截图建议 2
 *
 * 输出:stdout 一段 JSON(页面结构与异常清单) + 两张截图。
 */
import { spawn } from 'node:child_process';
import { existsSync, writeFileSync, mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const [path, w = '1440', h = '900', waitMs = '4000', outPrefix = 'audit', clickText = ''] = process.argv.slice(2);
const BASE = process.env.NAV_BASE || 'http://127.0.0.1:3000';
const COOKIE = process.env.NAV_COOKIE || '';
const JM_TOKEN = process.env.NAV_JM_TOKEN || '';
const JM_USER = process.env.NAV_JM_USER || '';

const AUDIT_JS = String.raw`(async () => {
  const out = {};
  out.path = location.pathname + location.search;
  out.title = document.title;
  out.viewport = { w: window.innerWidth, h: window.innerHeight };
  const de = document.documentElement, body = document.body;
  out.overflowX = { scroll: de.scrollWidth, client: de.clientWidth, delta: de.scrollWidth - de.clientWidth };
  out.pageHeight = { scrollHeight: de.scrollHeight, clientHeight: de.clientHeight };

  const vis = (el) => {
    const cs = getComputedStyle(el);
    if (cs.display === 'none' || cs.visibility === 'hidden' || cs.opacity === '0') return false;
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  };
  const inHScroller = (el) => {
    let p = el.parentElement;
    while (p && p !== body) {
      const cs = getComputedStyle(p);
      if ((cs.overflowX === 'auto' || cs.overflowX === 'scroll') && p.scrollWidth > p.clientWidth) return true;
      p = p.parentElement;
    }
    return false;
  };
  const label = (el) => {
    const t = ((el.innerText || el.textContent || '') + '').trim().replace(/\s+/g, ' ').slice(0, 50);
    return t || el.getAttribute('aria-label') || el.getAttribute('title') || el.tagName.toLowerCase();
  };

  // 1 横向溢出(排除可横滑容器内部)
  const off = [];
  for (const el of document.querySelectorAll('body *')) {
    if (!vis(el) || inHScroller(el)) continue;
    const r = el.getBoundingClientRect();
    if (r.right > window.innerWidth + 1.5 || r.left < -1.5) {
      off.push({ tag: el.tagName.toLowerCase(), cls: String(el.className || '').slice(0, 70), text: label(el), left: Math.round(r.left), right: Math.round(r.right) });
      if (off.length >= 12) break;
    }
  }
  out.overflowOffenders = off;

  // 2 图片加载失败
  out.brokenImages = [...document.images].filter(im => im.complete && im.naturalWidth === 0)
    .slice(0, 12).map(im => ({ src: (im.currentSrc || im.src || '').slice(0, 100), cls: String(im.className || '').slice(0, 50) }));

  // 3 按钮清单(可见)
  out.buttons = [...document.querySelectorAll('button, [role="button"]')].filter(vis).slice(0, 140).map(b => {
    const r = b.getBoundingClientRect();
    return { t: label(b), d: b.disabled === true, ad: b.getAttribute('aria-disabled'), w: Math.round(r.width), h: Math.round(r.height) };
  });

  // 4 异常链接
  out.badLinks = [...document.querySelectorAll('a')].filter(vis).filter(a => {
    const hr = a.getAttribute('href');
    return !hr || hr === '#' || hr === 'javascript:void(0)';
  }).slice(0, 12).map(a => ({ t: label(a), href: a.getAttribute('href') }));

  // 5 文本层异常
  const leaves = [...document.querySelectorAll('body *')].filter(el => {
    if (!vis(el)) return false;
    if (el.children.length > 0) return false;
    const t = ((el.textContent || '') + '').trim();
    return t.length > 0 && t.length < 200;
  });
  out.leafCount = leaves.length;

  const mojibake = [], tiny = [], clipped = [];
  for (const el of leaves) {
    const t = ((el.textContent || '') + '').trim();
    if (/\uFFFD|锟斤拷|ï¿½|\u00C3\u00A4|\u00E2\u20AC/.test(t)) mojibake.push({ t: t.slice(0, 60), cls: String(el.className || '').slice(0, 50) });
    const cs = getComputedStyle(el);
    const fs = parseFloat(cs.fontSize);
    if (fs > 0 && fs < 9.5) tiny.push({ t: t.slice(0, 40), fs, cls: String(el.className || '').slice(0, 50) });
    const clamp = cs.webkitLineClamp;
    if ((!clamp || clamp === 'none') && cs.overflowY === 'hidden' && el.clientHeight > 0 && el.scrollHeight > el.clientHeight + 3) {
      clipped.push({ t: t.slice(0, 40), sh: el.scrollHeight, ch: el.clientHeight });
    }
  }
  out.mojibake = mojibake.slice(0, 10);
  out.tinyFonts = tiny.slice(0, 10);
  out.clipped = clipped.slice(0, 10);

  // 6 相邻重复文本
  const dup = [];
  for (const parent of document.querySelectorAll('body *')) {
    if (!vis(parent)) continue;
    const kids = [...parent.children].filter(vis);
    if (kids.length < 2 || kids.length > 40) continue;
    const txt = kids.map(k => ((k.innerText || '') + '').trim().replace(/\s+/g, ' '));
    for (let i = 1; i < txt.length; i++) {
      if (txt[i] && txt[i].length >= 2 && txt[i] === txt[i - 1]) {
        dup.push({ t: txt[i].slice(0, 40), parentCls: String(parent.className || '').slice(0, 50) });
        break;
      }
    }
    if (dup.length >= 10) break;
  }
  out.duplicateAdjacent = dup;

  // 7 固定层(含越界)
  out.fixedLayers = [...document.querySelectorAll('body *')].filter(el => {
    const cs = getComputedStyle(el);
    return cs.position === 'fixed' && vis(el);
  }).slice(0, 20).map(el => {
    const r = el.getBoundingClientRect();
    return { cls: String(el.className || '').slice(0, 60), t: label(el).slice(0, 30), top: Math.round(r.top), bottom: Math.round(r.bottom), z: getComputedStyle(el).zIndex };
  });
  out.outOfViewportFixed = out.fixedLayers.filter(f => f.top < -2 || f.bottom > window.innerHeight + 2);

  out.bodyTextLen = (body.innerText || '').length;
  out.bodyHead = (body.innerText || '').replace(/\s+/g, ' ').slice(0, 400);
  return out;
})()`;

// Chrome 路径:优先 NAV_CHROME,否则按平台探测常见安装位置
function findChrome() {
  const env = process.env.NAV_CHROME;
  if (env) return env;
  const candidates = [
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
  ];
  for (const c of candidates) {
    try {
      if (existsSync(c)) return c;
    } catch {
      /* 忽略探测失败,继续试下一个 */
    }
  }
  return candidates[0];
}

const profile = mkdtempSync(join(tmpdir(), 'audit-'));
const chrome = spawn(findChrome(), [
  '--headless=new', '--disable-gpu', '--hide-scrollbars', '--no-first-run',
  '--no-default-browser-check', '--remote-debugging-port=0',
  `--user-data-dir=${profile}`, `--window-size=${w},${h}`, 'about:blank',
], { stdio: 'ignore' });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let cdpPort = 0;
async function getWs() {
  for (let i = 0; i < 120; i++) {
    if (!cdpPort) {
      try {
        const t = readFileSync(join(profile, 'DevToolsActivePort'), 'utf8').split('\n');
        if (t[0] && Number(t[0]) > 0) cdpPort = Number(t[0]);
      } catch { }
    }
    if (cdpPort) {
      try {
        const j = await (await fetch(`http://127.0.0.1:${cdpPort}/json/version`)).json();
        if (j.webSocketDebuggerUrl) return j.webSocketDebuggerUrl;
      } catch { }
    }
    await sleep(250);
  }
  throw new Error('no CDP');
}

let id = 0; const pending = new Map();
try {
  const ws = new WebSocket(await getWs());
  await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pending.has(m.id)) {
      const p = pending.get(m.id); pending.delete(m.id);
      m.error ? p.rej(new Error(JSON.stringify(m.error))) : p.res(m.result);
    }
  };
  const send = (method, params = {}, sessionId) => new Promise((res, rej) => {
    const mid = ++id; pending.set(mid, { res, rej });
    ws.send(JSON.stringify({ id: mid, method, params, sessionId }));
  });

  const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
  const S = (m, p) => send(m, p, sessionId);

  await S('Page.enable'); await S('Runtime.enable'); await S('Network.enable');
  await S('Emulation.setDeviceMetricsOverride', {
    width: +w, height: +h, deviceScaleFactor: +w < 700 ? 2 : 1, mobile: +w < 700,
  });

  const host = new URL(BASE).hostname;
  await S('Network.setCookie', { name: 'nowen_session', value: COOKIE, domain: host, path: '/', httpOnly: true });

  await S('Page.navigate', { url: BASE + '/' });
  await sleep(900);
  if (JM_TOKEN) {
    await S('Runtime.evaluate', {
      expression: `try{localStorage.setItem('jm_token', ${JSON.stringify(JM_TOKEN)});` +
        (JM_USER ? `localStorage.setItem('jm_user_info', ${JSON.stringify(JM_USER)});` : '') + `}catch(e){}1`,
    });
  }

  await S('Page.navigate', { url: BASE + path });
  await sleep(+waitMs);

  // 滚动一遍触发懒加载,再回到顶部
  await S('Runtime.evaluate', {
    expression: `(async()=>{const s=()=>new Promise(r=>setTimeout(r,260));for(let i=0;i<6;i++){window.scrollTo(0,(i+1)*window.innerHeight*0.9);await s();}window.scrollTo(0,0);await s();return 1;})()`,
    awaitPromise: true,
  });

  const res = await S('Runtime.evaluate', { expression: AUDIT_JS, awaitPromise: true, returnByValue: true });
  const report = res.result.value || { error: 'no-value', raw: JSON.stringify(res).slice(0, 500) };

  let clickResult = null;
  if (clickText) {
    const before = await S('Runtime.evaluate', {
      expression: `(() => ({ loc: location.pathname + location.search, len: (document.body.innerText||'').length }))()`,
      returnByValue: true,
    });
    const clicked = await S('Runtime.evaluate', {
      expression: `(() => {
        const want = ${JSON.stringify(clickText)};
        const cands = [...document.querySelectorAll('button, a, [role="button"]')].filter(el => {
          const t = ((el.innerText||el.textContent||'') + '').trim();
          const a = el.getAttribute('aria-label') || '';
          return (t.includes(want) || a.includes(want)) && !el.disabled;
        });
        if (!cands.length) return { ok: false, reason: 'not-found' };
        const el = cands[0];
        el.scrollIntoView({ block: 'center' });
        el.click();
        return { ok: true, text: ((el.innerText||el.getAttribute('aria-label')||'')+'').trim().slice(0, 40) };
      })()`,
      returnByValue: true,
    });
    await sleep(1000);
    const after = await S('Runtime.evaluate', {
      expression: `(() => {
        const overlays = [...document.querySelectorAll('[role="dialog"], .sheet-layer, .fixed')].filter(el => {
          const cs = getComputedStyle(el);
          return cs.position === 'fixed' && cs.display !== 'none' && el.getBoundingClientRect().height > 100;
        }).map(el => ((el.innerText||'')+'').trim().slice(0, 60));
        const toasts = [...document.querySelectorAll('[class*="toast"]')].map(el => ((el.innerText||'')+'').trim().slice(0,80));
        return { loc: location.pathname + location.search, len: (document.body.innerText||'').length, overlays: overlays.slice(0,4), toasts: toasts.slice(0,3) };
      })()`,
      returnByValue: true,
    });
    clickResult = { clicked: clicked.result.value, before: before.result.value, after: after.result.value };
    report.clickProbe = clickResult;
  }

  const shot = await S('Page.captureScreenshot', { format: 'png' });
  writeFileSync(`${outPrefix}.png`, Buffer.from(shot.data, 'base64'));

  const de = await S('Runtime.evaluate', {
    expression: `JSON.stringify({sh: document.documentElement.scrollHeight, ih: window.innerHeight})`,
    returnByValue: true,
  });
  const { sh, ih } = JSON.parse(de.result.value);
  const clipH = Math.min(sh, 6000);
  if (clipH > ih + 50) {
    const full = await S('Page.captureScreenshot', {
      format: 'png', captureBeyondViewport: true,
      clip: { x: 0, y: 0, width: +w, height: clipH, scale: 1 },
    });
    writeFileSync(`${outPrefix}-full.png`, Buffer.from(full.data, 'base64'));
    report.fullShot = `${outPrefix}-full.png`;
  }
  console.log(JSON.stringify(report, null, 1));
} finally {
  chrome.kill();
  await sleep(300);
}
