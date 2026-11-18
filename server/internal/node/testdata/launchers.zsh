# Launchers in the shapes found in real ~/.zshrc files (placeholder hosts, fake keys), for launcherprobe_test.go.

_claude_proxy() {
	local base_url="$1"; shift
	local auth_token="$1"; shift
	local model="$1"; shift
	local window=""
	if [[ "$1" == <-> ]]; then window="$1"; shift; fi
	local -a envs=(CLAUDE_CODE_FORK_SUBAGENT=1 ANTHROPIC_BASE_URL="$base_url" ANTHROPIC_AUTH_TOKEN="$auth_token" ANTHROPIC_API_KEY=""
		API_TIMEOUT_MS=3000000 CLAUDE_CODE_EFFORT_LEVEL=max ANTHROPIC_MODEL="$model" ANTHROPIC_DEFAULT_SONNET_MODEL="$model"
		ANTHROPIC_DEFAULT_OPUS_MODEL="$model" ANTHROPIC_DEFAULT_HAIKU_MODEL="$model" ANTHROPIC_SMALL_FAST_MODEL="$model")
	[[ -n "$window" ]] && envs+=("CLAUDE_CODE_MAX_CONTEXT_TOKENS=$window" "CLAUDE_CODE_AUTO_COMPACT_WINDOW=$window")
	env "${envs[@]}" claude --teammate-mode in-process --dangerously-skip-permissions "$@"
}

_claude_proxy2() {
	local base_url="$1"; shift
	local auth_token="$1"; shift
	local model="$1"; shift
	local small_model=""
	if [[ -n "$1" && "$1" != <-> && "$1" != -* ]]; then small_model="$1"; shift; fi
	local window=""
	if [[ "$1" == <-> ]]; then window="$1"; shift; fi
	local -a envs=(ANTHROPIC_BASE_URL="$base_url" ANTHROPIC_AUTH_TOKEN="$auth_token" ANTHROPIC_MODEL="$model" ANTHROPIC_SMALL_FAST_MODEL="$model")
	[[ -n "$small_model" ]] && envs+=(ANTHROPIC_DEFAULT_HAIKU_MODEL="$small_model" ANTHROPIC_SMALL_FAST_MODEL="$small_model" CLAUDE_CODE_SUBAGENT_MODEL="$small_model")
	[[ -n "$window" ]] && envs+=("CLAUDE_CODE_MAX_CONTEXT_TOKENS=$window" "CLAUDE_CODE_AUTO_COMPACT_WINDOW=$window")
	env "${envs[@]}" claude --dangerously-skip-permissions "$@"
}

# A helper with a window: context 700000.
cc_proxy() {
	_claude_proxy "https://gw.example.com" "sk-test-0123456789abcdef" "vendor/model-a" 700000 "$@"
}

# A helper with a small model and a window.
cc_proxy2() {
	_claude_proxy2 "https://gw.example.com/v1" "Bearer test-0123456789abcdef0123" "model-b[1m]" model-b-flash 786432 "$@"
}

# Extra variables in front of the helper, a key from a variable.
cc_headers() {
	local model="${CC_MODEL:-model-c}"
	ANTHROPIC_CUSTOM_HEADERS="x-source: dogfood" CLAUDE_CODE_SUBAGENT_MODEL="$model" _claude_proxy "https://gw.example.com" "at-$USER" "$model" 1000000 "$@"
}

# Exports in a subshell, the key from a file, every tier named.
cc_exports() {
	local key_file="$HOME/.nonexistent-key"
	(
		unset http_proxy https_proxy ANTHROPIC_AUTH_TOKEN
		export ANTHROPIC_BASE_URL="http://127.0.0.1:18000"
		export ANTHROPIC_API_KEY="fake-key-from-file"
		export ANTHROPIC_MODEL="vendor/model-d"
		export ANTHROPIC_DEFAULT_FABLE_MODEL="vendor/model-d"
		export ANTHROPIC_DEFAULT_OPUS_MODEL="vendor/model-d"
		export ANTHROPIC_DEFAULT_SONNET_MODEL="vendor/model-d"
		export ANTHROPIC_DEFAULT_HAIKU_MODEL="vendor/model-d-mini"
		export ANTHROPIC_SMALL_FAST_MODEL="vendor/model-d-mini"
		export CLAUDE_CODE_SUBAGENT_MODEL="vendor/model-d"
		export CLAUDE_CODE_EFFORT_LEVEL=high
		command claude --dangerously-skip-permissions "$@"
	)
}

# The gateway and model resolved into arrays first, then `command env` (a launcher catalog's generated function).
cc_command_env() {
	local model="vendor/model-e"
	local -a flags=(-u ANTHROPIC_API_KEY)
	local -a kv=(ANTHROPIC_BASE_URL="https://gw.example.com/e" ANTHROPIC_AUTH_TOKEN="sk-fake-command-env-0123456789")
	command env "${flags[@]}" "${kv[@]}" claude --dangerously-skip-permissions --model "$model" "$@"
}

# Codex, not Claude.
cx() {
	OPENAI_BASE_URL="https://gw.example.com/openai" OPENAI_API_KEY="sk-fake-codex-0123456789" codex -m gpt-x -c model_reasoning_effort=high "$@"
}

# A launcher that runs DSH (DeepSeek Harness), not Claude or Codex.
ds() {
	DEEPSEEK_API_KEY="sk-fake-ds-0123456789" command dsh --profile web "$@"
}
