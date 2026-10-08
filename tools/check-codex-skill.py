"""Offline host-policy regression probe; requires an installed Codex CLI."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
MARKER = "MSS_COMPAT_EXPLICIT_BODY_71cb"


def prompt(env, cwd, text):
    result = subprocess.run(
        ["codex", "debug", "prompt-input", text], cwd=cwd, env=env,
        text=True, capture_output=True, timeout=30, check=True,
    )
    return json.dumps(json.loads(result.stdout), ensure_ascii=False)


with tempfile.TemporaryDirectory(prefix="mss-codex-compat-") as temporary:
    home = Path(temporary)
    cwd = home / "project"
    cwd.mkdir()
    (home / ".codex").mkdir()
    target = home / ".agents/skills/mss"
    target.mkdir(parents=True)
    env = {key: value for key, value in os.environ.items()
           if not any(word in key for word in ("TOKEN", "KEY", "SECRET", "AUTH", "SUPER_ENGINEERING", "SUPERCONDUCTOR"))}
    env.update(HOME=str(home), USERPROFILE=str(home), CODEX_HOME=str(home / ".codex"))
    version = subprocess.check_output(["codex", "--version"], env=env, text=True).strip()
    for language in ("SKILL.md", "SKILL.zh-CN.md"):
        content = (ROOT / "skills/mss" / language).read_text()
        (target / "SKILL.md").write_text(content + "\n" + MARKER + "\n")
        policy = target / "agents/openai.yaml"
        policy.parent.mkdir(exist_ok=True)
        if policy.exists():
            policy.unlink()
        control = prompt(env, cwd, "we discussed this before")
        assert "- mss:" in control, "control must advertise MSS without Codex policy"
        shutil.copyfile(ROOT / "skills/mss/agents/openai.yaml", policy)
        automatic = prompt(env, cwd, "we discussed this before")
        assert "- mss:" not in automatic, "ordinary chat must not advertise MSS"
        assert MARKER not in automatic, "ordinary chat must not expand MSS"
        explicit = prompt(env, cwd, "$mss frobnicator")
        status = "expanded" if MARKER in explicit else "not expanded: use explicit file-read request or tested TUI picker"
        print(f"{version}, {language}: automatic invocation disabled; non-TUI $mss {status}")
