// Recorder for citydiff showcase videos.
//
// 1920x1080, headless Chromium on the real GPU, video captured by Playwright and
// muxed to H.264 mp4 by ffmpeg. Every file it writes is measured for real motion,
// because container metadata claims 25 fps whether or not the frames are.
//
//   node record.mjs --check                     measure the renderer, record nothing
//   node record.mjs OUTDIR                      sanity check the toolchain
//
// Then drive a scenario from your own module:
//
//   import { record } from "./record.mjs";
//   await record("01-tour", { url, run: async ({ page, still }) => { ... } });

import { loadChromium, findChrome } from "./playwright.mjs";
import { execFileSync } from "node:child_process";
import { mkdirSync, readdirSync, renameSync, rmSync, statSync, readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));

export const W = 1920;
export const H = 1080;

// The real GPU. SwiftShader renders this city at ~1 fps and the recorder pads
// the result to 25 fps with duplicates, which is what "laggy" actually is.
// CITYDIFF_GL_ARGS overrides it, which is how you reproduce the bad case on
// purpose rather than by accident.
export const GPU_ARGS = process.env.CITYDIFF_GL_ARGS
  ? process.env.CITYDIFF_GL_ARGS.split(/\s+/).filter(Boolean)
  : ["--use-gl=angle", "--use-angle=gl", "--enable-gpu"];

export function log(...a) {
  console.log(`[${new Date().toISOString().slice(11, 19)}]`, ...a);
}

export async function record(name, { url, run, outDir = "videos", tmp = "/tmp/citydiff-rec" }) {
  const chromium = await loadChromium();
  const executablePath = findChrome();
  if (!executablePath) {
    throw new Error(
      "no Chromium found. Run: npx playwright install chromium\n" +
      "or set CITYDIFF_CHROME to a Chromium binary."
    );
  }

  mkdirSync(outDir, { recursive: true });
  const dir = join(tmp, name);
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });

  const browser = await chromium.launch({
    executablePath,
    headless: true,
    args: [
      ...GPU_ARGS,
      "--no-sandbox",
      "--disable-dev-shm-usage",
      "--force-device-scale-factor=1",
      "--hide-scrollbars",
      "--mute-audio",
      "--force-color-profile=srgb",
      "--disable-lcd-text",
      "--font-render-hinting=none",
    ],
  });

  const context = await browser.newContext({
    viewport: { width: W, height: H },
    deviceScaleFactor: 1,
    recordVideo: { dir, size: { width: W, height: H } },
  });

  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e.message)));
  page.on("response", (r) => {
    // /tour.json 404s when no tour is served; the favicon is never served.
    if (r.status() >= 400 && !/tour\.json|favicon/.test(r.url())) {
      errors.push(`HTTP ${r.status()} ${r.url()}`);
    }
  });
  page.on("console", (m) => {
    if (m.type() === "error" && !/Failed to load resource/.test(m.text())) {
      errors.push(`console: ${m.text()}`);
    }
  });

  const ctx = {
    page,
    browser,
    /** Pause for ms. Use this to let a camera move or a panel settle. */
    wait: (ms) => page.waitForTimeout(ms),
    /** Wait until two consecutive canvas captures match, or budget runs out. */
    still: async (interval = 900, budget = 45000) => {
      const start = Date.now();
      let prev = null;
      while (Date.now() - start < budget) {
        const cur = (await page.locator("canvas").screenshot()).toString("base64");
        if (prev !== null && cur === prev) return true;
        prev = cur;
        await page.waitForTimeout(interval);
      }
      return false;
    },
  };

  try {
    await page.goto(url, { waitUntil: "load", timeout: 60000 });
    await page.waitForTimeout(9000); // the city builds client-side; see SKILL.md
    await run(ctx);
  } finally {
    await context.close(); // flushes the video
    await browser.close();
  }

  const raw = readdirSync(dir).filter((f) => f.endsWith(".webm"));
  if (!raw.length) {
    throw new Error(
      `no video captured for ${name} — is Playwright's ffmpeg installed? ` +
      `run: npx playwright@1.64.0 install ffmpeg`
    );
  }

  const out = join(outDir, `${name}.mp4`);
  execFileSync("ffmpeg", [
    "-y", "-i", join(dir, raw[0]),
    "-c:v", "libx264", "-preset", "slow", "-crf", "18",
    "-pix_fmt", "yuv420p", "-movflags", "+faststart",
    out,
  ], { stdio: "inherit" });

  for (const png of readdirSync(dir).filter((f) => f.endsWith(".png"))) {
    renameSync(join(dir, png), join(outDir, `${name}-${png}`));
  }

  log(`done ${name} -> ${out} (${(statSync(out).size / 1e6).toFixed(1)} MB)`);
  log(`  ${motion(out)}`);
  if (errors.length) log(`  page errors (${errors.length}):`, errors.slice(0, 5));
  return out;
}

// motion reports the frame rate inside runs of moving frames. The whole-file
// average is misleading the other way: a showcase holds on each step on purpose,
// so a high "static" share is fine and a low "fps while moving" is not.
export function motion(file) {
  const txt = `/tmp/citydiff-motion-${process.pid}.txt`;
  try {
    execFileSync("ffmpeg", [
      "-v", "error", "-i", file,
      "-vf", `tblend=all_mode=difference,signalstats,metadata=print:key=lavfi.signalstats.YAVG:file=${txt}`,
      "-f", "null", "-",
    ], { stdio: ["ignore", "ignore", "ignore"] });
    const y = [...readFileSync(txt, "utf8").matchAll(/YAVG=([0-9.]+)/g)].map((m) => Number(m[1]));
    rmSync(txt, { force: true });
    if (!y.length) return "no frames analysed";

    const runs = [];
    let start = null;
    y.forEach((v, i) => {
      if (v >= 0.35 && start === null) start = i;
      else if (v < 0.35 && start !== null) { runs.push([start, i]); start = null; }
    });
    if (start !== null) runs.push([start, y.length]);

    const secs = runs.filter(([a, b]) => b - a >= 8).map(([a, b]) => (b - a) / 25);
    if (!secs.length) return `${y.length} frames, no moving runs`;
    const fps = secs.flatMap((s) => Array(Math.round(s * 25)).fill(1 / s)).sort((a, b) => a - b);
    const held = (100 * (y.length - fps.length) / y.length).toFixed(0);
    return (
      `fps while moving: median ${fps[fps.length >> 1].toFixed(1)} ` +
      `(min ${fps[0].toFixed(1)}, max ${fps[fps.length - 1].toFixed(1)}); ` +
      `${held}% of frames are deliberate holds. Laggy if the median is far below 25.`
    );
  } catch (e) {
    return `motion check failed: ${String(e.message).split("\n")[0]}`;
  }
}

// --check: prove the renderer can keep up before spending minutes on a recording.
// Usage: node record.mjs --check [viewer-url]
// With a URL it measures the frames the browser actually delivers while the
// camera orbits, which is the only number that cannot be faked by the muxer.
async function check(url) {
  const chromium = await loadChromium();
  const executablePath = findChrome();
  if (!executablePath) throw new Error("no Chromium found: npx playwright install chromium");
  console.log(`chromium: ${executablePath}`);

  const browser = await chromium.launch({
    executablePath, headless: true,
    args: [...GPU_ARGS, "--no-sandbox", "--disable-dev-shm-usage", "--hide-scrollbars"],
  });
  const page = await browser.newPage({ viewport: { width: 640, height: 360 } });
  await page.goto(url || "about:blank", { waitUntil: "load", timeout: 60000 });
  await page.waitForTimeout(url ? 12000 : 0);

  const renderer = await page.evaluate(() => {
    const gl = document.createElement("canvas").getContext("webgl2");
    if (!gl) return "no webgl2";
    const d = gl.getExtension("WEBGL_debug_renderer_info");
    return d ? gl.getParameter(d.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER);
  });
  console.log(`renderer: ${renderer}`);

  if (!url) {
    if (/swiftshader/i.test(renderer)) {
      console.log("FAIL: software rendering. Expect ~1 fps and a padded, juddery video.");
      console.log("      Use --use-gl=angle --use-angle=gl on a machine with /dev/dri/renderD*.");
    } else {
      console.log("OK: hardware renderer. Pass a viewer URL to measure the real frame rate.");
    }
    await browser.close();
    return;
  }

  // CDP delivers a screencast frame only when the compositor draws one, so
  // counting them over wall-clock time is the frame rate the viewer really ran.
  const cdp = await page.context().newCDPSession(page);
  const stamps = [];
  cdp.on("Page.screencastFrame", async (f) => {
    stamps.push(f.metadata.timestamp);
    try { await cdp.send("Page.screencastFrameAck", { sessionId: f.sessionId }); } catch {}
  });
  await cdp.send("Page.startScreencast", { format: "jpeg", quality: 80, everyNthFrame: 1 });
  await page.waitForTimeout(1500);
  stamps.length = 0;

  const SECONDS = 12;
  await page.keyboard.down("q"); // orbit: keeps the scene changing
  const t0 = Date.now();
  await page.waitForTimeout(SECONDS * 1000);
  await page.keyboard.up("q");
  const wall = (Date.now() - t0) / 1000;
  await cdp.send("Page.stopScreencast");
  await browser.close();

  const gaps = stamps.slice(1).map((s, i) => (s - stamps[i]) * 1000).sort((a, b) => a - b);
  const fps = stamps.length / wall;
  const median = gaps.length ? gaps[gaps.length >> 1] : 0;
  console.log(
    `screencast: ${stamps.length} frames / ${wall.toFixed(1)}s = ${fps.toFixed(1)} fps ` +
    `(median gap ${median.toFixed(1)} ms)`
  );
  if (/swiftshader/i.test(renderer) || fps < 20) {
    console.log("FAIL: under ~20 fps. A recording made now would be padded and juddery.");
    process.exitCode = 1;
  } else {
    console.log("OK: fast enough to record 25 fps without padding.");
  }
}

if (process.argv[1] && process.argv[1].endsWith("record.mjs")) {
  const i = process.argv.indexOf("--check");
  if (i >= 0) await check(process.argv[i + 1]);
  else log(`record.mjs exports record(). Import it from a scenario module. Scripts live in ${HERE}`);
}
