// Run against an installed Pi SDK; no model request or package installation.
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

if (!process.argv[2]) throw new Error("usage: node tools/check-pi-skill.mjs /absolute/path/to/pi/skills.js");
const imported = await import(pathToFileURL(resolve(process.argv[2])).href);
const sdk = typeof imported.loadSkillsFromDir === "function" ? imported : imported.dist_exports;
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const scratch = await mkdtemp(join(tmpdir(), "mss-pi-compat-"));
try {
  for (const language of ["SKILL.md", "SKILL.zh-CN.md"]) {
    const dir = join(scratch, language, "mss");
    await mkdir(dir, { recursive: true });
    const content = await readFile(join(root, "skills/mss", language), "utf8");
    await writeFile(join(dir, "SKILL.md"), content);
    const loaded = sdk.loadSkillsFromDir({ dir, source: "path" });
    assert.deepEqual(loaded.diagnostics ?? loaded.warnings, [], language);
    assert.equal(loaded.skills.length, 1, language);
    assert.equal(loaded.skills[0].name, "mss");
    assert.equal(loaded.skills[0].disableModelInvocation, true);
    assert.equal(sdk.formatSkillsForPrompt(loaded.skills), "", "ordinary conversation must not advertise MSS");

    // Prove the boundary is the host policy, not a description saying 'manual'.
    await writeFile(join(dir, "SKILL.md"), content.replace("disable-model-invocation: true", "hide: true"));
    const legacy = sdk.loadSkillsFromDir({ dir, source: "path" });
    assert.match(sdk.formatSkillsForPrompt(legacy.skills), /<name>mss<\/name>/);
    console.log(`${language}: loaded for explicit use; absent from automatic prompt; legacy hide control exposed`);
  }
} finally {
  await rm(scratch, { recursive: true, force: true });
}
