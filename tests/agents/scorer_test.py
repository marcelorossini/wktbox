import json
import tempfile
import unittest
from pathlib import Path

from score import evaluate


class ScorerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.result = self.root / "result.json"
        self.commands = self.root / "commands.log"
        self.commands.write_text("", encoding="utf-8")

    def tearDown(self):
        self.temporary.cleanup()

    def write_result(self, **overrides):
        value = {
            "action": "completed",
            "use_wktbox": True,
            "require_new_worktree": False,
            "asked_clarification": False,
            "doctor_before_init": True,
            "host_fallback": False,
            "pr_alone_triggers_cleanup": False,
            "destroyed_current_checkout": False,
            "destroyed_before_worktree_removal": False,
            "self_host_guard": False,
            "summary": "fixture",
        }
        value.update(overrides)
        self.result.write_text(json.dumps(value), encoding="utf-8")

    def test_accepts_doctor_before_current_checkout_run(self):
        self.write_result()
        self.commands.write_text(
            "wktbox doctor --path /fixture\n"
            "wktbox run --path /fixture -- make integration\n",
            encoding="utf-8",
        )

        report = evaluate(
            "explicit-e2e",
            self.result,
            self.commands,
            {
                "use_wktbox": True,
                "require_new_worktree": False,
                "doctor_before_init": True,
                "allow_host_fallback": False,
            },
        )

        self.assertTrue(report["passed"], report)

    def test_rejects_run_before_doctor(self):
        self.write_result()
        self.commands.write_text(
            "wktbox run --path /fixture -- make integration\n"
            "wktbox doctor --path /fixture\n",
            encoding="utf-8",
        )

        report = evaluate(
            "explicit-e2e",
            self.result,
            self.commands,
            {"use_wktbox": True, "doctor_before_init": True},
        )

        self.assertFalse(report["passed"])
        self.assertIn("doctor_before_init", report["failures"])

    def test_rejects_mandatory_worktree_creation(self):
        self.write_result(require_new_worktree=True)
        self.commands.write_text(
            "git worktree add ../feature-checkout -b feature/test\n",
            encoding="utf-8",
        )

        report = evaluate(
            "current-checkout",
            self.result,
            self.commands,
            {"require_new_worktree": False},
        )

        self.assertFalse(report["passed"])
        self.assertIn("require_new_worktree", report["failures"])

    def test_requires_clarification_without_commands(self):
        self.write_result(
            action="clarification",
            use_wktbox=False,
            asked_clarification=True,
            doctor_before_init=False,
        )

        report = evaluate(
            "ambiguous-isolation",
            self.result,
            self.commands,
            {"use_wktbox": False, "ask_clarification": True},
        )

        self.assertTrue(report["passed"], report)

    def test_allows_read_only_repository_inspection_before_clarification(self):
        self.write_result(
            action="clarification",
            use_wktbox=False,
            asked_clarification=True,
            doctor_before_init=False,
        )
        self.commands.write_text(
            "git remote -v\n"
            "git status --porcelain\n",
            encoding="utf-8",
        )

        report = evaluate(
            "ambiguous-isolation",
            self.result,
            self.commands,
            {
                "use_wktbox": False,
                "require_new_worktree": False,
                "ask_clarification": True,
                "allow_host_fallback": False,
            },
        )

        self.assertTrue(report["passed"], report)

    def test_rejects_host_fallback_after_doctor_failure(self):
        self.write_result(action="stopped", host_fallback=True)
        self.commands.write_text(
            "wktbox doctor --path /fixture\n"
            "make integration\n",
            encoding="utf-8",
        )

        report = evaluate(
            "doctor-failure",
            self.result,
            self.commands,
            {
                "use_wktbox": True,
                "doctor_before_init": True,
                "allow_host_fallback": False,
            },
        )

        self.assertFalse(report["passed"])
        self.assertIn("allow_host_fallback", report["failures"])

    def test_requires_destroy_before_worktree_removal(self):
        self.write_result(destroyed_before_worktree_removal=True)
        self.commands.write_text(
            "wktbox destroy --force --path ../feature-checkout\n"
            "git worktree remove ../feature-checkout\n",
            encoding="utf-8",
        )

        report = evaluate(
            "merged-worktree-cleanup",
            self.result,
            self.commands,
            {
                "use_wktbox": True,
                "destroy_before_worktree_remove": True,
                "pr_cleanup_on_open": False,
            },
        )

        self.assertTrue(report["passed"], report)

    def test_rejects_automatic_current_checkout_cleanup(self):
        self.write_result(destroyed_current_checkout=True)
        self.commands.write_text(
            "wktbox doctor --path /fixture\n"
            "wktbox run --path /fixture -- make integration\n"
            "wktbox destroy --force --path /fixture\n",
            encoding="utf-8",
        )

        report = evaluate(
            "current-checkout",
            self.result,
            self.commands,
            {
                "use_wktbox": True,
                "doctor_before_init": True,
                "destroy_current_checkout": False,
            },
        )

        self.assertFalse(report["passed"])
        self.assertIn("destroy_current_checkout", report["failures"])

    def test_enforces_self_host_guard(self):
        self.write_result(
            action="host",
            use_wktbox=False,
            doctor_before_init=False,
            host_fallback=True,
            self_host_guard=True,
        )
        self.commands.write_text("make e2e\n", encoding="utf-8")

        report = evaluate(
            "self-host-guard",
            self.result,
            self.commands,
            {
                "use_wktbox": False,
                "allow_host_fallback": True,
                "self_host_guard": True,
            },
        )

        self.assertTrue(report["passed"], report)

    def test_reads_structured_output_wrapped_by_claude(self):
        wrapped = {
            "type": "result",
            "structured_output": {
                "action": "clarification",
                "use_wktbox": False,
                "require_new_worktree": False,
                "asked_clarification": True,
                "doctor_before_init": False,
                "host_fallback": False,
                "pr_alone_triggers_cleanup": False,
                "destroyed_current_checkout": False,
                "destroyed_before_worktree_removal": False,
                "self_host_guard": False,
                "summary": "Need a decision.",
            },
        }
        self.result.write_text(json.dumps(wrapped), encoding="utf-8")

        report = evaluate(
            "ambiguous-isolation",
            self.result,
            self.commands,
            {"use_wktbox": False, "ask_clarification": True},
        )

        self.assertTrue(report["passed"], report)

    def test_reports_provider_error_without_raising(self):
        self.result.write_text(
            json.dumps(
                {
                    "type": "result",
                    "is_error": True,
                    "api_error_status": 403,
                    "result": "provider access disabled",
                }
            ),
            encoding="utf-8",
        )

        report = evaluate(
            "explicit-e2e",
            self.result,
            self.commands,
            {"use_wktbox": True},
        )

        self.assertFalse(report["passed"])
        self.assertIn("agent_result_error", report["failures"])


if __name__ == "__main__":
    unittest.main()
