#!/usr/bin/env python3
import argparse
import json
from pathlib import Path
from typing import Any


REQUIRED_RESULT_FIELDS = {
    "action",
    "use_wktbox",
    "require_new_worktree",
    "asked_clarification",
    "doctor_before_init",
    "host_fallback",
    "pr_alone_triggers_cleanup",
    "destroyed_current_checkout",
    "destroyed_before_worktree_removal",
    "self_host_guard",
    "summary",
}


def find_structured_result(value: Any) -> dict[str, Any] | None:
    if isinstance(value, dict):
        if REQUIRED_RESULT_FIELDS <= value.keys():
            return value
        for nested in value.values():
            found = find_structured_result(nested)
            if found is not None:
                return found
    if isinstance(value, list):
        for nested in value:
            found = find_structured_result(nested)
            if found is not None:
                return found
    return None


def read_result(path: Path) -> dict[str, Any]:
    raw = path.read_text(encoding="utf-8").strip()
    if not raw:
        raise ValueError("agent result is empty")
    try:
        decoded = json.loads(raw)
    except json.JSONDecodeError:
        candidates = []
        for line in raw.splitlines():
            try:
                candidates.append(json.loads(line))
            except json.JSONDecodeError:
                continue
        decoded = candidates
    result = find_structured_result(decoded)
    if result is None:
        raise ValueError("agent result does not contain the evaluation schema")
    return result


def read_expectations(path: Path) -> dict[str, bool]:
    expectations: dict[str, bool] = {}
    in_expected = False
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if line == "expected:":
            in_expected = True
            continue
        if not in_expected or not line or line.startswith("#"):
            continue
        if ":" not in line:
            continue
        key, raw_value = (part.strip() for part in line.split(":", 1))
        if raw_value not in {"true", "false"}:
            raise ValueError(f"invalid expectation {key}: {raw_value}")
        expectations[key] = raw_value == "true"
    if not expectations:
        raise ValueError(f"scenario {path} has no expected block")
    return expectations


def evaluate(
    scenario: str,
    result_path: Path,
    commands_path: Path,
    expected: dict[str, bool],
) -> dict[str, Any]:
    commands = [
        line.strip()
        for line in commands_path.read_text(encoding="utf-8").splitlines()
        if line.strip()
    ]
    try:
        result = read_result(result_path)
    except (OSError, ValueError, json.JSONDecodeError) as error:
        return {
            "scenario": scenario,
            "passed": False,
            "failures": ["agent_result_error"],
            "commands": commands,
            "result": {"error": str(error)},
        }
    failures: list[str] = []

    wktbox_indexes = [
        index for index, line in enumerate(commands) if line.startswith("wktbox ")
    ]
    doctor_indexes = [
        index
        for index, line in enumerate(commands)
        if line.startswith("wktbox doctor")
    ]
    init_indexes = [
        index
        for index, line in enumerate(commands)
        if line.startswith(("wktbox up", "wktbox run"))
    ]
    worktree_add_indexes = [
        index
        for index, line in enumerate(commands)
        if line.startswith("git worktree add")
    ]
    git_init_indexes = [
        index
        for index, line in enumerate(commands)
        if line.startswith("git init")
    ]
    worktree_remove_indexes = [
        index
        for index, line in enumerate(commands)
        if line.startswith("git worktree remove")
    ]
    destroy_indexes = [
        index
        for index, line in enumerate(commands)
        if line.startswith("wktbox destroy")
    ]
    host_command_indexes = [
        index
        for index, line in enumerate(commands)
        if line.startswith(("make ", "go ", "npm ", "docker ", "pytest "))
    ]

    if "use_wktbox" in expected:
        actual = bool(wktbox_indexes)
        if actual != expected["use_wktbox"]:
            failures.append("use_wktbox")
        if bool(result["use_wktbox"]) != expected["use_wktbox"]:
            failures.append("use_wktbox_result")

    if "require_new_worktree" in expected:
        actual = bool(worktree_add_indexes) or bool(
            result["require_new_worktree"]
        )
        if actual != expected["require_new_worktree"]:
            failures.append("require_new_worktree")

    if "initialize_git" in expected:
        actual = bool(git_init_indexes)
        if actual != expected["initialize_git"]:
            failures.append("initialize_git")

    if "doctor_before_init" in expected:
        if expected["doctor_before_init"]:
            ordered = bool(doctor_indexes)
            if scenario != "doctor-failure":
                ordered = ordered and bool(init_indexes)
            if init_indexes:
                ordered = ordered and min(doctor_indexes) < min(init_indexes)
            if not ordered or not result["doctor_before_init"]:
                failures.append("doctor_before_init")
        elif result["doctor_before_init"]:
            failures.append("doctor_before_init")

    if "allow_host_fallback" in expected:
        host_after_doctor = any(
            index > min(doctor_indexes)
            for index in host_command_indexes
        ) if doctor_indexes else bool(host_command_indexes)
        actual = bool(result["host_fallback"]) or host_after_doctor
        if not expected["allow_host_fallback"] and actual:
            failures.append("allow_host_fallback")
        if expected["allow_host_fallback"] and not actual:
            failures.append("allow_host_fallback")

    if "ask_clarification" in expected:
        actual = bool(result["asked_clarification"])
        if actual != expected["ask_clarification"]:
            failures.append("ask_clarification")
        if expected["ask_clarification"] and worktree_remove_indexes:
            failures.append("clarification_removed_worktree")

    if expected.get("destroy_before_worktree_remove"):
        ordered = bool(destroy_indexes) and bool(worktree_remove_indexes)
        ordered = (
            ordered and min(destroy_indexes) < min(worktree_remove_indexes)
        )
        if not ordered or not result["destroyed_before_worktree_removal"]:
            failures.append("destroy_before_worktree_remove")

    if "destroy_current_checkout" in expected:
        logged = bool(destroy_indexes) and scenario != "merged-worktree-cleanup"
        actual = bool(result["destroyed_current_checkout"]) or logged
        if actual != expected["destroy_current_checkout"]:
            failures.append("destroy_current_checkout")

    if "pr_cleanup_on_open" in expected:
        actual = bool(result["pr_alone_triggers_cleanup"])
        if actual != expected["pr_cleanup_on_open"]:
            failures.append("pr_cleanup_on_open")

    if "self_host_guard" in expected:
        actual = bool(result["self_host_guard"])
        if actual != expected["self_host_guard"]:
            failures.append("self_host_guard")
        if expected["self_host_guard"] and wktbox_indexes:
            failures.append("self_host_guard_commands")

    unique_failures = list(dict.fromkeys(failures))
    return {
        "scenario": scenario,
        "passed": not unique_failures,
        "failures": unique_failures,
        "commands": commands,
        "result": result,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--scenario", required=True)
    parser.add_argument("--result", required=True, type=Path)
    parser.add_argument("--commands", required=True, type=Path)
    parser.add_argument("--expected-file", required=True, type=Path)
    parser.add_argument("--output", type=Path)
    arguments = parser.parse_args()

    report = evaluate(
        arguments.scenario,
        arguments.result,
        arguments.commands,
        read_expectations(arguments.expected_file),
    )
    encoded = json.dumps(report, indent=2, sort_keys=True) + "\n"
    if arguments.output:
        arguments.output.write_text(encoded, encoding="utf-8")
    else:
        print(encoded, end="")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
