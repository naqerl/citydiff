// Resolving Playwright and its Chromium.
//
// The skill is installed under the agent's skills directory, which is usually not
// a Node project, so `import "playwright-core"` does not necessarily resolve from
// there. This tries, in order: the path in CITYDIFF_PLAYWRIGHT, a plain import,
// a node_modules near the skill, and a global install. It also finds the Chromium
// build Playwright downloaded, which is what CITYDIFF_CHROME overrides.

import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { createRequire } from "node:module";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));

// A module imported by file path may be CommonJS, in which case the named
// export is missing and everything hangs off `default`.
function pickChromium(mod) {
  return mod?.chromium ?? mod?.default?.chromium ?? undefined;
}

export async function loadChromium() {
  const tried = [];

  // 1. an explicit module path
  const explicit = process.env.CITYDIFF_PLAYWRIGHT;
  if (explicit) {
    try {
      const found = pickChromium(await import(explicit.startsWith("/") ? `file://${explicit}` : explicit));
      if (found) return found;
      tried.push(`${explicit} (no chromium export)`);
    } catch (e) {
      tried.push(`${explicit} (${e.code || e.message})`);
    }
  }

  // 2. a normal resolution, which works when the skill sits in a Node project
  try {
    const found = pickChromium(await import("playwright-core"));
    if (found) return found;
    tried.push("playwright-core (no chromium export)");
  } catch (e) {
    tried.push(`playwright-core (${e.code || e.message})`);
  }

  // 3. a node_modules at or above the skill directory
  for (const base of [HERE, process.cwd(), process.env.HOME].filter(Boolean)) {
    let dir = base;
    for (let i = 0; i < 6; i++) {
      const candidate = join(dir, "node_modules", "playwright-core");
      if (existsSync(candidate)) {
        try {
          const found = pickChromium(await import(`file://${candidate}`));
          if (found) return found;
          tried.push(`${candidate} (no chromium export)`);
        } catch (e) {
          tried.push(`${candidate} (${e.code || e.message})`);
        }
      }
      const up = dirname(dir);
      if (up === dir) break;
      dir = up;
    }
  }

  throw new Error(
    "playwright-core not found. Install it and retry:\n" +
      "  npm install -g playwright-core && npx playwright install chromium ffmpeg\n" +
      "or point CITYDIFF_PLAYWRIGHT at an existing playwright-core.\n" +
      "tried:\n  " + tried.join("\n  ")
  );
}

// findChrome returns the Chromium Playwright downloaded, or undefined. Its
// revision must match what the installed Playwright expects, so prefer the
// revision Playwright itself names over any other build on the machine.
export function findChrome() {
  const override = process.env.CITYDIFF_CHROME;
  if (override) return override;

  const root = process.env.PLAYWRIGHT_BROWSERS_PATH ||
    join(process.env.HOME || "/root", ".cache", "ms-playwright");
  if (!existsSync(root)) return undefined;

  const entries = readdirSync(root).filter((e) => e.startsWith("chromium-"));
  const preferred = String(readdirSync(root).find((e) => e === "chromium-1243") || "");

  const build = (rev) => {
    for (const p of [
      join(root, rev, "chrome-linux64", "chrome"),
      join(root, rev, "chrome-linux", "chrome"),
      join(root, rev, "chrome-mac", "Chromium.app", "Contents", "MacOS", "Chromium"),
    ]) {
      if (existsSync(p)) return p;
    }
    return undefined;
  };

  // Newest revision first: a newer Chromium is at worst ignored by Playwright,
  // and the one it downloaded for itself is usually the newest anyway.
  for (const rev of entries.sort().reverse()) {
    const p = build(rev);
    if (p) return p;
  }
  return preferred ? build(preferred) : undefined;
}
