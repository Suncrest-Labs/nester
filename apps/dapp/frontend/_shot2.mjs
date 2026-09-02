import { chromium } from '@playwright/test';
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1440, height: 900 } });
await p.goto('http://localhost:3002/', { waitUntil: 'domcontentloaded', timeout: 30000 });
await p.waitForTimeout(2500);
for (const label of ['Reject Optional', 'Skip']) {
  const btn = p.getByRole('button', { name: label });
  if (await btn.count()) { await btn.first().click().catch(()=>{}); await p.waitForTimeout(1200); }
}
await p.screenshot({ path: process.env.SHOTDIR + '/app.png' });
console.log('after dismiss:', (await p.locator('h1,h2').first().textContent().catch(()=>'(none)')||'').trim().slice(0,70));
for (const [n,u] of [['vaults','/vaults'],['savings','/savings']]) {
  try { await p.goto('http://localhost:3002'+u, {waitUntil:'domcontentloaded',timeout:20000}); await p.waitForTimeout(2500);
    await p.screenshot({ path: process.env.SHOTDIR + '/' + n + '.png' });
    console.log(n+':', (await p.locator('h1,h2').first().textContent().catch(()=>'(none)')||'').trim().slice(0,60));
  } catch(e){ console.log(n+': FAILED', e.message.slice(0,70)); }
}
await b.close();
