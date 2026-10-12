// Writes web/src/ui/theme/tokens.css from tokens.ts. Run from anywhere:
//   node --experimental-strip-types web/tools/gen_tokens.mjs
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { renderTokensCss } from "../src/ui/theme/tokens.ts";

const out = fileURLToPath(new URL("../src/ui/theme/tokens.css", import.meta.url));
writeFileSync(out, renderTokensCss());
console.log(`wrote ${out}`);
