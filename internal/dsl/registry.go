package dsl

// This file is the single-source-of-truth field/metric catalog for the
// DSL, per markup.md "fieldカタログ": DSL validation, the Web UI catalog
// endpoints (GET /api/dsl/fields, GET /api/dsl/metrics), and the renderer's
// field-metadata lookups all resolve field/metric metadata through
// FieldByName/Fields/MetricByName/Metrics instead of duplicating it. The
// legacy hand-maintained catalogs (the old internal/config/metrics.go and
// internal/webconfig/catalog.go widget tables) have been removed.

// Descriptions holds the localized one-line descriptions of a field or
// metric. Display names stay English; only descriptions are localized
// (markup.md "i18n").
type Descriptions struct {
	EN string
	JA string
}

// FieldDef describes one content field in a tool's catalog.
type FieldDef struct {
	// Name is the field's `name` attribute value on a <field> node, e.g.
	// "model" or "context-percentage-usable".
	Name string
	// SelfMetric is the metric name this field exposes as "self" in a
	// when/color-rule expression on the same node. Empty means the field
	// has no self metric (self is then a validation error).
	SelfMetric string
	// Linkable marks fields that support the `hyperlink` attribute.
	Linkable bool
	// Formats lists the formatter names applicable to this field via
	// `format="..."`. Empty means the field accepts no format attribute
	// at all (formatter validation rejects any format="..." for it).
	Formats []string
	// DisplayName is the English UI label for the field (e.g. "Context
	// Length"). It is the single source of truth for the configurator's
	// field picker, replacing the hand-maintained webconfig catalog.
	DisplayName string
	// Descriptions is the localized one-line description shown in the UI.
	Descriptions Descriptions
	// Category groups fields in the UI: "common" (tool-agnostic) or
	// "claude" (claude-code specific).
	Category string
	// Capability, when non-empty, names a runtime capability the field
	// depends on (e.g. "oauth-usage" fields need the authenticated usage
	// API to be reachable). The configurator hides such fields from the
	// palette until a capability probe confirms availability. Empty means
	// always available.
	Capability string
}

// MetricDef describes one named metric usable in `when`/`color-rule`
// expressions (via a bare kebab-case reference, not "self").
type MetricDef struct {
	Name string
	// DisplayName is the English UI label for the metric (e.g. "Context
	// Used (%)"), used by the configurator's condition editor.
	DisplayName string
	// Descriptions is the localized one-line description shown in the UI.
	Descriptions Descriptions
}

// claudeCodeFields is the tool="claude-code" content field catalog
// (markup.md "コンテンツfield", 25 entries - the spec text says "26種"
// counting the historical widget-type list, but separator/flex-separator
// are explicitly excluded here since they become the <text role="separator">
// and <flex/> nodes instead of fields).
//
// Formats assignment rationale (also see markup.md "formatter" for the
// formatter name catalog):
//   - Plain/free-form strings (model, git-branch, tool-version,
//     current-directory, session-name, agent-name, repo-name, worktree):
//     no formatter makes sense, Formats is nil.
//   - git-changes is a composite "+N -M" string, not a single scalar a
//     formatter could act on: Formats is nil.
//   - thinking-effort / vim-mode / pr-review-state are closed string
//     enumerations: the "enum" formatter (markup.md formatter candidates)
//     applies to these.
//   - pr-number is an identifier, not a formatted quantity (no thousands
//     separator etc. makes sense for it): Formats is nil; it is linkable
//     instead.
//   - context-length is a token count: both "number" (with thousands
//     separators) and "compact-number" (64.0k form) are meaningful.
//   - context-percentage / context-percentage-usable / five-hour-usage /
//     weekly-usage / cache-hit-rate are ratios: "percent".
//   - session-cost is a USD amount: "currency".
//   - session-duration / api-duration are elapsed time: "duration".
//   - five-hour-reset / weekly-reset are time-until: "countdown".
//   - lines-changed is a single integer total: "number".
var claudeCodeFields = []FieldDef{
	{
		Name: "model", Category: "common", DisplayName: "Model",
		Descriptions: Descriptions{EN: "The active model's display name.", JA: "現在使用中のモデルの表示名です。"},
	},
	{Name: "model-id", Category: "claude", DisplayName: "Model ID", Descriptions: Descriptions{EN: "The active model's identifier.", JA: "現在使用中のモデルIDです。"}},
	{Name: "output-style", Category: "claude", DisplayName: "Output Style", Formats: []string{"enum"}, Descriptions: Descriptions{EN: "The active Claude Code output style.", JA: "現在のClaude Code出力スタイルです。"}},
	{Name: "session-id", Category: "claude", DisplayName: "Session ID", Descriptions: Descriptions{EN: "The current session identifier.", JA: "現在のセッションIDです。"}},
	{Name: "thinking-enabled", Category: "claude", DisplayName: "Thinking Enabled", SelfMetric: "thinking-enabled", Formats: []string{"enum"}, Descriptions: Descriptions{EN: "Whether extended thinking is enabled.", JA: "拡張思考が有効かどうかです。"}},
	{Name: "project-directory", Category: "common", DisplayName: "Project Directory", Descriptions: Descriptions{EN: "The absolute project directory path.", JA: "プロジェクトディレクトリの絶対パスです。"}},
	{Name: "git-root", Category: "common", DisplayName: "Git Root", Descriptions: Descriptions{EN: "The git repository root path.", JA: "gitリポジトリのルートパスです。"}},
	{
		Name: "git-branch", Category: "common", DisplayName: "Git Branch",
		Descriptions: Descriptions{EN: "The current git branch name.", JA: "現在のgitブランチ名です。"},
	},
	{
		Name: "git-changes", Category: "common", DisplayName: "Git Changes",
		Descriptions: Descriptions{EN: "Added/deleted line counts from git status.", JA: "gitでの追加・削除行数を表示します。"},
	},
	{
		Name: "tool-version", Category: "common", DisplayName: "Tool Version",
		Descriptions: Descriptions{EN: "The coding agent's version string.", JA: "コーディングエージェントのバージョンです。"},
	},
	{
		Name: "current-directory", Category: "common", DisplayName: "Current Directory",
		Descriptions: Descriptions{EN: "The base name of the current working directory.", JA: "現在の作業ディレクトリ名（最後の要素のみ）です。"},
	},
	{
		Name: "thinking-effort", Category: "claude", DisplayName: "Thinking Effort", Formats: []string{"enum"},
		Descriptions: Descriptions{EN: "The active reasoning effort level (low/medium/high/xhigh/max).", JA: "現在の思考努力レベル（low/medium/high/xhigh/max）です。"},
	},
	{
		Name: "context-length", Category: "claude", DisplayName: "Context Length", SelfMetric: "context-tokens", Formats: []string{"number", "compact-number"},
		Descriptions: Descriptions{EN: "Total input tokens used in the context window.", JA: "コンテキストウィンドウで使用中の入力トークン総数です。"},
	},
	{Name: "context-window-size", Category: "claude", DisplayName: "Context Window Size", SelfMetric: "context-window-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "The model's context window size in tokens.", JA: "モデルのコンテキストウィンドウサイズ（トークン数）です。"}},
	{Name: "context-remaining", Category: "claude", DisplayName: "Context Remaining", SelfMetric: "context-remaining-percent", Formats: []string{"percent"}, Descriptions: Descriptions{EN: "Percentage of the context window remaining.", JA: "コンテキストウィンドウの残り割合です。"}},
	{Name: "context-output-tokens", Category: "claude", DisplayName: "Context Output Tokens", SelfMetric: "context-output-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Total output tokens in the session context.", JA: "セッションコンテキストの出力トークン総数です。"}},
	{Name: "current-input-tokens", Category: "claude", DisplayName: "Current Input Tokens", SelfMetric: "current-input-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Input tokens in the latest API call.", JA: "直近API呼び出しの入力トークン数です。"}},
	{Name: "current-output-tokens", Category: "claude", DisplayName: "Current Output Tokens", SelfMetric: "current-output-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Output tokens in the latest API call.", JA: "直近API呼び出しの出力トークン数です。"}},
	{Name: "cache-creation-tokens", Category: "claude", DisplayName: "Cache Creation Tokens", SelfMetric: "cache-creation-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Cache creation input tokens in the latest API call.", JA: "直近API呼び出しのキャッシュ作成入力トークン数です。"}},
	{Name: "cache-read-tokens", Category: "claude", DisplayName: "Cache Read Tokens", SelfMetric: "cache-read-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Cache read input tokens in the latest API call.", JA: "直近API呼び出しのキャッシュ読み取り入力トークン数です。"}},
	{
		Name: "context-percentage", Category: "claude", DisplayName: "Context Percentage", SelfMetric: "context-percent", Formats: []string{"percent"},
		Descriptions: Descriptions{EN: "Raw percentage of the context window used.", JA: "コンテキストウィンドウ全体に対する使用率です。"},
	},
	{
		Name: "context-percentage-usable", Category: "claude", DisplayName: "Context Percentage (Usable)", SelfMetric: "context-usable-percent", Formats: []string{"percent"},
		Descriptions: Descriptions{EN: "Percentage of the usable context window used, after reserving auto-compact headroom.", JA: "自動コンパクト用の予約分を除いた、実際に使用可能なコンテキストに対する使用率です。"},
	},
	{
		Name: "session-cost", Category: "claude", DisplayName: "Session Cost", SelfMetric: "session-cost-usd", Formats: []string{"currency"},
		Descriptions: Descriptions{EN: "Estimated session cost in US dollars.", JA: "セッションの推定コスト（米ドル）です。"},
	},
	{
		Name: "five-hour-usage", Category: "claude", DisplayName: "5-Hour Usage", SelfMetric: "five-hour-percent", Formats: []string{"percent"},
		Descriptions: Descriptions{EN: "Percentage of the rolling 5-hour rate limit window used.", JA: "5時間のレート制限ウィンドウの使用率です。"},
	},
	{
		Name: "five-hour-reset", Category: "claude", DisplayName: "5-Hour Reset", SelfMetric: "five-hour-reset-minutes", Formats: []string{"countdown"},
		Descriptions: Descriptions{EN: "Countdown until the 5-hour rate limit window resets.", JA: "5時間のレート制限がリセットされるまでの残り時間です。"},
	},
	{
		Name: "weekly-usage", Category: "claude", DisplayName: "Weekly Usage", SelfMetric: "seven-day-percent", Formats: []string{"percent"},
		Descriptions: Descriptions{EN: "Percentage of the rolling 7-day rate limit window used.", JA: "7日間のレート制限ウィンドウの使用率です。"},
	},
	{
		Name: "weekly-reset", Category: "claude", DisplayName: "Weekly Reset", SelfMetric: "seven-day-reset-minutes", Formats: []string{"countdown"},
		Descriptions: Descriptions{EN: "Countdown until the 7-day rate limit window resets.", JA: "7日間のレート制限がリセットされるまでの残り時間です。"},
	},
	{
		Name: "session-name", Category: "claude", DisplayName: "Session Name",
		Descriptions: Descriptions{EN: "The custom session name set via --name or /rename.", JA: "--name や /rename で設定したセッション名です。"},
	},
	{
		Name: "agent-name", Category: "claude", DisplayName: "Agent Name",
		Descriptions: Descriptions{EN: "The name of the running agent (when started with --agent).", JA: "実行中のエージェント名（--agent 指定時）です。"},
	},
	{
		Name: "vim-mode", Category: "claude", DisplayName: "Vim Mode", Formats: []string{"enum"},
		Descriptions: Descriptions{EN: "The current vim editing mode (NORMAL/INSERT/VISUAL).", JA: "現在のvim編集モード（NORMAL/INSERT/VISUAL）です。"},
	},
	{
		Name: "pr-number", Category: "common", DisplayName: "PR Number", Linkable: true,
		Descriptions: Descriptions{EN: "The open pull request number for the current branch.", JA: "現在のブランチに紐づくオープン中のプルリクエスト番号です。"},
	},
	{
		Name: "pr-review-state", Category: "common", DisplayName: "PR Review State", Linkable: true, Formats: []string{"enum"},
		Descriptions: Descriptions{EN: "The review state of the current branch's pull request (approved/pending/changes_requested/draft).", JA: "現在のブランチのプルリクエストのレビュー状態（approved/pending/changes_requested/draft）です。"},
	},
	{
		Name: "repo-name", Category: "common", DisplayName: "Repository", Linkable: true,
		Descriptions: Descriptions{EN: "The repository identity (owner/name) from the origin remote.", JA: "originリモートから得たリポジトリ名（owner/name）です。"},
	},
	{
		Name: "worktree", Category: "common", DisplayName: "Worktree",
		Descriptions: Descriptions{EN: "The name of the linked git worktree (empty on the main working tree).", JA: "リンクされたgit worktree名です（メインの作業ツリーでは空）。"},
	},
	{
		Name: "session-duration", Category: "claude", DisplayName: "Session Duration", SelfMetric: "session-duration-minutes", Formats: []string{"duration"},
		Descriptions: Descriptions{EN: "Wall-clock time elapsed since the session started.", JA: "セッション開始からの経過時間です。"},
	},
	{
		Name: "api-duration", Category: "claude", DisplayName: "API Duration", SelfMetric: "api-duration-minutes", Formats: []string{"duration"},
		Descriptions: Descriptions{EN: "Time spent waiting for API responses this session.", JA: "今セッションでAPI応答待ちに費やした時間です。"},
	},
	{
		Name: "lines-changed", Category: "claude", DisplayName: "Lines Changed", SelfMetric: "lines-changed-total", Formats: []string{"number"},
		Descriptions: Descriptions{EN: "Lines of code added and removed this session.", JA: "今セッションで追加・削除されたコード行数です。"},
	},
	{Name: "lines-added", Category: "claude", DisplayName: "Lines Added", SelfMetric: "lines-added", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Lines added during this session.", JA: "今セッションで追加された行数です。"}},
	{Name: "lines-removed", Category: "claude", DisplayName: "Lines Removed", SelfMetric: "lines-removed", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Lines removed during this session.", JA: "今セッションで削除された行数です。"}},
	{Name: "git-staged", Category: "common", DisplayName: "Git Staged", SelfMetric: "git-staged", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Number of staged files.", JA: "ステージ済みファイル数です。"}},
	{Name: "git-unstaged", Category: "common", DisplayName: "Git Unstaged", SelfMetric: "git-unstaged", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Number of unstaged files.", JA: "未ステージファイル数です。"}},
	{Name: "git-untracked", Category: "common", DisplayName: "Git Untracked", SelfMetric: "git-untracked", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Number of untracked files.", JA: "未追跡ファイル数です。"}},
	{Name: "git-ahead", Category: "common", DisplayName: "Git Ahead", SelfMetric: "git-ahead", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Commits ahead of the upstream branch.", JA: "upstreamブランチより先行しているコミット数です。"}},
	{Name: "git-behind", Category: "common", DisplayName: "Git Behind", SelfMetric: "git-behind", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Commits behind the upstream branch.", JA: "upstreamブランチより遅れているコミット数です。"}},
	{Name: "git-clean", Category: "common", DisplayName: "Git Clean", SelfMetric: "git-clean", Formats: []string{"enum"}, Descriptions: Descriptions{EN: "Whether the working tree is clean.", JA: "作業ツリーがクリーンかどうかです。"}},
	{Name: "exceeds-200k", Category: "claude", DisplayName: "Exceeds 200K", SelfMetric: "exceeds-200k", Formats: []string{"enum"}, Descriptions: Descriptions{EN: "Whether context usage exceeds 200,000 tokens.", JA: "コンテキスト使用量が20万トークンを超えているかどうかです。"}},
	{
		Name: "cache-hit-rate", Category: "claude", DisplayName: "Cache Hit Rate", SelfMetric: "cache-hit-percent", Formats: []string{"percent"},
		Descriptions: Descriptions{EN: "Cache-read hit rate of the last API call.", JA: "直近API呼び出しのキャッシュ読み取りヒット率です。"},
	},
	{Name: "compaction-count", Category: "claude", DisplayName: "Compactions", SelfMetric: "compaction-count", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Number of transcript compactions.", JA: "transcriptのcompact回数です。"}},
	{Name: "compaction-auto", Category: "claude", DisplayName: "Automatic Compactions", SelfMetric: "compaction-auto", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Number of automatic compactions.", JA: "自動compact回数です。"}},
	{Name: "compaction-manual", Category: "claude", DisplayName: "Manual Compactions", SelfMetric: "compaction-manual", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Number of manual compactions.", JA: "手動compact回数です。"}},
	{Name: "compaction-unknown", Category: "claude", DisplayName: "Unknown Compactions", SelfMetric: "compaction-unknown", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Compactions with an unknown trigger.", JA: "起点不明のcompact回数です。"}},
	{Name: "compaction-tokens-reclaimed", Category: "claude", DisplayName: "Reclaimed Tokens", SelfMetric: "compaction-tokens-reclaimed", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Tokens reclaimed by compaction.", JA: "compactで回収されたトークン数です。"}},
	{Name: "session-input-tokens", Category: "claude", DisplayName: "Session Input Tokens", SelfMetric: "session-input-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Transcript-derived session input tokens.", JA: "transcript由来のセッション入力トークン数です。"}},
	{Name: "session-output-tokens", Category: "claude", DisplayName: "Session Output Tokens", SelfMetric: "session-output-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Transcript-derived session output tokens.", JA: "transcript由来のセッション出力トークン数です。"}},
	{Name: "session-cache-creation-tokens", Category: "claude", DisplayName: "Session Cache Creation Tokens", SelfMetric: "session-cache-creation-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Session cache-creation tokens.", JA: "セッションのキャッシュ作成トークン数です。"}},
	{Name: "session-cache-read-tokens", Category: "claude", DisplayName: "Session Cache Read Tokens", SelfMetric: "session-cache-read-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Session cache-read tokens.", JA: "セッションのキャッシュ読取トークン数です。"}},
	{Name: "session-total-tokens", Category: "claude", DisplayName: "Session Total Tokens", SelfMetric: "session-total-tokens", Formats: []string{"number", "compact-number"}, Descriptions: Descriptions{EN: "Total transcript-derived session tokens.", JA: "transcript由来のセッション総トークン数です。"}},
	{Name: "input-token-speed", Category: "claude", DisplayName: "Input Token Speed", SelfMetric: "input-token-speed", Formats: []string{"number"}, Descriptions: Descriptions{EN: "Average session input tokens per second.", JA: "セッション平均入力トークン毎秒です。"}},
	{Name: "output-token-speed", Category: "claude", DisplayName: "Output Token Speed", SelfMetric: "output-token-speed", Formats: []string{"number"}, Descriptions: Descriptions{EN: "Average session output tokens per second.", JA: "セッション平均出力トークン毎秒です。"}},
	{Name: "total-token-speed", Category: "claude", DisplayName: "Total Token Speed", SelfMetric: "total-token-speed", Formats: []string{"number"}, Descriptions: Descriptions{EN: "Average total tokens per second.", JA: "セッション平均総トークン毎秒です。"}},
	{
		Name: "extra-usage-cost", Category: "claude", Capability: "oauth-usage", SelfMetric: "extra-usage-cost-usd", Formats: []string{"currency"},
		DisplayName:  "Extra Usage Cost",
		Descriptions: Descriptions{EN: "Metered pay-as-you-go usage cost this month (US dollars), charged after you exceed your subscription limits.", JA: "サブスクリプションの上限を超えた後に発生する、今月の従量課金利用額（米ドル）です。"},
	},
	{
		Name: "extra-usage-limit", Category: "claude", Capability: "oauth-usage", SelfMetric: "extra-usage-limit-usd", Formats: []string{"currency"},
		DisplayName:  "Extra Usage Limit",
		Descriptions: Descriptions{EN: "The configured monthly spending cap for extra (pay-as-you-go) usage, in US dollars.", JA: "従量課金（追加利用）に設定された月間上限額（米ドル）です。"},
	},
	{
		Name: "extra-usage-percent", Category: "claude", Capability: "oauth-usage", SelfMetric: "extra-usage-percent", Formats: []string{"percent"},
		DisplayName:  "Extra Usage %",
		Descriptions: Descriptions{EN: "Percentage of the extra-usage monthly limit consumed.", JA: "従量課金の月間上限に対する使用率です。"},
	},
	{
		Name: "weekly-usage-opus", Category: "claude", Capability: "oauth-usage", SelfMetric: "seven-day-opus-percent", Formats: []string{"percent"},
		DisplayName:  "Weekly Usage (Opus)",
		Descriptions: Descriptions{EN: "Percentage of the rolling 7-day rate limit consumed by Opus models.", JA: "Opus系モデルの7日間レート制限の使用率です。"},
	},
	{
		Name: "weekly-usage-sonnet", Category: "claude", Capability: "oauth-usage", SelfMetric: "seven-day-sonnet-percent", Formats: []string{"percent"},
		DisplayName:  "Weekly Usage (Sonnet)",
		Descriptions: Descriptions{EN: "Percentage of the rolling 7-day rate limit consumed by Sonnet models.", JA: "Sonnet系モデルの7日間レート制限の使用率です。"},
	},
	{
		Name: "weekly-reset-opus", Category: "claude", Capability: "oauth-usage", SelfMetric: "seven-day-opus-reset-minutes", Formats: []string{"countdown"},
		DisplayName:  "Weekly Reset (Opus)",
		Descriptions: Descriptions{EN: "Countdown until the Opus 7-day rate limit window resets.", JA: "Opusの7日間レート制限がリセットされるまでの残り時間です。"},
	},
	{
		Name: "weekly-reset-sonnet", Category: "claude", Capability: "oauth-usage", SelfMetric: "seven-day-sonnet-reset-minutes", Formats: []string{"countdown"},
		DisplayName:  "Weekly Reset (Sonnet)",
		Descriptions: Descriptions{EN: "Countdown until the Sonnet 7-day rate limit window resets.", JA: "Sonnetの7日間レート制限がリセットされるまでの残り時間です。"},
	},
}

// claudeCodeMetrics is the tool="claude-code" named-metric catalog
// (markup.md "メトリクス名" and 付録), including the "git-dirty" boolean
// and the compaction/session-token/token-speed metrics derived from
// transcript analysis.
var claudeCodeMetrics = []MetricDef{
	{Name: "compaction-count", DisplayName: "Compactions", Descriptions: Descriptions{EN: "Number of transcript compactions.", JA: "transcriptのcompact回数です。"}},
	{Name: "compaction-auto", DisplayName: "Automatic Compactions", Descriptions: Descriptions{EN: "Number of automatic compactions.", JA: "自動compact回数です。"}},
	{Name: "compaction-manual", DisplayName: "Manual Compactions", Descriptions: Descriptions{EN: "Number of manual compactions.", JA: "手動compact回数です。"}},
	{Name: "compaction-unknown", DisplayName: "Unknown Compactions", Descriptions: Descriptions{EN: "Compactions with an unknown trigger.", JA: "起点不明のcompact回数です。"}},
	{Name: "compaction-tokens-reclaimed", DisplayName: "Reclaimed Tokens", Descriptions: Descriptions{EN: "Tokens reclaimed by compaction.", JA: "compactで回収されたトークン数です。"}},
	{Name: "session-input-tokens", DisplayName: "Session Input Tokens", Descriptions: Descriptions{EN: "Transcript-derived session input tokens.", JA: "transcript由来のセッション入力トークン数です。"}},
	{Name: "session-output-tokens", DisplayName: "Session Output Tokens", Descriptions: Descriptions{EN: "Transcript-derived session output tokens.", JA: "transcript由来のセッション出力トークン数です。"}},
	{Name: "session-cache-creation-tokens", DisplayName: "Session Cache Creation Tokens", Descriptions: Descriptions{EN: "Session cache-creation tokens.", JA: "セッションのキャッシュ作成トークン数です。"}},
	{Name: "session-cache-read-tokens", DisplayName: "Session Cache Read Tokens", Descriptions: Descriptions{EN: "Session cache-read tokens.", JA: "セッションのキャッシュ読取トークン数です。"}},
	{Name: "session-total-tokens", DisplayName: "Session Total Tokens", Descriptions: Descriptions{EN: "Total transcript-derived session tokens.", JA: "transcript由来のセッション総トークン数です。"}},
	{Name: "input-token-speed", DisplayName: "Input Token Speed", Descriptions: Descriptions{EN: "Average session input tokens per second.", JA: "セッション平均入力トークン毎秒です。"}},
	{Name: "output-token-speed", DisplayName: "Output Token Speed", Descriptions: Descriptions{EN: "Average session output tokens per second.", JA: "セッション平均出力トークン毎秒です。"}},
	{Name: "total-token-speed", DisplayName: "Total Token Speed", Descriptions: Descriptions{EN: "Average total tokens per second.", JA: "セッション平均総トークン毎秒です。"}},
	{Name: "thinking-enabled", DisplayName: "Thinking Enabled", Descriptions: Descriptions{EN: "Whether extended thinking is enabled.", JA: "拡張思考が有効かどうかです。"}},
	{Name: "exceeds-200k", DisplayName: "Exceeds 200K", Descriptions: Descriptions{EN: "Whether context usage exceeds 200,000 tokens.", JA: "コンテキスト使用量が20万トークンを超えているかどうかです。"}},
	{Name: "git-clean", DisplayName: "Git Clean", Descriptions: Descriptions{EN: "Whether the working tree is clean.", JA: "作業ツリーがクリーンかどうかです。"}},
	{Name: "context-window-tokens", DisplayName: "Context Window Tokens", Descriptions: Descriptions{EN: "The context window size in tokens.", JA: "コンテキストウィンドウのトークン数です。"}},
	{Name: "context-remaining-percent", DisplayName: "Context Remaining (%)", Descriptions: Descriptions{EN: "Percentage of context remaining.", JA: "コンテキストの残り割合です。"}},
	{Name: "context-output-tokens", DisplayName: "Context Output Tokens", Descriptions: Descriptions{EN: "Total output tokens in context.", JA: "コンテキストの出力トークン総数です。"}},
	{Name: "current-input-tokens", DisplayName: "Current Input Tokens", Descriptions: Descriptions{EN: "Input tokens in the latest API call.", JA: "直近API呼び出しの入力トークン数です。"}},
	{Name: "current-output-tokens", DisplayName: "Current Output Tokens", Descriptions: Descriptions{EN: "Output tokens in the latest API call.", JA: "直近API呼び出しの出力トークン数です。"}},
	{Name: "cache-creation-tokens", DisplayName: "Cache Creation Tokens", Descriptions: Descriptions{EN: "Cache creation tokens in the latest API call.", JA: "直近API呼び出しのキャッシュ作成トークン数です。"}},
	{Name: "cache-read-tokens", DisplayName: "Cache Read Tokens", Descriptions: Descriptions{EN: "Cache read tokens in the latest API call.", JA: "直近API呼び出しのキャッシュ読み取りトークン数です。"}},
	{Name: "git-staged", DisplayName: "Git Staged", Descriptions: Descriptions{EN: "Number of staged files.", JA: "ステージ済みファイル数です。"}},
	{Name: "git-unstaged", DisplayName: "Git Unstaged", Descriptions: Descriptions{EN: "Number of unstaged files.", JA: "未ステージファイル数です。"}},
	{Name: "git-untracked", DisplayName: "Git Untracked", Descriptions: Descriptions{EN: "Number of untracked files.", JA: "未追跡ファイル数です。"}},
	{Name: "git-ahead", DisplayName: "Git Ahead", Descriptions: Descriptions{EN: "Commits ahead of upstream.", JA: "upstreamより先行しているコミット数です。"}},
	{Name: "git-behind", DisplayName: "Git Behind", Descriptions: Descriptions{EN: "Commits behind upstream.", JA: "upstreamより遅れているコミット数です。"}},
	{
		Name: "api-duration-minutes", DisplayName: "API Duration (min)",
		Descriptions: Descriptions{EN: "Minutes spent waiting for API responses this session.", JA: "今セッションでAPI応答待ちに費やした分数です。"},
	},
	{
		Name: "cache-hit-percent", DisplayName: "Cache Hit Rate (%)",
		Descriptions: Descriptions{EN: "Cache-read hit rate of the last API call, as a percentage.", JA: "直近API呼び出しのキャッシュ読み取りヒット率（％）です。"},
	},
	{
		Name: "context-percent", DisplayName: "Context Used (%)",
		Descriptions: Descriptions{EN: "Raw percentage of the context window used.", JA: "コンテキストウィンドウ全体に対する使用率（％）です。"},
	},
	{
		Name: "context-tokens", DisplayName: "Context Tokens",
		Descriptions: Descriptions{EN: "Total input tokens used in the context window.", JA: "コンテキストウィンドウで使用中の入力トークン総数です。"},
	},
	{
		Name: "context-usable-percent", DisplayName: "Context Used, Usable (%)",
		Descriptions: Descriptions{EN: "Percentage of the usable context window used, after reserving auto-compact headroom.", JA: "自動コンパクト用の予約分を除いた使用可能コンテキストに対する使用率（％）です。"},
	},
	{
		Name: "five-hour-percent", DisplayName: "5-Hour Usage (%)",
		Descriptions: Descriptions{EN: "Percentage of the rolling 5-hour rate limit window used.", JA: "5時間のレート制限ウィンドウの使用率（％）です。"},
	},
	{
		Name: "five-hour-reset-minutes", DisplayName: "5-Hour Reset (min)",
		Descriptions: Descriptions{EN: "Minutes until the 5-hour rate limit window resets.", JA: "5時間のレート制限がリセットされるまでの分数です。"},
	},
	{
		Name: "git-dirty", DisplayName: "Git Dirty",
		Descriptions: Descriptions{EN: "Whether the working tree has uncommitted changes.", JA: "作業ツリーに未コミットの変更があるかどうかです。"},
	},
	{
		Name: "lines-added", DisplayName: "Lines Added",
		Descriptions: Descriptions{EN: "Lines of code added this session.", JA: "今セッションで追加されたコード行数です。"},
	},
	{
		Name: "lines-changed-total", DisplayName: "Lines Changed (total)",
		Descriptions: Descriptions{EN: "Total lines of code added and removed this session.", JA: "今セッションで追加・削除されたコード行数の合計です。"},
	},
	{
		Name: "lines-removed", DisplayName: "Lines Removed",
		Descriptions: Descriptions{EN: "Lines of code removed this session.", JA: "今セッションで削除されたコード行数です。"},
	},
	{
		Name: "session-cost-usd", DisplayName: "Session Cost (USD)",
		Descriptions: Descriptions{EN: "Estimated session cost in US dollars.", JA: "セッションの推定コスト（米ドル）です。"},
	},
	{
		Name: "session-duration-minutes", DisplayName: "Session Duration (min)",
		Descriptions: Descriptions{EN: "Minutes elapsed since the session started.", JA: "セッション開始からの経過分数です。"},
	},
	{
		Name: "seven-day-percent", DisplayName: "Weekly Usage (%)",
		Descriptions: Descriptions{EN: "Percentage of the rolling 7-day rate limit window used.", JA: "7日間のレート制限ウィンドウの使用率（％）です。"},
	},
	{
		Name: "seven-day-reset-minutes", DisplayName: "Weekly Reset (min)",
		Descriptions: Descriptions{EN: "Minutes until the 7-day rate limit window resets.", JA: "7日間のレート制限がリセットされるまでの分数です。"},
	},
	{
		Name: "extra-usage-cost-usd", DisplayName: "Extra Usage Cost (USD)",
		Descriptions: Descriptions{EN: "Metered pay-as-you-go usage cost this month (US dollars), charged after you exceed your subscription limits.", JA: "サブスクリプションの上限を超えた後に発生する、今月の従量課金利用額（米ドル）です。"},
	},
	{
		Name: "extra-usage-limit-usd", DisplayName: "Extra Usage Limit (USD)",
		Descriptions: Descriptions{EN: "The configured monthly spending cap for extra (pay-as-you-go) usage, in US dollars.", JA: "従量課金（追加利用）に設定された月間上限額（米ドル）です。"},
	},
	{
		Name: "extra-usage-percent", DisplayName: "Extra Usage (%)",
		Descriptions: Descriptions{EN: "Percentage of the extra-usage monthly limit consumed.", JA: "従量課金の月間上限に対する使用率です。"},
	},
	{
		Name: "seven-day-opus-percent", DisplayName: "Weekly Usage Opus (%)",
		Descriptions: Descriptions{EN: "Percentage of the rolling 7-day rate limit consumed by Opus models.", JA: "Opus系モデルの7日間レート制限の使用率です。"},
	},
	{
		Name: "seven-day-sonnet-percent", DisplayName: "Weekly Usage Sonnet (%)",
		Descriptions: Descriptions{EN: "Percentage of the rolling 7-day rate limit consumed by Sonnet models.", JA: "Sonnet系モデルの7日間レート制限の使用率です。"},
	},
	{
		Name: "seven-day-opus-reset-minutes", DisplayName: "Weekly Reset Opus (minutes)",
		Descriptions: Descriptions{EN: "Minutes until the Opus 7-day rate limit window resets.", JA: "Opusの7日間レート制限がリセットされるまでの分数です。"},
	},
	{
		Name: "seven-day-sonnet-reset-minutes", DisplayName: "Weekly Reset Sonnet (minutes)",
		Descriptions: Descriptions{EN: "Minutes until the Sonnet 7-day rate limit window resets.", JA: "Sonnetの7日間レート制限がリセットされるまでの分数です。"},
	},
	widthMetric,
}

// widthMetric is the tool-agnostic terminal-width metric, shared by every
// tool's catalog. It backs width-conditional visibility (when="width ge 80")
// so a layout can drop lower-priority content on narrow terminals. An unknown
// width (Options.Width <= 0) resolves to an unbounded value, so a width
// condition never hides content when the width cannot be determined.
var widthMetric = MetricDef{
	Name: "width", DisplayName: "Terminal Width",
	Descriptions: Descriptions{
		EN: "Terminal width in columns; usable in when= to show/hide by available width. Unknown width counts as unbounded (content shown).",
		JA: "端末の幅（桁数）。when= で幅に応じて表示/非表示を切り替える。幅不明時は無制限扱い（内容は表示）。",
	},
}

// claudeCodeSubagentFields is the tool="claude-code-subagent" content field
// catalog: one entry per subagentStatusLine task (an agent-panel row), per
// the subagent-status-line implementation spec. All fields read
// schema.StatusSnapshot.Subagent, which is nil outside a subagent render
// context (the renderer treats every field here as nil-safe).
var claudeCodeSubagentFields = []FieldDef{
	{
		Name: "task-description", Category: "subagent", DisplayName: "Task Description",
		Descriptions: Descriptions{EN: "The task's description (falls back to its label when empty).", JA: "タスクの説明です（空の場合はラベルにフォールバックします）。"},
	},
	{
		Name: "task-model", Category: "subagent", DisplayName: "Task Model",
		Descriptions: Descriptions{EN: "The task's model display name.", JA: "タスクで使用中のモデルの表示名です。"},
	},
	{
		Name: "task-model-id", Category: "subagent", DisplayName: "Task Model ID",
		Descriptions: Descriptions{EN: "The task's model identifier.", JA: "タスクで使用中のモデルIDです。"},
	},
	{
		Name: "task-status", Category: "subagent", DisplayName: "Task Status", Formats: []string{"enum"},
		Descriptions: Descriptions{EN: "The task's execution status (running/completed).", JA: "タスクの実行状態（running/completed）です。"},
	},
	{
		Name: "task-tokens", Category: "subagent", SelfMetric: "task-token-count", Formats: []string{"number", "compact-number"},
		DisplayName:  "Task Tokens",
		Descriptions: Descriptions{EN: "The task's current token count.", JA: "タスクの現在のトークン数です。"},
	},
	{
		// SelfMetric is a deliberate deviation from the spec table (which
		// lists "-", no self metric): without one, an explicit
		// format="compact-number" attribute would be inert (only the
		// width-driven compact/raw toggle would apply, per
		// docEval.applyFormat's "self == \"\" -> keep the renderContent
		// default" rule), leaving the token/context-size ratio in the
		// default document inconsistently formatted. This mirrors
		// context-window-size's identical SelfMetric pattern in the
		// claude-code catalog.
		Name: "task-context-size", Category: "subagent", SelfMetric: "task-context-window-tokens", Formats: []string{"number", "compact-number"},
		DisplayName:  "Task Context Size",
		Descriptions: Descriptions{EN: "The task's context window size in tokens.", JA: "タスクのコンテキストウィンドウサイズ（トークン数）です。"},
	},
	{
		Name: "task-context-percent", Category: "subagent", SelfMetric: "task-context-percent", Formats: []string{"percent"},
		DisplayName:  "Task Context %",
		Descriptions: Descriptions{EN: "Percentage of the task's context window used.", JA: "タスクのコンテキストウィンドウに対する使用率です。"},
	},
	{
		Name: "task-duration", Category: "subagent", SelfMetric: "task-duration-seconds", Formats: []string{"duration"},
		DisplayName:  "Task Duration",
		Descriptions: Descriptions{EN: "Wall-clock time elapsed since the task started.", JA: "タスク開始からの経過時間です。"},
	},
	{
		Name: "task-effort", Category: "subagent", Capability: "subagent-effort", Formats: []string{"enum"},
		DisplayName:  "Task Effort",
		Descriptions: Descriptions{EN: "The task's reasoning effort level (not yet available from Claude Code).", JA: "タスクの思考努力レベルです（現時点ではClaude Codeから提供されません）。"},
	},
}

// claudeCodeSubagentMetrics is the tool="claude-code-subagent" named-metric
// catalog, backing the self metrics above for when/color-rule expressions.
var claudeCodeSubagentMetrics = []MetricDef{
	{
		Name: "task-token-count", DisplayName: "Task Tokens",
		Descriptions: Descriptions{EN: "The task's current token count.", JA: "タスクの現在のトークン数です。"},
	},
	{
		Name: "task-context-window-tokens", DisplayName: "Task Context Window Size",
		Descriptions: Descriptions{EN: "The task's context window size in tokens.", JA: "タスクのコンテキストウィンドウサイズ（トークン数）です。"},
	},
	{
		Name: "task-context-percent", DisplayName: "Task Context Used (%)",
		Descriptions: Descriptions{EN: "Percentage of the task's context window used.", JA: "タスクのコンテキストウィンドウに対する使用率（％）です。"},
	},
	{
		Name: "task-duration-seconds", DisplayName: "Task Duration (sec)",
		Descriptions: Descriptions{EN: "Seconds elapsed since the task started.", JA: "タスク開始からの経過秒数です。"},
	},
	widthMetric,
}

// toolCatalog bundles one tool's field and metric catalogs.
type toolCatalog struct {
	fields  []FieldDef
	metrics []MetricDef
}

// toolCatalogs is keyed by the `tool` root attribute value: "claude-code"
// (the session statusLine) and "claude-code-subagent" (one row per
// subagentStatusLine task). Unknown tools yield zero values / false from
// every lookup function below.
var toolCatalogs = map[string]toolCatalog{
	"claude-code":          {fields: claudeCodeFields, metrics: claudeCodeMetrics},
	"claude-code-subagent": {fields: claudeCodeSubagentFields, metrics: claudeCodeSubagentMetrics},
}

// FieldByName looks up a field definition by tool and field name.
func FieldByName(tool, name string) (FieldDef, bool) {
	cat, ok := toolCatalogs[tool]
	if !ok {
		return FieldDef{}, false
	}
	for _, f := range cat.fields {
		if f.Name == name {
			return f, true
		}
	}
	return FieldDef{}, false
}

// Fields returns every field definition for tool, in catalog definition
// order. It returns nil for an unknown tool. The returned slice is a
// defensive copy; mutating it does not affect the registry.
func Fields(tool string) []FieldDef {
	cat, ok := toolCatalogs[tool]
	if !ok {
		return nil
	}
	out := make([]FieldDef, len(cat.fields))
	copy(out, cat.fields)
	return out
}

// MetricByName looks up a named metric definition by tool and metric
// name. It does not resolve "self" (self resolution is field/node
// specific and handled via FieldDef.SelfMetric, not this registry).
func MetricByName(tool, name string) (MetricDef, bool) {
	cat, ok := toolCatalogs[tool]
	if !ok {
		return MetricDef{}, false
	}
	for _, m := range cat.metrics {
		if m.Name == name {
			return m, true
		}
	}
	return MetricDef{}, false
}

// Metrics returns every named metric definition for tool, in catalog
// definition order. It returns nil for an unknown tool. The returned
// slice is a defensive copy.
func Metrics(tool string) []MetricDef {
	cat, ok := toolCatalogs[tool]
	if !ok {
		return nil
	}
	out := make([]MetricDef, len(cat.metrics))
	copy(out, cat.metrics)
	return out
}
