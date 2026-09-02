import { chromium } from '@playwright/test';
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1440, height: 900 } });
const errs = [];
p.on('console', m => { if (m.type() === 'error') errs.push(m.text().slice(0,140)); });
for (const [name, url] of [['landing','http://localhost:3002/'],['dashboard','http://localhost:3002/dashboard']]) {
  try {
    await p.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await p.waitForTimeout(3500);
    await p.screenshot({ path: `${process.env.SHOTDIR}/${name}.png`, fullPage: false });
    const t = (await p.title()) || '(no title)';
    const h = (await p.locator('h1,h2').first().textContent().catch(()=>null)) || '(no heading)';
    console.log(`${name}: title="${t}" heading="${h.trim().slice(0,60)}"`);
  } catch (e) { console.log(`${name}: FAILED ${e.message.slice(0,100)}`); }
}
if (errs.length) console.log('console errors:', [...new Set(errs)].slice(0,5).join(' | '));
await b.close();
