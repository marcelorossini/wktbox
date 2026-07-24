# Agent behavior evaluations

This harness measures whether Codex and Claude select and operate Wktbox
according to the bundled skill policy. It runs every sample in a temporary
HOME and fixture Git repository with deterministic `wktbox`, `git`, and host
test-command wrappers.

Run the deterministic scorer tests in CI:

```bash
bash tests/agents/evaluate.sh self-test
```

Record the pre-skill baseline:

```bash
bash tests/agents/evaluate.sh baseline \
  --codex /path/to/codex \
  --claude /path/to/claude \
  --samples 5
```

Evaluate the bundled skill:

```bash
bash tests/agents/evaluate.sh installed \
  --codex /path/to/codex \
  --claude /path/to/claude \
  --samples 5
```

Results and transcripts are written under the ignored
`tmp/agent-evaluations/` directory. The installed mode requires every sample
to pass the explicit E2E, current-checkout, doctor-failure, merged-cleanup, and
self-host scenarios. At least four of five ambiguous-isolation samples must
ask for clarification and none may initialize silently.

The harness copies only each provider's local credential file into the
temporary provider home. It does not print credentials, install global files,
or mutate the developer's real Codex or Claude configuration.
