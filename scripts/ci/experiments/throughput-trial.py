#!/usr/bin/env python3
"""Temporary read-only hosted trial; never a replacement for merge validation."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import statistics
import subprocess
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=["planner", "cache", "race", "consume"])
    parser.add_argument("--base", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--cache", type=Path)
    parser.add_argument("--artifact", type=Path)
    args = parser.parse_args()
    root = Path.cwd()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    records = []
    result = {
        "mode": args.mode,
        "head": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
        "base": args.base,
        "platform": platform.platform(),
        "go": subprocess.check_output(["go", "version"], text=True).strip() if args.mode != "consume" else None,
        "run_id": os.environ.get("GITHUB_RUN_ID"),
        "attempt": os.environ.get("GITHUB_RUN_ATTEMPT"),
        "runner_image": os.environ.get("ImageVersion"),
        "records": records,
    }

    def save():
        (output / "results.json").write_text(json.dumps(result, indent=2) + "\n")

    def run(name, command, env=None, cwd=root):
        start = time.monotonic()
        completed = subprocess.run(command, cwd=cwd, env=env, text=True, capture_output=True)
        elapsed = time.monotonic() - start
        (output / (name + ".log")).write_text(completed.stdout + completed.stderr)
        records.append({"name": name, "command": command, "cwd": str(cwd),
                        "seconds": elapsed, "exit_code": completed.returncode})
        save()
        print(f"{name}: {elapsed:.3f}s exit={completed.returncode}", flush=True)
        completed.check_returncode()
        return completed.stdout

    def cache_env(path):
        path.mkdir(parents=True, exist_ok=True)
        return {**os.environ, "GOCACHE": str(path)}

    def check_test(text, target, json_events):
        if json_events:
            events = [json.loads(line) for line in text.splitlines() if line]
            matches = [e for e in events if e.get("Action") == "pass" and e.get("Test") == target]
            package_pass = any(e.get("Action") == "pass" and not e.get("Test") for e in events)
            if len(matches) != 1 or not package_pass:
                raise RuntimeError("test/package completion missing")
        elif text.count("--- PASS: " + target + " ") != 1 or "SKIP" in text or "\nPASS\n" not in text:
            raise RuntimeError("direct binary did not complete selected test exactly once")

    try:
        if args.mode == "planner":
            old = output / "baseline.go"
            old.write_bytes(subprocess.check_output(["git", "show", args.base + ":scripts/ci/analysis-shards-go/main.go"]))
            new = root / "scripts/ci/analysis-shards-go/main.go"
            result["planner_sha256"] = {"base": hashlib.sha256(old.read_bytes()).hexdigest(),
                                        "candidate": hashlib.sha256(new.read_bytes()).hexdigest()}

            def workflow_step(source):
                marker = "      - name: Plan complete test, race, and fuzz shards\n"
                if source.count(marker) != 1:
                    raise RuntimeError("expected exactly one planner step")
                tail = source.split(marker, 1)[1].split("        run: |\n", 1)[1]
                lines = []
                indent = None
                for line in tail.splitlines():
                    if line:
                        leading = len(line) - len(line.lstrip(" "))
                        if indent is None:
                            indent = leading
                            if indent <= 8:
                                raise RuntimeError("invalid planner block indentation")
                        if leading < indent:
                            break
                    lines.append(line[indent:] if line else "")
                if not lines:
                    raise RuntimeError("empty planner workflow step")
                return "\n".join(lines) + "\n"

            baseline_workflow = subprocess.check_output(["git", "show", args.base + ":.github/workflows/ci.yml"], text=True)
            candidate_workflow = (root / ".github/workflows/ci.yml").read_text()
            scripts = {}
            for variant, source in [("old", baseline_workflow), ("new", candidate_workflow)]:
                scripts[variant] = output / (variant + "-workflow-step.sh")
                scripts[variant].write_text(workflow_step(source))
            result["comparison"] = "Actual planner workflow steps over the same candidate inventory. New step uses candidate as future trusted base; this does not change production trust or publish merge proof."

            def plan(variant, label):
                work = output / (label + "-work")
                work.mkdir()
                env = {**os.environ, "BASE_SHA": args.base if variant == "old" else result["head"],
                       "EVENT_NAME": "pull_request", "RUNNER_TEMP": str(work),
                       "GITHUB_OUTPUT": str(work / "outputs")}
                run(label, ["bash", "-e", str(scripts[variant])], env=env)
                plans = {}
                for line in (work / "outputs").read_text().splitlines():
                    key, value = line.split("=", 1)
                    if key in plans:
                        raise RuntimeError("duplicate planner output")
                    plans[key] = json.loads(value)
                (output / (label + ".json")).write_text(json.dumps(plans, indent=2))
                return plans, records[-1]["seconds"]

            # First invocations share a runner, so these are not a cold A/B pair.
            expected, first_old = plan("old", "prime-old")
            candidate, first_new = plan("new", "prime-new")
            if candidate != expected:
                raise RuntimeError("candidate plan differs from trusted baseline")
            result["first_invocations_not_cold_pair"] = {"old": first_old, "new": first_new}
            result["trials"] = []
            for repeat in range(4):
                for variant in (["old", "new"] if repeat % 2 == 0 else ["new", "old"]):
                    actual, elapsed = plan(variant, f"repeat-{repeat}-{variant}")
                    if actual != expected:
                        raise RuntimeError("plan changed across equivalent trials")
                    result["trials"].append({"repeat": repeat, "variant": variant, "seconds": elapsed})
                    save()
            result["median_seconds"] = {v: statistics.median(t["seconds"] for t in result["trials"] if t["variant"] == v) for v in ["old", "new"]}
            result["inventory"] = {k: sum(map(len, expected[k])) for k in ["race_plan", "fuzz_plan", "isolation_plan"]}
        elif args.mode == "cache":
            if args.cache is None:
                raise RuntimeError("cache path required")
            restored = args.cache.resolve()
            result["cache_primary_key"] = os.environ.get("TRIAL_CACHE_KEY")
            result["cache_matched_key"] = os.environ.get("TRIAL_CACHE_MATCH")
            result["cache_hit"] = os.environ.get("TRIAL_CACHE_HIT")
            result["restored_files"] = sum(1 for p in restored.rglob("*") if p.is_file())
            result["restored_bytes"] = sum(p.stat().st_size for p in restored.rglob("*") if p.is_file())
            if not result["cache_matched_key"] or not result["restored_files"]:
                result["comparison_available"] = False
                raise RuntimeError("trusted cache was not restored; cold-versus-cold is not a valid cache comparison")
            result["comparison_available"] = True
            # Prefetch separately: this comparison isolates compiler cache, not network.
            run("modules", ["go", "mod", "download"])
            command = ["go", "test", "-c", "-o", "/dev/null", "./internal/cli", "./scripts/ci"]
            run("restored-first", command, cache_env(restored))
            cold = output / "cold-build-cache"
            if cold.exists():
                raise RuntimeError("cold cache unexpectedly exists")
            run("cold-first", command, cache_env(cold))
            run("cold-now-warm", command, cache_env(cold))
            run("restored-now-warm", command, cache_env(restored))
            result["limitation"] = "One cold/restored pair; shared module cache; restore step and job costs are reported separately; compile-only, not test execution."
        elif args.mode == "race":
            target = "TestOperatorCustodyRejectsSymlinksAndUnsafeMode"
            pattern = "^(" + target + ")$"
            package = "./internal/upgradegate"
            command = ["go", "test", "-json", "-race", "-vet=off", "-count=1", "-timeout=20m", "-run", pattern, package]
            check_test(run("prime", command), target, True)
            binary = output / "upgradegate-race.test"
            direct = [str(binary), "-test.v", "-test.count=1", "-test.timeout=20m", "-test.run=" + pattern]
            result["trials"] = []
            for repeat, order in enumerate([["go-test", "binary"], ["binary", "go-test"]]):
                for variant in order:
                    start = time.monotonic()
                    if variant == "binary":
                        run(f"compile-{repeat}", ["go", "test", "-c", "-race", "-vet=off", "-o", str(binary), package])
                    for index in range(6):
                        text = run(f"{repeat}-{variant}-{index}", command if variant == "go-test" else direct,
                                   cwd=root if variant == "go-test" else root / "internal/upgradegate")
                        check_test(text, target, variant == "go-test")
                    result["trials"].append({"repeat": repeat, "variant": variant, "seconds": time.monotonic() - start})
                    save()
            metadata = run("binary-metadata", ["go", "version", "-m", str(binary)])
            if "-race=true" not in metadata:
                raise RuntimeError("race instrumentation missing")
            result["binary_bytes"] = binary.stat().st_size
            result["binary_sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
            result["target"] = target
            # Bind the transferred executable to the producing source/run.
            (output / "manifest.json").write_text(json.dumps({k: result[k] for k in ["head", "base", "run_id", "attempt", "target", "binary_sha256", "go"]}, indent=2))
            result["limitation"] = "Warm shared build cache; one selected test; not full-suite equivalence."
        else:
            if args.artifact is None:
                raise RuntimeError("artifact directory required")
            artifact = args.artifact.resolve()
            manifest = json.loads((artifact / "manifest.json").read_text())
            for key in ["head", "base", "run_id", "attempt"]:
                if manifest[key] != result[key]:
                    raise RuntimeError("artifact producer identity mismatch: " + key)
            binary = artifact / "upgradegate-race.test"
            if hashlib.sha256(binary.read_bytes()).hexdigest() != manifest["binary_sha256"]:
                raise RuntimeError("artifact binary digest mismatch")
            result["producer_go"] = manifest["go"]
            binary.chmod(0o700)
            target = manifest["target"]
            if target != "TestOperatorCustodyRejectsSymlinksAndUnsafeMode":
                raise RuntimeError("unexpected target")
            for index in range(6):
                text = run(f"transferred-{index}", [str(binary), "-test.v", "-test.count=1", "-test.timeout=20m", "-test.run=^(" + target + ")$"], cwd=root / "internal/upgradegate")
                check_test(text, target, False)
            result["limitation"] = "Read-only test job only; producer is experimental PR code, not a trusted merge attestation."
        result["conclusion"] = "success"
    except Exception as error:
        result["conclusion"] = "failure"
        result["error"] = str(error)
        raise
    finally:
        save()


if __name__ == "__main__":
    main()
