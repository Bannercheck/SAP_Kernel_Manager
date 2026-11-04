// Screenshots of the KernelMan Hub dashboard in demo mode (make hub-examples).
// Usage: NODE_PATH=$(npm root -g) node scripts/hub-screens.cjs <base-url> <out-dir>
const { chromium } = require("playwright");

const base = process.argv[2] || "http://127.0.0.1:18090";
const out = process.argv[3] || "docs/examples";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, colorScheme: "light" });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => console.error("page error:", e.message));
  page.on("console", (m) => { if (m.type() === "error") console.error("console:", m.text()); });
  // grow the viewport to the page so the fixed background and the sidebar cover everything
  const shot = async (name, full = true) => {
    await page.mouse.move(2, 2); // no hover effect in the pictures
    await sleep(500);
    if (full) {
      const h = await page.evaluate(() => document.documentElement.scrollHeight);
      await page.setViewportSize({ width: 1440, height: Math.max(900, h) });
      await sleep(300);
    }
    await page.screenshot({ path: `${out}/hub-${name}.png` });
    await page.setViewportSize({ width: 1440, height: 900 });
    console.log(`${out}/hub-${name}.png`);
  };

  await page.goto(base);
  await page.waitForSelector("form input[name=password]");
  await shot("login", false);
  await page.fill("input[name=user]", "admin");
  await page.fill("input[name=password]", "kernelman1");
  await page.click("button[type=submit]");
  await page.waitForSelector(".sys");
  await shot("overview");

  await page.click("[data-open=PRD]");
  await page.waitForSelector(".modal .detail");
  await shot("overview-open", false);
  await page.keyboard.press("Escape");
  await page.fill("[data-q]", "suse");
  await shot("overview-filter", false);
  await page.fill("[data-q]", "");

  await page.goto(base + "/#/system/PRD");
  await page.waitForSelector("[data-op=update]");
  await shot("system");
  await page.click("[data-unlock]");
  await page.click("[data-ok]");
  await page.waitForSelector("[data-op=update]:not([disabled])");
  await page.click("[data-op=update]");
  await page.waitForFunction(() => document.querySelectorAll(".plan > div").length > 5);
  await page.fill("input[name=confirm]", "PRD");
  await page.setViewportSize({ width: 1440, height: 1300 });
  await page.evaluate(() => { document.querySelector(".modal").scrollTop = 0; });
  await sleep(400);
  await page.locator(".modal").screenshot({ path: `${out}/hub-update-plan.png` });
  await page.setViewportSize({ width: 1440, height: 900 });
  console.log(`${out}/hub-update-plan.png`);

  await page.click("[data-run]");
  await page.waitForSelector(".console .l");
  await sleep(7000);
  await shot("job-running", false);
  await page.waitForFunction(() => /Done|Failed/.test(document.querySelector("[data-head]")?.textContent || ""), null, { timeout: 180000 });
  await sleep(1500);
  await shot("job-done", false);

  await page.goto(base + "/#/");
  await sleep(4000); // the hub refreshes the hosts right after a job
  await page.reload();
  await page.waitForSelector(".sys");
  await shot("overview-after");

  for (const p of ["hosts", "archives", "settings"]) {
    await page.goto(`${base}/#/${p}`);
    await page.waitForSelector(".card");
    await shot(p);
  }
  await page.goto(`${base}/#/hosts`);
  await page.click("#top [data-act=add]");
  await page.waitForSelector(".modal input[name=name]");
  await page.locator(".modal").screenshot({ path: `${out}/hub-add-host.png` });
  console.log(`${out}/hub-add-host.png`);

  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto(base + "/#/");
  await page.waitForSelector(".sys");
  await shot("overview-dark");
  await browser.close();
})().catch((e) => { console.error(e); process.exit(1); });
