import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { ELEMENTS, assetURL, loadSkin, mergeSkin, pageVars, skinFromValue, skinURL, skyboxKind } from "./skin.js";

function readSkin(name) {
  return JSON.parse(readFileSync(new URL("../skins/" + name + "/skin.json", import.meta.url), "utf8"));
}

function paths(value, prefix = "") {
  if (Array.isArray(value) || !value || typeof value !== "object") return [prefix];
  return Object.keys(value).flatMap((key) => paths(value[key], prefix ? prefix + "." + key : key));
}

const dark = readSkin("dark");
const light = readSkin("light");

test("dark keeps the colours the city already used", () => {
  assert.equal(dark.name, "dark");
  assert.equal(dark.background.color, "#09090b");
  assert.equal(dark.ground.color, "#18181b");
  assert.equal(dark.horizon.color, "#3f3f46");
  assert.equal(dark.horizon.opacity, 0.7);
  assert.deepEqual(dark.package.steps, ["#27272a", "#3f3f46", "#52525b", "#71717a"]);
  assert.equal(dark.package.synthetic, "#18181b");
  assert.equal(dark.external.color, "#3f3f46");
  assert.equal(dark.type.color, "#d4d4d8");
  assert.equal(dark.function.color, "#fafafa");
  assert.equal(dark.method.color, "#a1a1aa");
  assert.equal(dark.change.added, "#71d083");
  assert.equal(dark.change.removed, "#e5484d");
  assert.equal(dark.change.modified, "#ffc53d");
  assert.equal(dark.change.body, "#b36b00");
  assert.equal(dark.change.both, "#fff1a0");
  assert.equal(dark.change.moved, "#7d66d9");
  assert.equal(dark.change.same, "#f4f4f5");
  assert.equal(dark.call.color, "#23afd0");
  assert.equal(dark.call.std, "#d4d4d8");
  assert.equal(dark.label.background, "transparent");
  assert.equal(dark.label.text, "#fafafa");
  assert.equal(dark.light.exposure, 1.08);
  assert.equal(dark.light.hemiSky, "#f4f4f5");
  assert.equal(dark.light.hemiGround, "#27272a");
  assert.equal(dark.light.hemiIntensity, 0.55);
  assert.equal(dark.light.key, "#fafafa");
  assert.equal(dark.light.keyIntensity, 0.8);
  assert.equal(dark.light.rim, "#d4d4d8");
  assert.equal(dark.light.rimIntensity, 0.18);
  assert.equal(dark.fog.falloff, 0.07);
  assert.equal(dark.entity.emissiveIntensity, 0.28);
  assert.equal(dark.dim.entity, 0.04);
  assert.equal(dark.selection.ring, "#a1a1aa");
  assert.equal(dark.selection.ringOpacity, 0.35);
  assert.equal(dark.hud.bg, "#09090b");
  assert.equal(dark.hud.fg, "#fafafa");
});

test("light and dark name the same elements", () => {
  assert.deepEqual(Object.keys(dark), ["name", ...ELEMENTS]);
  assert.deepEqual(Object.keys(light), ["name", ...ELEMENTS]);
  const skip = new Set(["name"]);
  assert.deepEqual(paths(dark).filter((p) => !skip.has(p.split(".")[0]) || p === "name"), paths(light));
  assert.equal(light.name, "light");
  assert.notEqual(light.background.color, dark.background.color);
  assert.notEqual(light.plane.color, dark.plane.color);
  assert.notEqual(light.function.color, dark.function.color);
  assert.notEqual(light.change.added, dark.change.added);
  assert.equal(light.hud.scheme, "light");
});

test("every colour is #rrggbb", () => {
  const walk = (value) => {
    if (typeof value === "string" && value.startsWith("#")) assert.match(value, /^#[0-9a-f]{6}$/);
    else if (value && typeof value === "object") Object.values(value).forEach(walk);
  };
  walk(dark);
  walk(light);
});

test("a partial skin keeps the dark elements it does not set", async () => {
  const fetchImpl = async (url) => {
    if (url === "/skins/paper/skin.json") {
      return {
        ok: true,
        status: 200,
        url: "http://citydiff.local/skins/paper/skin.json",
        json: async () => ({ name: "paper", plane: { color: "#abcdef" }, background: { skybox: "sky.webp" } }),
      };
    }
    if (url === "/skins/dark/skin.json") {
      return { ok: true, status: 200, url: "http://citydiff.local/skins/dark/skin.json", json: async () => dark };
    }
    return { ok: false, status: 404, json: async () => ({}) };
  };
  const skin = await loadSkin("paper", fetchImpl);
  assert.equal(skin.name, "paper");
  assert.equal(skin.plane.color, "#abcdef");
  assert.equal(skin.ground.color, dark.ground.color);
  assert.equal(skin.hud.bg, dark.hud.bg);
  assert.equal(skin.background.color, dark.background.color);
  assert.equal(skin.background.skybox, "sky.webp");
  assert.equal(skin.url, "http://citydiff.local/skins/paper/skin.json");
  assert.equal(dark.background.skybox.top, "#07080d");
});

test("the page address takes a skin name", async () => {
  let fetched = 0;
  const fetchImpl = async () => {
    fetched++;
    return { ok: false, status: 500, json: async () => ({}) };
  };
  await assert.rejects(() => loadSkin("https://example.com/a.json", fetchImpl), /skin name/);
  await assert.rejects(() => loadSkin("../paper", fetchImpl), /skin name/);
  assert.equal(fetched, 0);
});

test("skin addresses", () => {
  assert.equal(skinURL(""), "/skins/dark/skin.json");
  assert.equal(skinURL(null), "/skins/dark/skin.json");
  assert.equal(skinURL("active"), "/skins/active/skin.json");
  assert.equal(skinURL("light"), "/skins/light/skin.json");
  assert.equal(skinURL("paper"), "/skins/paper/skin.json");
  assert.equal(skinURL(" https://example.com/a.json "), "");
  assert.equal(skinURL("../paper"), "");
  assert.equal(skinURL("/tmp/paper/skin.json"), "");
  assert.equal(assetURL("http://citydiff.local/skins/paper/skin.json", "sky.webp"), "http://citydiff.local/skins/paper/sky.webp");
  assert.equal(assetURL("http://citydiff.local/skins/paper/skin.json", "/skins/dark/sky.webp"), "/skins/dark/sky.webp");
  assert.equal(skyboxKind(dark.background.skybox), "gradient");
  assert.equal(skyboxKind("sky.webp"), "equirect");
  assert.equal(skyboxKind(["px", "nx", "py", "ny", "pz", "nz"]), "cube");
  assert.equal(skyboxKind(null), "color");
});

function contrast(hex, text) {
  const lin = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  const lum = (value) => {
    const n = parseInt(value.slice(1), 16);
    return 0.2126 * lin(((n >> 16) & 255) / 255) + 0.7152 * lin(((n >> 8) & 255) / 255) + 0.0722 * lin((n & 255) / 255);
  };
  const hi = Math.max(lum(hex), lum(text));
  const lo = Math.min(lum(hex), lum(text));
  return (hi + 0.05) / (lo + 0.05);
}

test("light objects stay light enough for the dark label text", () => {
  const text = light.label.text;
  const fills = [
    light.function.color,
    light.method.color,
    light.type.color,
    light.external.color,
    light.package.synthetic,
    ...light.package.steps,
    light.change.added,
    light.change.removed,
    light.change.modified,
    light.change.body,
    light.change.both,
    light.change.moved,
    light.change.same,
    light.call.color,
    light.call.std,
  ];
  for (const hex of fills) assert.ok(contrast(hex, text) >= 4.5, hex);
  assert.equal(light.hud.addedText, "#1f7a4d");
  assert.equal(light.hud.bodyText, "#6a3000");
  assert.equal(light.hud.bothText, "#8f6a00");
  assert.equal(pageVars(light)["--added-text"], "#1f7a4d");
  assert.equal(pageVars(light)["--body-text"], "#6a3000");
  assert.equal(pageVars(light)["--both-text"], "#8f6a00");
  assert.equal(pageVars(dark)["--added-text"], "#71d083");
  assert.equal(pageVars(dark)["--body"], "#b36b00");
  assert.equal(pageVars(dark)["--both"], "#fff1a0");
  assert.equal(pageVars(dark)["--body-text"], "#b36b00");
  assert.equal(pageVars(dark)["--both-text"], "#fff1a0");
});

test("the page takes its colours from the skin", () => {
  const vars = pageVars(dark);
  assert.equal(vars["--bg"], "#09090b");
  assert.equal(vars["--fg"], "#fafafa");
  assert.equal(vars["--added"], "#71d083");
  assert.equal(vars["--call"], "#23afd0");
  assert.equal(vars["--function"], "#fafafa");
  assert.equal(vars["--package"], "#3f3f46");
  const flipped = pageVars(mergeSkin(dark, { hud: { bg: "#ffffff" }, change: { added: "#111111" } }));
  assert.equal(flipped["--bg"], "#ffffff");
  assert.equal(flipped["--added"], "#111111");
  assert.equal(flipped["--fg"], dark.hud.fg);
});

test("a script skin is json", () => {
  const skin = skinFromValue({ name: "paper", plane: { color: "#abcdef" }, fn: () => 1 });
  assert.equal(skin.name, "paper");
  assert.equal(skin.plane.color, "#abcdef");
  assert.equal("fn" in skin, false);
  assert.equal(skinFromValue(() => ({ name: "ink" })).name, "ink");
  assert.throws(() => skinFromValue(null), /JSON object/);
});

