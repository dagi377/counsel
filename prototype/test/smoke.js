// Headless smoke test: boot the prototype, visit every smoke path as every user, click every action button.
const { JSDOM } = require("jsdom");
const fs = require("fs"), path = require("path");
const dir = process.argv[2];
const only = process.argv[3]; // optional module id
const html = fs.readFileSync(path.join(dir, "index.html"), "utf8").replace(/<script[\s\S]*?<\/script>/g, "");
const dom = new JSDOM(html, { runScripts: "outside-only", pretendToBeVisual: true, url: "https://example.test/" });
const w = dom.window; const errs = [];
w.console.error = (...a) => errs.push(a.map(String).join(" "));
w.addEventListener("error", (e) => errs.push("window error: " + e.message));
const files = [...fs.readFileSync(path.join(dir, "index.html"), "utf8").matchAll(/<script src="([^"]+)"/g)].map((m) => m[1]);
for (const f of files) { const p = path.join(dir, f); if (!fs.existsSync(p)) { console.log("missing", f); continue; }
  try { w.eval(fs.readFileSync(p, "utf8") + `\n//# sourceURL=${f}`); } catch (e) { errs.push(`${f}: ${e.stack.split("\n").slice(0,2).join(" ")}`); } }
const A = w.App; A.fast = true;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
(async () => {
  try { A.start(); } catch (e) { errs.push("start: " + e.stack); }
  await sleep(20);
  const paths = new Set(["home"]);
  for (const m of A._mods) { if (only && !only.split(",").includes(m.id)) continue; Object.keys(m.routes || {}).filter((r) => !r.includes(":")).forEach((r) => paths.add(r)); (m.smoke || []).forEach((r) => paths.add(r)); }
  let pages = 0, clicks = 0, failedRender = [];
  for (const u of A.store.all("users")) {
    A.setUser(u.id); await sleep(5);
    for (const p of paths) {
      A.go(p); await sleep(5); pages++;
      const v = w.document.getElementById("view");
      if (/failed to render/.test(v.textContent)) failedRender.push(`${u.id} ${p}: ${v.textContent.slice(0, 160)}`);
      const btns = [...v.querySelectorAll("[data-a]")].filter((b) => !["go"].includes(b.dataset.a)).slice(0, 25);
      for (const b of btns) { if (!b.isConnected) continue; try { b.click(); clicks++; } catch (e) { errs.push(`click ${b.dataset.a} on ${p} as ${u.id}: ${e.message}`); }
        await sleep(2); const mb = w.document.querySelector("#modal:not([hidden]) [data-mb]"); if (mb) { const btn = [...w.document.querySelectorAll("#modal [data-mb]")].pop(); try { btn.click(); } catch (e) { errs.push("modal: " + e.message); } }
        A.closeModal(); if (A.route() !== p) { A.go(p); await sleep(2); } }
    }
  }
  for (const q of ["what is due this week", "draft a letter", "summarize this matter", "open a matter for Tsion Haile", "trust balance"]) A.ask(q);
  await sleep(200);
  console.log(`users=${A.store.all("users").length} paths=${paths.size} pages=${pages} clicks=${clicks}`);
  failedRender.slice(0, 15).forEach((x) => console.log("RENDER FAIL", x));
  [...new Set(errs)].slice(0, 25).forEach((e) => console.log("ERROR", e));
  console.log(failedRender.length || errs.length ? "SMOKE FAIL" : "SMOKE OK");
  process.exit(0);
})();
