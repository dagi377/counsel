// Screenshots the style guide at 1280px in light and dark with system Chrome.
//   node web/tools/styleguide_shots.mjs [outDir]
// Starts a Vite dev server on a free port; no network calls other than the Google Fonts link
// in styleguide.html (the page renders with the fallback stack when offline).
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { createServer } from "vite";

const webRoot = fileURLToPath(new URL("..", import.meta.url));
const outDir = resolve(process.argv[2] ?? resolve(webRoot, "src/ui/styleguide/shots"));
mkdirSync(outDir, { recursive: true });

const server = await createServer({ root: webRoot, configFile: resolve(webRoot, "vite.config.ts"), server: { port: 0, strictPort: false }, logLevel: "error" });
await server.listen();
const port = server.config.server.port ?? server.httpServer.address().port;
const url = `http://localhost:${port}/styleguide.html`;

const browser = await chromium.launch({ channel: "chrome", headless: true });
try {
  for (const scheme of ["light", "dark"]) {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: scheme, deviceScaleFactor: 1 });
    const page = await ctx.newPage();
    await page.goto(url, { waitUntil: "networkidle" });
    await page.waitForSelector("h1");
    const file = resolve(outDir, `styleguide-1280-${scheme}.png`);
    await page.screenshot({ path: file, fullPage: true });
    console.log(`wrote ${file}`);
    await ctx.close();
  }
} finally {
  await browser.close();
  await server.close();
}
