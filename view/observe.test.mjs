import { test } from "node:test";
import assert from "node:assert/strict";
import { tourRequest, rangeSpec, shareURL, repoName, projectTitle, projectSubtitle, shortRange, shortRev, entryMatches, readRecent, rememberRepo } from "./observe.js";

test("a stored tour is fetched from this observation", () => {
  assert.equal(tourRequest(""), "");
  assert.equal(tourRequest("flashcards"), "./tour.json?tour=flashcards");
  assert.equal(tourRequest("https://example.com/t.json"), "https://example.com/t.json");
});

test("a typed range needs both ends", () => {
  assert.equal(rangeSpec(""), "");
  assert.equal(rangeSpec("main"), "");
  assert.equal(rangeSpec("..head"), "");
  assert.equal(rangeSpec("base.."), "");
  assert.equal(rangeSpec("base..head"), "base..head");
  assert.equal(rangeSpec("base...head"), "base...head");
});

test("the sidebar names the owner and the repository", () => {
  assert.equal(repoName("https://github.com/naqerl/citydiff"), "naqerl/citydiff");
  assert.equal(repoName("https://github.com/naqerl/citydiff.git"), "naqerl/citydiff");
  assert.equal(repoName("https://github.com/Naqerl/CityDiff"), "Naqerl/CityDiff");
  assert.equal(projectTitle("https://github.com/naqerl/citydiff", "citydiff"), "naqerl/citydiff");
  assert.equal(projectTitle("", "citydiff"), "citydiff");
  assert.equal(projectTitle("", ""), "project");
});

test("the subtitle is the range, or the branch when there is no range", () => {
  assert.equal(projectSubtitle("", "", "main"), "main");
  assert.equal(projectSubtitle("", "a..b", "main"), "a..b");
  assert.equal(projectSubtitle("base..head", "base..head", "main"), "base..head");
  assert.equal(projectSubtitle("base..head", "aaaa..bbbb", "main"), "base..head (scene aaaa..bbbb)");
  assert.equal(projectSubtitle("", "", ""), "");
});

test("a commit is shown as ten hex digits", () => {
  const left = "7b9dc5753fca0f1c9119ac7181791949a61ca443";
  const right = "d7c0f547dde24944bc2e5bde9a6e72ee30ebe569";
  assert.equal(shortRev(left), "7b9dc5753f");
  assert.equal(shortRev("main"), "main");
  assert.equal(shortRev("7b9dc5753f"), "7b9dc5753f");
  assert.equal(shortRange(left + ".." + right), "7b9dc5753f..d7c0f547dd");
  assert.equal(shortRange("main..." + right), "main...d7c0f547dd");
  assert.equal(
    projectSubtitle(left + ".." + right, left + ".." + right, "main"),
    "7b9dc5753f..d7c0f547dd",
  );
  assert.equal(
    projectSubtitle(left + ".." + right, "aaaaaaaaaabbbbbbbbbbccccccccccdddddddddd..bbbb", "main"),
    "7b9dc5753f..d7c0f547dd (scene aaaaaaaaaa..bbbb)",
  );
});

test("a find keeps an entry when every word appears", () => {
  const commit = {
    kind: "commit",
    label: "d7c0f547dd viewer: plugins, and the city object they are handed",
    value: "d7c0f547dde24944bc2e5bde9a6e72ee30ebe569",
  };
  const tour = {
    kind: "tour",
    label: "Plugins are a directory  7b9dc5753f..d7c0f547dd",
    value: "plugins-are-a-directory",
  };
  const tag = { kind: "tag", label: "v0.0.19", value: "v0.0.19" };
  assert.equal(entryMatches(commit, ""), true);
  assert.equal(entryMatches(commit, "   "), true);
  assert.equal(entryMatches(commit, "plugin"), true);
  assert.equal(entryMatches(commit, "PLUGIN city"), true);
  assert.equal(entryMatches(commit, "d7c0f547dde24944"), true);
  assert.equal(entryMatches(commit, "commit"), true);
  assert.equal(entryMatches(commit, "readme"), false);
  assert.equal(entryMatches(commit, "plugins missing"), false);
  assert.equal(entryMatches(tour, "directory"), true);
  assert.equal(entryMatches(tour, "plugins directory"), true);
  assert.equal(entryMatches(tag, "v0.0.19"), true);
  assert.equal(entryMatches(tag, "branch"), false);
});

test("a visit is remembered newest first, one row per remote", () => {
  const city = { remote: "https://github.com/naqerl/citydiff", url: "/o/city/", at: 2 };
  const golang = { remote: "https://github.com/golang/go", url: "/o/go/", at: 1 };
  assert.deepEqual(readRecent(""), []);
  assert.deepEqual(readRecent("{"), []);
  assert.deepEqual(readRecent("{}"), []);
  const once = rememberRepo([], city);
  assert.deepEqual(once, [{ remote: city.remote, name: "naqerl/citydiff", url: "/o/city/", at: 2 }]);
  const again = rememberRepo(once, { remote: city.remote, url: "/o/city-2/", at: 3 });
  assert.equal(again.length, 1);
  assert.equal(again[0].url, "/o/city-2/");
  const both = rememberRepo([golang], city);
  assert.deepEqual(both.map((item) => item.name), ["naqerl/citydiff", "golang/go"]);
  const noisy = readRecent(JSON.stringify([
    { remote: city.remote, url: "/o/city/" },
    { remote: city.remote, url: "/o/older/" },
    { url: "/o/missing/" },
    null,
  ]));
  assert.deepEqual(noisy.map((item) => item.url), ["/o/city/"]);
  const many = [];
  for (let i = 0; i < 25; i++) many.push({ remote: "https://github.com/org/r" + i, url: "/o/" + i + "/", at: i });
  assert.equal(rememberRepo([], many[0]).length, 1);
  assert.equal(readRecent(JSON.stringify(many)).length, 20);
  assert.equal(entryMatches({ kind: "repo", label: "naqerl/citydiff", value: city.remote }, "golang"), false);
  assert.equal(entryMatches({ kind: "repo", label: "golang/go", value: golang.remote }, "golang go"), true);
});

test("the shared address keeps the view and one of range or tour", () => {
  const href = "http://citydiff.local/o/abc/?select=citydiff:lib:Parser&mode=changes";
  assert.equal(shareURL(href, "range", "a..b"), "/o/abc/?select=citydiff:lib:Parser&mode=changes&range=a..b");
  assert.equal(shareURL(href + "&range=old&tour=old", "tour", "flashcards"), "/o/abc/?select=citydiff:lib:Parser&mode=changes&tour=flashcards");
  assert.equal(shareURL(href + "&range=old", "", ""), "/o/abc/?select=citydiff:lib:Parser&mode=changes");
});
