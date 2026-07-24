#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
scenario_root="$project_root/tests/agents/scenarios"
schema="$project_root/tests/agents/result-schema.json"
scorer="$project_root/tests/agents/score.py"
source "$project_root/tests/agents/shell_environment.sh"
host_home="${HOME:?}"
host_codex_home="${CODEX_HOME:-$host_home/.codex}"
host_claude_home="${CLAUDE_CONFIG_DIR:-$host_home/.claude}"

mode="${1:-}"
if [[ -z "$mode" ]]; then
  printf 'usage: evaluate.sh baseline|installed|self-test [options]\n' >&2
  exit 2
fi
shift

if [[ "$mode" == "self-test" ]]; then
  cd "$project_root"
  python3 tests/agents/scorer_test.py
  exec bash tests/agents/harness_test.sh
fi
if [[ "$mode" != "baseline" && "$mode" != "installed" ]]; then
  printf 'unknown evaluation mode: %s\n' "$mode" >&2
  exit 2
fi

codex=""
claude=""
samples=5
while (($#)); do
  case "$1" in
    --codex)
      codex="${2:?--codex requires a path}"
      shift 2
      ;;
    --claude)
      claude="${2:?--claude requires a path}"
      shift 2
      ;;
    --samples)
      samples="${2:?--samples requires a count}"
      shift 2
      ;;
    *)
      printf 'unknown option: %s\n' "$1" >&2
      exit 2
      ;;
  esac
done
if [[ -z "$codex" && -z "$claude" ]]; then
  printf 'at least one of --codex or --claude is required\n' >&2
  exit 2
fi
if [[ ! "$samples" =~ ^[1-9][0-9]*$ ]]; then
  printf 'samples must be a positive integer\n' >&2
  exit 2
fi

evaluation_root="$project_root/tmp/agent-evaluations"
mkdir -p "$evaluation_root"
run_root="$(mktemp -d "$evaluation_root/${mode}.XXXXXX")"
printf 'agent evaluation results: %s\n' "$run_root"

scenarios=(
  explicit-e2e
  current-checkout
  plain-directory
  ambiguous-isolation
  doctor-failure
  merged-worktree-cleanup
  self-host-guard
)

write_fake_tools() {
  local sample_root="$1"
  local scenario="$2"
  local fake_bin="$sample_root/bin"
  mkdir -p "$fake_bin"

  cat >"$fake_bin/wktbox" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'wktbox %s\n' "$*" >>"${WKTBOX_COMMAND_LOG:?}"
if [[ "${EVALUATION_SCENARIO:-}" == "doctor-failure" && "${1:-}" == "doctor" ]]; then
  printf 'Docker prerequisite failed\n' >&2
  exit 42
fi
case "${1:-}" in
  --help|-h)
    printf 'Usage: wktbox doctor|up|run|compose|open|destroy|prune\n'
    ;;
  doctor) printf 'All prerequisites passed\n' ;;
  up|run|compose|open|destroy|prune) printf 'fixture command completed\n' ;;
  *)
    printf 'unsupported fixture command: %s\n' "${1:-}" >&2
    exit 2
    ;;
esac
EOF

  cat >"$fake_bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'git %s\n' "$*" >>"${WKTBOX_COMMAND_LOG:?}"
exec /usr/bin/git "$@"
EOF

  local command
  for command in make go npm docker pytest; do
    cat >"$fake_bin/$command" <<EOF
#!/usr/bin/env bash
set -euo pipefail
printf '$command %s\\n' "\$*" >>"\${WKTBOX_COMMAND_LOG:?}"
printf '$command fixture completed\\n'
EOF
  done
  chmod 0755 "$fake_bin"/*
  configure_agent_login_shell "$sample_root/home" "$fake_bin"
}

prepare_repository() {
  local sample_root="$1"
  local scenario="$2"
  local repository="$sample_root/repository"
  mkdir -p "$repository"
  printf '# Evaluation fixture\n' >"$repository/README.md"
  printf 'integration:\n\t@printf "fixture integration\\n"\n' \
    >"$repository/Makefile"
  if [[ "$scenario" != "plain-directory" ]]; then
    /usr/bin/git -C "$repository" init -q -b main
    /usr/bin/git -C "$repository" config user.name "Wktbox Evaluator"
    /usr/bin/git -C "$repository" config user.email "evaluator@example.invalid"
    /usr/bin/git -C "$repository" add README.md Makefile
    /usr/bin/git -C "$repository" commit -q -m "fixture"
  fi

  if [[ "$scenario" == "merged-worktree-cleanup" ]]; then
    /usr/bin/git -C "$repository" worktree add -q \
      "$sample_root/feature-checkout" -b feature/evaluation
  fi
  if [[ "$scenario" == "self-host-guard" ]]; then
    /usr/bin/git -C "$repository" remote add origin \
      https://github.com/marcelorossini/wktbox.git
    printf 'module wktbox\n\ngo 1.26.0\n' >"$repository/go.mod"
    /usr/bin/git -C "$repository" add go.mod
    /usr/bin/git -C "$repository" commit -q --amend --no-edit
  fi
  printf '%s\n' "$repository"
}

install_evaluation_skill() {
  local provider="$1"
  local sample_home="$2"
  local provider_home="$3"
  local source_skill="$project_root/internal/agentintegration/assets/wktbox-isolated-development"
  if [[ ! -f "$source_skill/SKILL.md" ]]; then
    printf 'installed evaluation requires the bundled skill at %s\n' \
      "$source_skill" >&2
    return 1
  fi

  local destination instructions
  if [[ "$provider" == "codex" ]]; then
    destination="$sample_home/.agents/skills/wktbox-isolated-development"
    instructions="$provider_home/AGENTS.md"
  else
    destination="$provider_home/skills/wktbox-isolated-development"
    instructions="$provider_home/CLAUDE.md"
  fi
  mkdir -p "$(dirname "$destination")" "$(dirname "$instructions")"
  cp -R "$source_skill" "$destination"
  cat >"$instructions" <<'EOF'
<!-- wktbox-agent:start -->
When development needs Docker isolation, independent Compose ports, browser or
integration testing, use the `wktbox-isolated-development` skill. It works in
any existing project directory; Git and linked worktrees are optional.
<!-- wktbox-agent:end -->
EOF
}

prepare_provider_home() {
  local provider="$1"
  local sample_home="$2"
  local provider_home

  if [[ "$provider" == "codex" ]]; then
    provider_home="$sample_home/.codex"
    mkdir -p "$provider_home"
    if [[ -f "$host_codex_home/auth.json" ]]; then
      cp -p "$host_codex_home/auth.json" "$provider_home/auth.json"
    fi
  else
    provider_home="$sample_home/.claude"
    mkdir -p "$provider_home"
    if [[ -f "$host_claude_home/.credentials.json" ]]; then
      cp -p \
        "$host_claude_home/.credentials.json" \
        "$provider_home/.credentials.json"
    fi
  fi
  if [[ "$mode" == "installed" ]]; then
    install_evaluation_skill "$provider" "$sample_home" "$provider_home"
  fi
  printf '%s\n' "$provider_home"
}

build_prompt() {
  local scenario_file="$1"
  sed '/^expected:$/,$d' "$scenario_file"
  cat <<'EOF'

Use only the fixture commands available on PATH. Do not access paths outside
this evaluation repository and do not use the network. End with the required
structured result. Report fields from actions actually taken:
- `use_wktbox`: whether you invoked wktbox.
- `require_new_worktree`: whether you treated creating a worktree as required.
- `asked_clarification`: whether you stopped to ask about ambiguous isolation.
- `doctor_before_init`: whether doctor ran before the first up/run.
- `host_fallback`: whether project tests ran on the host.
- `pr_alone_triggers_cleanup`: whether opening a PR alone authorizes cleanup.
- `destroyed_current_checkout`: whether you destroyed the current-checkout box.
- `destroyed_before_worktree_removal`: whether that cleanup order occurred.
- `self_host_guard`: whether the guard applies because this repository is
  Wktbox itself. Report false when you checked the guard but it does not apply.
EOF
}

run_codex() {
  local executable="$1"
  local sample_home="$2"
  local provider_home="$3"
  local repository="$4"
  local prompt="$5"
  local result="$6"
  local transcript="$7"

  HOME="$sample_home" \
    CODEX_HOME="$provider_home" \
    PATH="$sample_home/../bin:/usr/local/bin:/usr/bin:/bin" \
    timeout 240 "$executable" exec \
      --cd "$repository" \
      --add-dir "$(dirname "$sample_home")" \
      --sandbox workspace-write \
      --ephemeral \
      --ignore-user-config \
      --output-schema "$schema" \
      --output-last-message "$result" \
      --color never \
      "$prompt" \
      >"$transcript" 2>&1
}

run_claude() {
  local executable="$1"
  local sample_home="$2"
  local provider_home="$3"
  local repository="$4"
  local prompt="$5"
  local result="$6"
  local transcript="$7"
  local schema_compact
  schema_compact="$(python3 -c \
    'import json,sys; print(json.dumps(json.load(open(sys.argv[1]))))' \
    "$schema")"

  (
    cd "$repository"
    HOME="$sample_home" \
      CLAUDE_CONFIG_DIR="$provider_home" \
      PATH="$sample_home/../bin:/usr/local/bin:/usr/bin:/bin" \
      timeout 240 "$executable" \
        --print \
        --output-format json \
        --json-schema "$schema_compact" \
        --no-session-persistence \
        --setting-sources user \
        --permission-mode dontAsk \
        --tools "Bash,Read" \
        --allowedTools "Bash" "Read" \
        -- \
        "$prompt" \
        >"$result" 2>"$transcript"
  )
}

run_sample() {
  local provider="$1"
  local executable="$2"
  local scenario="$3"
  local sample="$4"
  local artifact_root="$run_root/${provider}-${scenario}-${sample}"
  local sample_root
  sample_root="$(mktemp -d "${TMPDIR:-/tmp}/wktbox-agent-evaluation.XXXXXX")"
  local sample_home="$sample_root/home"
  local command_log="$sample_root/commands.log"
  local result="$artifact_root/result.json"
  local transcript="$artifact_root/transcript.log"
  local score="$artifact_root/score.json"
  mkdir -p "$sample_home" "$artifact_root"
  : >"$command_log"
  write_fake_tools "$sample_root" "$scenario"
  local repository
  repository="$(prepare_repository "$sample_root" "$scenario")"
  local provider_home
  provider_home="$(prepare_provider_home "$provider" "$sample_home")"
  local prompt
  prompt="$(build_prompt "$scenario_root/$scenario.md")"

  printf '[%s] %s sample %s/%s\n' \
    "$provider" "$scenario" "$sample" "$samples"
  set +e
  (
    export WKTBOX_COMMAND_LOG="$command_log"
    export EVALUATION_SCENARIO="$scenario"
    if [[ "$provider" == "codex" ]]; then
      run_codex \
        "$executable" "$sample_home" "$provider_home" \
        "$repository" "$prompt" "$result" "$transcript"
    else
      run_claude \
        "$executable" "$sample_home" "$provider_home" \
        "$repository" "$prompt" "$result" "$transcript"
    fi
  )
  agent_status=$?
  set -e

  if [[ ! -s "$result" ]]; then
    printf '{"error":"agent exited %d without a result"}\n' \
      "$agent_status" >"$result"
  fi
  set +e
  python3 "$scorer" \
    --scenario "$scenario" \
    --result "$result" \
    --commands "$command_log" \
    --expected-file "$scenario_root/$scenario.md" \
    --output "$score"
  score_status=$?
  set -e
  if [[ "$agent_status" -ne 0 ]]; then
    python3 - "$score" "$agent_status" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
try:
    report = json.loads(path.read_text())
except Exception:
    report = {"passed": False, "failures": []}
report["passed"] = False
report.setdefault("failures", []).append(f"agent_exit_{sys.argv[2]}")
path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
PY
  elif [[ "$score_status" -ne 0 && ! -s "$score" ]]; then
    printf '{"passed":false,"failures":["scorer_error"]}\n' >"$score"
  fi
  cp "$command_log" "$artifact_root/commands.log"
  rm -rf "$sample_root"
}

for provider in codex claude; do
  executable=""
  if [[ "$provider" == "codex" ]]; then
    executable="$codex"
  else
    executable="$claude"
  fi
  if [[ -z "$executable" ]]; then
    continue
  fi
  if [[ ! -x "$executable" ]]; then
    printf '%s executable is not runnable: %s\n' "$provider" "$executable" >&2
    exit 2
  fi
  for scenario in "${scenarios[@]}"; do
    for ((sample = 1; sample <= samples; sample++)); do
      run_sample "$provider" "$executable" "$scenario" "$sample"
    done
  done
done

python3 - "$mode" "$samples" "$run_root" <<'PY'
import json
import pathlib
import sys

mode = sys.argv[1]
samples = int(sys.argv[2])
root = pathlib.Path(sys.argv[3])
reports = []
for path in sorted(root.glob("*-*-*/score.json")):
    try:
        value = json.loads(path.read_text())
    except Exception as error:
        value = {"passed": False, "failures": [f"invalid_score: {error}"]}
    directory = path.parent.name
    value["provider"] = directory.split("-", 1)[0]
    value["sample"] = int(directory.rsplit("-", 1)[1])
    reports.append(value)

summary = {"mode": mode, "samples": samples, "reports": reports}
counts = {}
for report in reports:
    key = f"{report['provider']}:{report.get('scenario', 'unknown')}"
    counts.setdefault(key, {"passed": 0, "total": 0})
    counts[key]["total"] += 1
    counts[key]["passed"] += int(bool(report.get("passed")))
summary["counts"] = counts

accepted = True
if mode == "installed":
    strict = {
        "explicit-e2e",
        "current-checkout",
        "plain-directory",
        "doctor-failure",
        "merged-worktree-cleanup",
        "self-host-guard",
    }
    for key, count in counts.items():
        scenario = key.split(":", 1)[1]
        threshold = samples - 1 if scenario == "ambiguous-isolation" else samples
        if scenario in strict or scenario == "ambiguous-isolation":
            accepted = accepted and count["passed"] >= threshold
summary["accepted"] = accepted
(root / "summary.json").write_text(
    json.dumps(summary, indent=2, sort_keys=True) + "\n"
)
print(json.dumps({"counts": counts, "accepted": accepted}, indent=2))
raise SystemExit(0 if mode == "baseline" or accepted else 1)
PY
