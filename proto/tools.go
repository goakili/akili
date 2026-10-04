// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Risk is a tool's blast radius. The control plane derives it from this catalog, never from what an
// agent claims, so a compromised agent cannot relabel a destructive call as harmless.
type Risk int

const (
	RiskLow Risk = iota + 1
	RiskMedium
	RiskHigh
	RiskCritical
)

var riskNames = map[Risk]string{RiskLow: "low", RiskMedium: "medium", RiskHigh: "high", RiskCritical: "critical"}

// Valid reports whether r is one of the four risk levels.
func (r Risk) Valid() bool { return r >= RiskLow && r <= RiskCritical }

func (r Risk) String() string {
	if s, ok := riskNames[r]; ok {
		return s
	}
	return "unknown"
}

// ParseRisk parses "low".."critical".
func ParseRisk(s string) (Risk, error) {
	for r, n := range riskNames {
		if strings.EqualFold(s, n) {
			return r, nil
		}
	}
	return 0, fmt.Errorf("unknown risk %q", s)
}

// MarshalJSON encodes an unset risk (e.g. an unknown tool) as "" so it round-trips.
func (r Risk) MarshalJSON() ([]byte, error) {
	if r == 0 {
		return json.Marshal("")
	}
	return json.Marshal(r.String())
}

func (r *Risk) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" || s == "unknown" {
		*r = 0
		return nil
	}
	v, err := ParseRisk(s)
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// ToolSpec is the control plane's and agent's shared definition of a tool.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Risk        Risk            `json:"risk"`
	InputSchema json.RawMessage `json:"input_schema"`
	// Remote tools run on the control plane (which holds the forge credentials); the agent only
	// relays the call and receives the result in the tool decision.
	Remote bool `json:"remote,omitempty"`
	// Project tools need a session bound to a project workspace.
	Project bool `json:"project,omitempty"`
	// resources extracts what the call touches, for policy checks.
	resources func(json.RawMessage) (Resources, error)
}

// Resources is what a tool call touches.
type Resources struct {
	Paths      []string `json:"paths,omitempty"`
	Commands   []string `json:"commands,omitempty"`
	Domains    []string `json:"domains,omitempty"`
	Services   []string `json:"services,omitempty"`
	Containers []string `json:"containers,omitempty"`
	Apps       []string `json:"apps,omitempty"`
}

// Def returns the model-facing definition.
func (t ToolSpec) Def() ToolDef {
	return ToolDef{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
}

// Resources parses input and returns what the call touches. Malformed input is an error, which the
// policy engine treats as deny.
func (t ToolSpec) Resources(input json.RawMessage) (Resources, error) {
	if t.resources == nil {
		return Resources{}, nil
	}
	return t.resources(input)
}

// Tool names.
const (
	ToolFSRead    = "fs_read"
	ToolFSList    = "fs_list"
	ToolFSWrite   = "fs_write"
	ToolFSEdit    = "fs_edit"
	ToolSearch    = "search"
	ToolShell     = "shell"
	ToolHTTPFetch = "http_fetch"
	ToolHostInfo  = "host_info"

	// Project (coding) tools.
	ToolGitStatus   = "git_status"
	ToolGitDiff     = "git_diff"
	ToolGitCommit   = "git_commit"
	ToolGitPush     = "git_push"
	ToolSandboxExec = "sandbox_exec"
	ToolPROpen      = "pr_open"
	ToolPRStatus    = "pr_status"

	// Operator (host) tools.
	ToolServiceStatus  = "service_status"
	ToolServiceRestart = "service_restart"
	ToolJournalLogs    = "journal_logs"
	ToolDiskUsage      = "disk_usage"
	ToolProcessList    = "process_list"
	ToolDockerPS       = "docker_ps"
	ToolDockerLogs     = "docker_logs"
	ToolDockerRestart  = "docker_restart"
	ToolCertCheck      = "cert_check"
	ToolPackageUpdates = "package_updates"
	ToolNetProbe       = "net_probe"
	// Miabi (PaaS) tools; they run on the control plane, which holds the Miabi API key.
	ToolMiabiWorkspaces   = "miabi_workspaces"
	ToolMiabiOverview     = "miabi_overview"
	ToolMiabiAlerts       = "miabi_alerts"
	ToolMiabiAlert        = "miabi_alert"
	ToolMiabiEvents       = "miabi_events"
	ToolMiabiDatabases    = "miabi_databases"
	ToolMiabiDBBackups    = "miabi_db_backups"
	ToolMiabiDBBackup     = "miabi_db_backup"
	ToolMiabiDBRestore    = "miabi_db_restore"
	ToolMiabiTraffic      = "miabi_traffic"
	ToolMiabiEnv          = "miabi_env"
	ToolMiabiEnvSet       = "miabi_env_set"
	ToolMiabiScale        = "miabi_scale"
	ToolMiabiMaintenance  = "miabi_maintenance"
	ToolMiabiCanary       = "miabi_canary"
	ToolMiabiStackRestart = "miabi_stack_restart"
	ToolMiabiCronJobs     = "miabi_cronjobs"
	ToolMiabiCronRun      = "miabi_cron_run"
	ToolMiabiPipelines    = "miabi_pipelines"
	ToolMiabiPipelineRun  = "miabi_pipeline_run"
	ToolMiabiApps         = "miabi_apps"
	ToolMiabiStatus       = "miabi_status"
	ToolMiabiDeployments  = "miabi_deployments"
	ToolMiabiReleases     = "miabi_releases"
	ToolMiabiLogs         = "miabi_logs"
	ToolMiabiDeployLogs   = "miabi_deploy_logs"
	ToolMiabiDeploy       = "miabi_deploy"
	ToolMiabiRollback     = "miabi_rollback"
	ToolMiabiRestart      = "miabi_restart"
	ToolLessonPropose     = "lesson_propose"
	ToolPlanPhaseUpdate   = "plan_phase_update"
	ToolPlanPropose       = "plan_propose"
	// ToolChangeRun executes an approved change plan: steps, verification, automatic rollback.
	ToolChangeRun = "change_run"
)

// Tool inputs. Agents decode into these; the catalog's resource extractors use the same types, so
// both sides agree on what a call touches.
type (
	FSReadInput struct {
		Path   string `json:"path"`
		Offset int    `json:"offset,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	FSListInput struct {
		Path string `json:"path"`
	}
	FSWriteInput struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	FSEditInput struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all,omitempty"`
	}
	SearchInput struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path,omitempty"`
		Glob    string `json:"glob,omitempty"`
	}
	ShellInput struct {
		Command    string `json:"command"`
		Cwd        string `json:"cwd,omitempty"`
		TimeoutSec int    `json:"timeout_sec,omitempty"`
	}
	HTTPFetchInput struct {
		URL    string `json:"url"`
		Method string `json:"method,omitempty"`
	}
	HostInfoInput struct{}

	GitDiffInput struct {
		Path   string `json:"path,omitempty"`
		Staged bool   `json:"staged,omitempty"`
	}
	GitCommitInput struct {
		Message string `json:"message"`
	}
	SandboxExecInput struct {
		Command    string `json:"command"`
		TimeoutSec int    `json:"timeout_sec,omitempty"`
	}
	PROpenInput struct {
		Title string `json:"title"`
		Body  string `json:"body,omitempty"`
		Draft bool   `json:"draft,omitempty"`
	}
	EmptyInput struct{}

	ServiceInput struct {
		Unit string `json:"unit"`
	}
	JournalInput struct {
		Unit  string `json:"unit,omitempty"`
		Since string `json:"since,omitempty"`
		Lines int    `json:"lines,omitempty"`
		Grep  string `json:"grep,omitempty"`
	}
	DiskUsageInput struct {
		Path string `json:"path,omitempty"`
	}
	ProcessListInput struct {
		Sort  string `json:"sort,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}
	DockerPSInput struct {
		All bool `json:"all,omitempty"`
	}
	ContainerInput struct {
		Container string `json:"container"`
		Lines     int    `json:"lines,omitempty"`
		Since     string `json:"since,omitempty"`
	}
	HostPortInput struct {
		Host string `json:"host"`
		Port int    `json:"port,omitempty"`
	}

	// PlanPhaseInput reports progress on a phase of a project plan linked to the task.
	PlanPhaseInput struct {
		Plan   string `json:"plan"`
		Phase  string `json:"phase"`
		Status string `json:"status"`
		Note   string `json:"note,omitempty"`
	}

	// PlanProposeInput proposes a project plan; it is stored as a draft until a person activates it.
	PlanProposeInput struct {
		Title       string              `json:"title"`
		Description string              `json:"description,omitempty"`
		Phases      []PlanProposalPhase `json:"phases"`
	}

	// PlanProposalPhase is one phase of a proposed plan.
	PlanProposalPhase struct {
		Title    string `json:"title"`
		Detail   string `json:"detail,omitempty"`
		DoneWhen string `json:"done_when,omitempty"`
	}

	// LessonInput proposes a lesson for future sessions; it is used only after an operator approves it.
	LessonInput struct {
		Lesson string `json:"lesson"`
	}

	// MiabiInput addresses a Miabi app. Workspace and app are names (handles), never numeric ids or
	// uids: policy rules match "workspace/app", so the name must be the only way to address it.
	// Integration names the Miabi integration when there are several.
	MiabiInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Integration string `json:"integration,omitempty"`
		Lines       int    `json:"lines,omitempty"`
		Limit       int    `json:"limit,omitempty"`
	}
	MiabiWorkspacesInput struct {
		Integration string `json:"integration,omitempty"`
	}
	MiabiAppsInput struct {
		Workspace   string `json:"workspace"`
		Integration string `json:"integration,omitempty"`
	}
	MiabiDeployInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Tag         string `json:"tag,omitempty"`
		Integration string `json:"integration,omitempty"`
	}
	MiabiRollbackInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		ReleaseID   int64  `json:"release_id,omitempty"`
		Integration string `json:"integration,omitempty"`
	}
	MiabiDeployLogsInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Deployment  int64  `json:"deployment"`
		Integration string `json:"integration,omitempty"`
	}

	// MiabiWorkspaceInput addresses a whole Miabi workspace (overview, alerts, events, lists).
	MiabiWorkspaceInput struct {
		Workspace   string `json:"workspace"`
		Integration string `json:"integration,omitempty"`
		Limit       int    `json:"limit,omitempty"`
		All         bool   `json:"all,omitempty"` // alerts: include resolved ones
		App         string `json:"app,omitempty"` // cron jobs: only this app's
	}
	MiabiAlertInput struct {
		Workspace   string `json:"workspace"`
		Integration string `json:"integration,omitempty"`
		Alert       int64  `json:"alert"`
		Action      string `json:"action"` // ack | resolve
	}
	// MiabiDatabaseInput addresses a database instance by name and, optionally, one logical database
	// on it (default: the only one).
	MiabiDatabaseInput struct {
		Workspace   string `json:"workspace"`
		Integration string `json:"integration,omitempty"`
		Database    string `json:"database"`
		DB          string `json:"db,omitempty"`
		Comment     string `json:"comment,omitempty"`
		Backup      int64  `json:"backup,omitempty"` // restore: the backup number
	}
	MiabiTrafficInput struct {
		Workspace    string  `json:"workspace"`
		App          string  `json:"app"`
		Integration  string  `json:"integration,omitempty"`
		Range        string  `json:"range,omitempty"`          // 15m (default), 1h, 24h
		MaxErrorRate float64 `json:"max_error_rate,omitempty"` // fraction, default 0.02
		MaxP95Ms     int     `json:"max_p95_ms,omitempty"`
	}
	MiabiEnvSetInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Integration string `json:"integration,omitempty"`
		Key         string `json:"key"`
		Value       string `json:"value"`
	}
	MiabiScaleInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Integration string `json:"integration,omitempty"`
		Replicas    int    `json:"replicas"`
	}
	MiabiMaintenanceInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Integration string `json:"integration,omitempty"`
		Enabled     bool   `json:"enabled"`
		Message     string `json:"message,omitempty"`
	}
	MiabiCanaryInput struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Integration string `json:"integration,omitempty"`
		Action      string `json:"action"` // promote | abort
	}
	// MiabiNamedInput addresses a stack, cron job or pipeline by name.
	MiabiNamedInput struct {
		Workspace   string `json:"workspace"`
		Integration string `json:"integration,omitempty"`
		Name        string `json:"name"`
		Branch      string `json:"branch,omitempty"` // pipeline runs
	}

	// ChangeCall is one tool call inside a change plan.
	ChangeCall struct {
		Tool        string          `json:"tool"`
		Input       json.RawMessage `json:"input"`
		Description string          `json:"description,omitempty"`
		// Expect, on a verify call, is text the output must contain for the check to pass.
		Expect string `json:"expect,omitempty"`
		// Reject, on a verify call, is text the output must not contain.
		Reject string `json:"reject,omitempty"`
	}
	// ChangePlan is proposed with change_run and approved by a human as a whole.
	ChangePlan struct {
		Title    string       `json:"title"`
		Reason   string       `json:"reason"`
		Steps    []ChangeCall `json:"steps"`
		Verify   []ChangeCall `json:"verify"`
		Rollback []ChangeCall `json:"rollback,omitempty"`
	}
)

func decode[T any](input json.RawMessage) (T, error) {
	var v T
	if len(input) == 0 {
		input = []byte("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("invalid input: %w", err)
	}
	return v, nil
}

var unitRE = regexp.MustCompile(`^[A-Za-z0-9@._:\\-]{1,200}$`)

// normUnit appends ".service" to bare unit names so policies match what systemd runs.
func normUnit(u string) string {
	u = strings.TrimSpace(u)
	if !strings.Contains(u, ".") {
		u += ".service"
	}
	return u
}

func validUnit(u string) error {
	if !unitRE.MatchString(strings.TrimSpace(u)) || strings.HasPrefix(strings.TrimSpace(u), "-") {
		return fmt.Errorf("invalid unit name")
	}
	return nil
}

func serviceResources(in json.RawMessage) (Resources, error) {
	v, err := decode[ServiceInput](in)
	if err == nil {
		err = validUnit(v.Unit)
	}
	return Resources{Services: []string{normUnit(v.Unit)}}, err
}

var containerRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func containerResources(in json.RawMessage) (Resources, error) {
	v, err := decode[ContainerInput](in)
	if err == nil && !containerRE.MatchString(v.Container) {
		err = fmt.Errorf("invalid container name")
	}
	return Resources{Containers: []string{v.Container}}, err
}

func hostResources(in json.RawMessage) (Resources, error) {
	v, err := decode[HostPortInput](in)
	if err == nil && (strings.TrimSpace(v.Host) == "" || strings.ContainsAny(v.Host, "/ :@")) {
		err = fmt.Errorf("host must be a plain hostname or IP")
	}
	if err == nil && (v.Port < 0 || v.Port > 65535) {
		err = fmt.Errorf("invalid port")
	}
	return Resources{Domains: []string{strings.ToLower(v.Host)}}, err
}

// wsSchema is the workspace and integration properties shared by workspace-level Miabi tools.
const wsSchema = `"workspace":{"type":"string","description":"Miabi workspace name"},"integration":{"type":"string","description":"Miabi integration name; omit to use the default"}`

// miabiSchema builds a Miabi tool schema: workspace and app plus extra properties.
func miabiSchema(extra string, required ...string) json.RawMessage {
	props := `"workspace":{"type":"string","description":"Miabi workspace name (from miabi_workspaces)"},"app":{"type":"string","description":"app name (slug)"},"integration":{"type":"string","description":"Miabi integration name; omit to use the default"}`
	if extra != "" {
		props += "," + extra
	}
	req := `"workspace","app"`
	for _, r := range required {
		req += `,"` + r + `"`
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `},"required":[` + req + `]}`)
}

var miabiAppSchema = miabiSchema("")

var appRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func validApp(a string) error {
	if !appRE.MatchString(a) {
		return fmt.Errorf("invalid app name")
	}
	return nil
}

// workspaceRE is a Miabi workspace handle (lowercase letters, digits, dashes).
var workspaceRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// miabiApp validates a workspace and app and returns the "workspace/app" resource policies match.
func miabiApp(ws, app string) (Resources, error) {
	if !workspaceRE.MatchString(ws) {
		return Resources{}, fmt.Errorf("workspace must be a Miabi workspace name (lowercase handle)")
	}
	if err := validApp(app); err != nil {
		return Resources{}, err
	}
	return Resources{Apps: []string{ws + "/" + app}}, nil
}

// miabiNamed is a non-app resource: "workspace/<kind>:<name>" (":" never appears in names, so it
// cannot be confused with an app). A rule such as "prod/*" covers every kind.
func miabiNamed(ws, kind, name string) (Resources, error) {
	if !workspaceRE.MatchString(ws) {
		return Resources{}, fmt.Errorf("workspace must be a Miabi workspace name (lowercase handle)")
	}
	if !appRE.MatchString(name) {
		return Resources{}, fmt.Errorf("invalid %s name", kind)
	}
	return Resources{Apps: []string{ws + "/" + kind + ":" + name}}, nil
}

func miabiWorkspace(ws string) (Resources, error) {
	if !workspaceRE.MatchString(ws) {
		return Resources{}, fmt.Errorf("workspace must be a Miabi workspace name (lowercase handle)")
	}
	return Resources{Apps: []string{ws + "/*"}}, nil
}

func decodeWith[T any](check func(T) (Resources, error)) func(json.RawMessage) (Resources, error) {
	return func(in json.RawMessage) (Resources, error) {
		v, err := decode[T](in)
		if err != nil {
			return Resources{}, err
		}
		return check(v)
	}
}

var envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func miabiResources(in json.RawMessage) (Resources, error) {
	v, err := decode[MiabiInput](in)
	if err != nil {
		return Resources{}, err
	}
	return miabiApp(v.Workspace, v.App)
}

func noResources(in json.RawMessage) (Resources, error) {
	_, err := decode[EmptyInput](in)
	return Resources{}, err
}

func requirePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

func withDefault(p, def string) string {
	if strings.TrimSpace(p) == "" {
		return def
	}
	return p
}

var catalog = map[string]ToolSpec{
	ToolFSRead: {
		Name:        ToolFSRead,
		Description: "Read a text file. Paths are relative to the agent workdir unless absolute. Use offset/limit (lines) for large files.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","description":"first line (0-based)"},"limit":{"type":"integer","description":"max lines"}},"required":["path"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[FSReadInput](in)
			if err == nil {
				err = requirePath(v.Path)
			}
			return Resources{Paths: []string{v.Path}}, err
		},
	},
	ToolFSList: {
		Name:        ToolFSList,
		Description: "List a directory's entries.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[FSListInput](in)
			return Resources{Paths: []string{withDefault(v.Path, ".")}}, err
		},
	},
	ToolFSWrite: {
		Name:        ToolFSWrite,
		Description: "Create or overwrite a file with the given content.",
		Risk:        RiskMedium,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[FSWriteInput](in)
			if err == nil {
				err = requirePath(v.Path)
			}
			return Resources{Paths: []string{v.Path}}, err
		},
	},
	ToolFSEdit: {
		Name:        ToolFSEdit,
		Description: "Replace an exact string in a file. old_string must be unique unless replace_all is true.",
		Risk:        RiskMedium,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["path","old_string","new_string"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[FSEditInput](in)
			if err == nil {
				err = requirePath(v.Path)
			}
			return Resources{Paths: []string{v.Path}}, err
		},
	},
	ToolSearch: {
		Name:        ToolSearch,
		Description: "Search file contents for a regular expression under a directory. Returns matching lines as path:line:text.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string","description":"file name glob, e.g. *.go"}},"required":["pattern"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[SearchInput](in)
			return Resources{Paths: []string{withDefault(v.Path, ".")}}, err
		},
	},
	ToolShell: {
		Name:        ToolShell,
		Description: "Run a shell command on the host (sh -c). Output is truncated. Prefer the dedicated file tools for reading and editing files.",
		Risk:        RiskHigh,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"cwd":{"type":"string"},"timeout_sec":{"type":"integer"}},"required":["command"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[ShellInput](in)
			if err == nil && strings.TrimSpace(v.Command) == "" {
				err = fmt.Errorf("command is required")
			}
			return Resources{Commands: []string{v.Command}, Paths: []string{withDefault(v.Cwd, ".")}}, err
		},
	},
	ToolHTTPFetch: {
		Name:        ToolHTTPFetch,
		Description: "Fetch a URL over HTTP(S) (GET or HEAD). Private and loopback addresses are refused.",
		Risk:        RiskMedium,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"method":{"type":"string","enum":["GET","HEAD"]}},"required":["url"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[HTTPFetchInput](in)
			if err != nil {
				return Resources{}, err
			}
			u, perr := url.Parse(v.URL)
			if perr != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return Resources{}, fmt.Errorf("invalid url")
			}
			return Resources{Domains: []string{strings.ToLower(u.Hostname())}}, nil
		},
	},
	ToolHostInfo: {
		Name:        ToolHostInfo,
		Description: "Report host facts: OS, CPU, memory, load, uptime, disk usage of the workdir.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		resources: func(in json.RawMessage) (Resources, error) {
			_, err := decode[HostInfoInput](in)
			return Resources{}, err
		},
	},
	ToolGitStatus: {
		Name:        ToolGitStatus,
		Description: "Show the project workspace's branch, changed files and commits not yet pushed.",
		Risk:        RiskLow,
		Project:     true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		resources:   noResources,
	},
	ToolGitDiff: {
		Name:        ToolGitDiff,
		Description: "Show the diff of uncommitted changes in the project workspace (or of staged changes), optionally for one path.",
		Risk:        RiskLow,
		Project:     true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"staged":{"type":"boolean"}}}`),
		resources: func(in json.RawMessage) (Resources, error) {
			_, err := decode[GitDiffInput](in)
			return Resources{}, err
		},
	},
	ToolGitCommit: {
		Name:        ToolGitCommit,
		Description: "Stage every change in the project workspace and commit it on the task branch with the given message.",
		Risk:        RiskMedium,
		Project:     true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","description":"conventional, imperative commit message"}},"required":["message"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[GitCommitInput](in)
			if err == nil && strings.TrimSpace(v.Message) == "" {
				err = fmt.Errorf("message is required")
			}
			return Resources{}, err
		},
	},
	ToolGitPush: {
		Name:        ToolGitPush,
		Description: "Push the task branch to the project's repository. Only akili/* branches can be pushed; the default branch is protected.",
		Risk:        RiskMedium,
		Project:     true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		resources:   noResources,
	},
	ToolSandboxExec: {
		Name: ToolSandboxExec,
		Description: "Run a command (sh -c) inside the project's disposable sandbox container with the workspace mounted at /workspace. " +
			"Use it to build and run tests. No host access; output is truncated.",
		Risk:        RiskMedium,
		Project:     true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout_sec":{"type":"integer"}},"required":["command"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[SandboxExecInput](in)
			if err == nil && strings.TrimSpace(v.Command) == "" {
				err = fmt.Errorf("command is required")
			}
			return Resources{Commands: []string{v.Command}}, err
		},
	},
	ToolPROpen: {
		Name:        ToolPROpen,
		Description: "Open a pull request from the pushed task branch into the project's default branch (or return the one already open).",
		Risk:        RiskMedium,
		Project:     true,
		Remote:      true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string","description":"what changed, why, how it was tested"},"draft":{"type":"boolean"}},"required":["title"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[PROpenInput](in)
			if err == nil && strings.TrimSpace(v.Title) == "" {
				err = fmt.Errorf("title is required")
			}
			return Resources{}, err
		},
	},
	ToolPRStatus: {
		Name:        ToolPRStatus,
		Description: "Report the task's pull request state and CI checks for the task branch head.",
		Risk:        RiskLow,
		Project:     true,
		Remote:      true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		resources:   noResources,
	},
	ToolServiceStatus: {
		Name:        ToolServiceStatus,
		Description: "Show a systemd unit's state and its most recent log lines (systemctl status).",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"unit":{"type":"string","description":"e.g. nginx.service"}},"required":["unit"]}`),
		resources:   serviceResources,
	},
	ToolServiceRestart: {
		Name:        ToolServiceRestart,
		Description: "Restart a systemd unit (through a sudo rule the operator granted) and report its state afterwards.",
		Risk:        RiskHigh,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"unit":{"type":"string"}},"required":["unit"]}`),
		resources:   serviceResources,
	},
	ToolJournalLogs: {
		Name:        ToolJournalLogs,
		Description: "Read system logs from journald, optionally for one unit, since a time (e.g. \"1 hour ago\") and filtered by a pattern.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"unit":{"type":"string"},"since":{"type":"string"},"lines":{"type":"integer"},"grep":{"type":"string"}}}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[JournalInput](in)
			if err != nil || v.Unit == "" {
				return Resources{}, err
			}
			return Resources{Services: []string{normUnit(v.Unit)}}, validUnit(v.Unit)
		},
	},
	ToolDiskUsage: {
		Name:        ToolDiskUsage,
		Description: "Report filesystem usage (df) and the largest directories under a path (default /).",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[DiskUsageInput](in)
			return Resources{Paths: []string{withDefault(v.Path, "/")}}, err
		},
	},
	ToolProcessList: {
		Name:        ToolProcessList,
		Description: "List the top processes by CPU or memory.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"sort":{"type":"string","enum":["cpu","mem"]},"limit":{"type":"integer"}}}`),
		resources: func(in json.RawMessage) (Resources, error) {
			_, err := decode[ProcessListInput](in)
			return Resources{}, err
		},
	},
	ToolDockerPS: {
		Name:        ToolDockerPS,
		Description: "List Docker containers with their state and health.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"all":{"type":"boolean"}}}`),
		resources: func(in json.RawMessage) (Resources, error) {
			_, err := decode[DockerPSInput](in)
			return Resources{}, err
		},
	},
	ToolDockerLogs: {
		Name:        ToolDockerLogs,
		Description: "Read a container's recent logs.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"container":{"type":"string"},"lines":{"type":"integer"},"since":{"type":"string"}},"required":["container"]}`),
		resources:   containerResources,
	},
	ToolDockerRestart: {
		Name:        ToolDockerRestart,
		Description: "Restart a Docker container and report its state afterwards.",
		Risk:        RiskHigh,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"container":{"type":"string"}},"required":["container"]}`),
		resources:   containerResources,
	},
	ToolCertCheck: {
		Name:        ToolCertCheck,
		Description: "Connect over TLS and report the certificate chain: subject, issuer, names and days until expiry.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"host":{"type":"string"},"port":{"type":"integer","description":"default 443"}},"required":["host"]}`),
		resources:   hostResources,
	},
	ToolPackageUpdates: {
		Name:        ToolPackageUpdates,
		Description: "List pending OS package updates (apt, dnf/yum or apk), security updates first when the package manager says so.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		resources:   noResources,
	},
	ToolNetProbe: {
		Name:        ToolNetProbe,
		Description: "Resolve a host and test a TCP connection to a port, with timing.",
		Risk:        RiskLow,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"host":{"type":"string"},"port":{"type":"integer"}},"required":["host","port"]}`),
		resources:   hostResources,
	},
	ToolChangeRun: {
		Name: ToolChangeRun,
		Description: "Make a change to the host safely. Propose the whole change as a plan: steps (tool calls that make the change), " +
			"verify (read-only checks that must succeed afterwards; \"expect\" is text the output must contain, \"reject\" text it must not) and rollback " +
			"(tool calls that undo the change). A human approves the plan once; the agent then runs the steps, runs the checks, and " +
			"rolls back automatically if a step or a check fails. Each call is still checked against the policy. Use this for any " +
			"change on a production host instead of calling mutating tools one by one.",
		Risk: RiskHigh,
		// resources is set in init: validating a plan looks tools up in this catalog.
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"reason":{"type":"string"},"steps":{"type":"array","items":{"$ref":"#/$defs/call"}},"verify":{"type":"array","items":{"$ref":"#/$defs/call"}},"rollback":{"type":"array","items":{"$ref":"#/$defs/call"}}},"required":["title","reason","steps","verify"],"$defs":{"call":{"type":"object","properties":{"tool":{"type":"string"},"input":{"type":"object"},"description":{"type":"string"},"expect":{"type":"string"},"reject":{"type":"string"}},"required":["tool","input"]}}}`),
	},
	ToolMiabiWorkspaces: {
		Name: ToolMiabiWorkspaces, Risk: RiskLow, Remote: true,
		Description: "List the Miabi workspaces Akili may use (name, role, number of apps). Every other Miabi tool takes one of these names as \"workspace\".",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"integration":{"type":"string","description":"Miabi integration name; omit to use the default"}}}`),
		resources: func(in json.RawMessage) (Resources, error) {
			_, err := decode[MiabiWorkspacesInput](in)
			return Resources{}, err
		},
	},
	ToolMiabiApps: {
		Name: ToolMiabiApps, Risk: RiskLow, Remote: true,
		Description: "List the applications in a Miabi workspace with their status, image and tag.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"workspace":{"type":"string","description":"Miabi workspace name"},"integration":{"type":"string","description":"Miabi integration name; omit to use the default"}},"required":["workspace"]}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[MiabiAppsInput](in)
			if err != nil {
				return Resources{}, err
			}
			// Listing a workspace is checked as its "workspace/*" resource.
			if !workspaceRE.MatchString(v.Workspace) {
				return Resources{}, fmt.Errorf("workspace must be a Miabi workspace name (lowercase handle)")
			}
			return Resources{Apps: []string{v.Workspace + "/*"}}, nil
		},
	},
	ToolMiabiStatus: {
		Name: ToolMiabiStatus, Risk: RiskLow, Remote: true,
		Description: "Show a Miabi app's live status (container state and health), current release, last deployment and URLs. Output includes lines \"status: <state>\" and \"health: <healthy|unhealthy|starting|none>\".",
		InputSchema: miabiAppSchema,
		resources:   miabiResources,
	},
	ToolMiabiDeployments: {
		Name: ToolMiabiDeployments, Risk: RiskLow, Remote: true,
		Description: "List a Miabi app's recent deployments (number, status, image, trigger, error).",
		InputSchema: miabiSchema(`"limit":{"type":"integer"}`),
		resources:   miabiResources,
	},
	ToolMiabiReleases: {
		Name: ToolMiabiReleases, Risk: RiskLow, Remote: true,
		Description: "List a Miabi app's releases (id, version, image, active) — the targets for miabi_rollback.",
		InputSchema: miabiAppSchema,
		resources:   miabiResources,
	},
	ToolMiabiLogs: {
		Name: ToolMiabiLogs, Risk: RiskLow, Remote: true,
		Description: "Read a Miabi app's recent container logs.",
		InputSchema: miabiSchema(`"lines":{"type":"integer"}`),
		resources:   miabiResources,
	},
	ToolMiabiDeployLogs: {
		Name: ToolMiabiDeployLogs, Risk: RiskLow, Remote: true,
		Description: "Read the build and deploy log of one Miabi deployment (by its id from miabi_deployments).",
		InputSchema: miabiSchema(`"deployment":{"type":"integer"}`, "deployment"),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[MiabiDeployLogsInput](in)
			if err != nil {
				return Resources{}, err
			}
			return miabiApp(v.Workspace, v.App)
		},
	},
	ToolMiabiDeploy: {
		Name: ToolMiabiDeploy, Risk: RiskHigh, Remote: true,
		Description: "Deploy a Miabi app (optionally a specific image tag) and wait for the deployment to finish. Prefer change_run with a miabi_status check and a miabi_rollback rollback.",
		InputSchema: miabiSchema(`"tag":{"type":"string"}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[MiabiDeployInput](in)
			if err != nil {
				return Resources{}, err
			}
			return miabiApp(v.Workspace, v.App)
		},
	},
	ToolMiabiRollback: {
		Name: ToolMiabiRollback, Risk: RiskHigh, Remote: true,
		Description: "Roll a Miabi app back to a release (default: the version that ran before) and wait for it to finish.",
		InputSchema: miabiSchema(`"release_id":{"type":"integer"}`),
		resources: func(in json.RawMessage) (Resources, error) {
			v, err := decode[MiabiRollbackInput](in)
			if err != nil {
				return Resources{}, err
			}
			return miabiApp(v.Workspace, v.App)
		},
	},
	ToolMiabiRestart: {
		Name: ToolMiabiRestart, Risk: RiskHigh, Remote: true,
		Description: "Restart a Miabi app's containers.",
		InputSchema: miabiAppSchema,
		resources:   miabiResources,
	},
	ToolMiabiOverview: {
		Name: ToolMiabiOverview, Risk: RiskLow, Remote: true,
		Description: "Summarise a Miabi workspace: every app with its status and health, database and stack counts, recent events, and quota usage.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `},"required":["workspace"]}`),
		resources:   decodeWith(func(v MiabiWorkspaceInput) (Resources, error) { return miabiWorkspace(v.Workspace) }),
	},
	ToolMiabiAlerts: {
		Name: ToolMiabiAlerts, Risk: RiskLow, Remote: true,
		Description: "List a Miabi workspace's alerts (firing and acknowledged; all=true includes resolved).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"all":{"type":"boolean"}},"required":["workspace"]}`),
		resources:   decodeWith(func(v MiabiWorkspaceInput) (Resources, error) { return miabiWorkspace(v.Workspace) }),
	},
	ToolMiabiAlert: {
		Name: ToolMiabiAlert, Risk: RiskMedium, Remote: true,
		Description: "Acknowledge or resolve a Miabi alert (by id from miabi_alerts) once you have handled it.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"alert":{"type":"integer"},"action":{"type":"string","enum":["ack","resolve"]}},"required":["workspace","alert","action"]}`),
		resources: decodeWith(func(v MiabiAlertInput) (Resources, error) {
			if v.Action != "ack" && v.Action != "resolve" {
				return Resources{}, fmt.Errorf("action must be ack or resolve")
			}
			if v.Alert <= 0 {
				return Resources{}, fmt.Errorf("alert id required")
			}
			return miabiWorkspace(v.Workspace)
		}),
	},
	ToolMiabiEvents: {
		Name: ToolMiabiEvents, Risk: RiskLow, Remote: true,
		Description: "List a Miabi workspace's recent events (deploys, containers, databases, backups, drift).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"limit":{"type":"integer"}},"required":["workspace"]}`),
		resources:   decodeWith(func(v MiabiWorkspaceInput) (Resources, error) { return miabiWorkspace(v.Workspace) }),
	},
	ToolMiabiDatabases: {
		Name: ToolMiabiDatabases, Risk: RiskLow, Remote: true,
		Description: "List a Miabi workspace's databases with engine, version, status and live health.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `},"required":["workspace"]}`),
		resources:   decodeWith(func(v MiabiWorkspaceInput) (Resources, error) { return miabiWorkspace(v.Workspace) }),
	},
	ToolMiabiDBBackups: {
		Name: ToolMiabiDBBackups, Risk: RiskLow, Remote: true,
		Description: "List the backups of a Miabi database (number, status, size, time).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"database":{"type":"string","description":"database instance name"},"db":{"type":"string","description":"logical database (default: the only one)"}},"required":["workspace","database"]}`),
		resources:   decodeWith(func(v MiabiDatabaseInput) (Resources, error) { return miabiNamed(v.Workspace, "db", v.Database) }),
	},
	ToolMiabiDBBackup: {
		Name: ToolMiabiDBBackup, Risk: RiskMedium, Remote: true,
		Description: "Take a backup of a Miabi database now (to the workspace's configured destination). Do this before any risky change.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"database":{"type":"string"},"db":{"type":"string"},"comment":{"type":"string"}},"required":["workspace","database"]}`),
		resources:   decodeWith(func(v MiabiDatabaseInput) (Resources, error) { return miabiNamed(v.Workspace, "db", v.Database) }),
	},
	ToolMiabiDBRestore: {
		Name: ToolMiabiDBRestore, Risk: RiskCritical, Remote: true,
		Description: "Restore a Miabi database from one of its backups (by number from miabi_db_backups). Overwrites current data; always needs a human.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"database":{"type":"string"},"db":{"type":"string"},"backup":{"type":"integer"}},"required":["workspace","database","backup"]}`),
		resources: decodeWith(func(v MiabiDatabaseInput) (Resources, error) {
			if v.Backup <= 0 {
				return Resources{}, fmt.Errorf("backup number required")
			}
			return miabiNamed(v.Workspace, "db", v.Database)
		}),
	},
	ToolMiabiTraffic: {
		Name: ToolMiabiTraffic, Risk: RiskLow, Remote: true,
		Description: "Measure a Miabi app's HTTP traffic (requests, error rate, p95 latency) over a window. Output includes \"traffic: ok\" or \"traffic: degraded\" against max_error_rate (default 0.02) and max_p95_ms, for change_run verify checks.",
		InputSchema: miabiSchema(`"range":{"type":"string","enum":["15m","1h","24h"]},"max_error_rate":{"type":"number"},"max_p95_ms":{"type":"integer"}`),
		resources:   decodeWith(func(v MiabiTrafficInput) (Resources, error) { return miabiApp(v.Workspace, v.App) }),
	},
	ToolMiabiEnv: {
		Name: ToolMiabiEnv, Risk: RiskLow, Remote: true,
		Description: "List a Miabi app's environment variables (secret values stay masked).",
		InputSchema: miabiAppSchema,
		resources:   miabiResources,
	},
	ToolMiabiEnvSet: {
		Name: ToolMiabiEnvSet, Risk: RiskHigh, Remote: true,
		Description: "Set a non-secret environment variable on a Miabi app (secrets are refused; set them in Miabi). Takes effect on the next deploy.",
		InputSchema: miabiSchema(`"key":{"type":"string"},"value":{"type":"string"}`, "key", "value"),
		resources: decodeWith(func(v MiabiEnvSetInput) (Resources, error) {
			if !envKeyRE.MatchString(v.Key) {
				return Resources{}, fmt.Errorf("invalid variable name")
			}
			return miabiApp(v.Workspace, v.App)
		}),
	},
	ToolMiabiScale: {
		Name: ToolMiabiScale, Risk: RiskHigh, Remote: true,
		Description: "Set a Miabi app's replica count (1-100; cluster apps only).",
		InputSchema: miabiSchema(`"replicas":{"type":"integer","minimum":1,"maximum":100}`, "replicas"),
		resources: decodeWith(func(v MiabiScaleInput) (Resources, error) {
			if v.Replicas < 1 || v.Replicas > 100 {
				return Resources{}, fmt.Errorf("replicas must be 1-100")
			}
			return miabiApp(v.Workspace, v.App)
		}),
	},
	ToolMiabiMaintenance: {
		Name: ToolMiabiMaintenance, Risk: RiskHigh, Remote: true,
		Description: "Turn maintenance mode on or off for every route of a Miabi app (visitors get a maintenance page).",
		InputSchema: miabiSchema(`"enabled":{"type":"boolean"},"message":{"type":"string"}`, "enabled"),
		resources: decodeWith(func(v MiabiMaintenanceInput) (Resources, error) {
			if len(v.Message) > 1024 {
				return Resources{}, fmt.Errorf("message too long")
			}
			return miabiApp(v.Workspace, v.App)
		}),
	},
	ToolMiabiCanary: {
		Name: ToolMiabiCanary, Risk: RiskHigh, Remote: true,
		Description: "Promote a Miabi app's running canary to all traffic, or abort it.",
		InputSchema: miabiSchema(`"action":{"type":"string","enum":["promote","abort"]}`, "action"),
		resources: decodeWith(func(v MiabiCanaryInput) (Resources, error) {
			if v.Action != "promote" && v.Action != "abort" {
				return Resources{}, fmt.Errorf("action must be promote or abort")
			}
			return miabiApp(v.Workspace, v.App)
		}),
	},
	ToolMiabiStackRestart: {
		Name: ToolMiabiStackRestart, Risk: RiskHigh, Remote: true,
		Description: "Restart every app of a Miabi stack, one at a time.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"name":{"type":"string","description":"stack name"}},"required":["workspace","name"]}`),
		resources:   decodeWith(func(v MiabiNamedInput) (Resources, error) { return miabiNamed(v.Workspace, "stack", v.Name) }),
	},
	ToolMiabiCronJobs: {
		Name: ToolMiabiCronJobs, Risk: RiskLow, Remote: true,
		Description: "List a Miabi workspace's cron jobs (optionally one app's) with schedule and last run.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"app":{"type":"string"}},"required":["workspace"]}`),
		resources:   decodeWith(func(v MiabiWorkspaceInput) (Resources, error) { return miabiWorkspace(v.Workspace) }),
	},
	ToolMiabiCronRun: {
		Name: ToolMiabiCronRun, Risk: RiskMedium, Remote: true,
		Description: "Run a Miabi cron job now and report how the run ended.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"name":{"type":"string","description":"cron job name"}},"required":["workspace","name"]}`),
		resources:   decodeWith(func(v MiabiNamedInput) (Resources, error) { return miabiNamed(v.Workspace, "cron", v.Name) }),
	},
	ToolMiabiPipelines: {
		Name: ToolMiabiPipelines, Risk: RiskLow, Remote: true,
		Description: "List a Miabi workspace's pipelines with their last run.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `},"required":["workspace"]}`),
		resources:   decodeWith(func(v MiabiWorkspaceInput) (Resources, error) { return miabiWorkspace(v.Workspace) }),
	},
	ToolMiabiPipelineRun: {
		Name: ToolMiabiPipelineRun, Risk: RiskHigh, Remote: true,
		Description: "Run a Miabi pipeline (build, and usually deploy) and wait for it to finish.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + wsSchema + `,"name":{"type":"string","description":"pipeline name"},"branch":{"type":"string"}},"required":["workspace","name"]}`),
		resources:   decodeWith(func(v MiabiNamedInput) (Resources, error) { return miabiNamed(v.Workspace, "pipeline", v.Name) }),
	},
	ToolLessonPropose: {
		Name: ToolLessonPropose, Risk: RiskLow, Remote: true,
		Description: "Propose a short, durable lesson for your future sessions (a host quirk, a working procedure, a pitfall). " +
			"An operator reviews it; only approved lessons appear in later sessions. One fact per lesson, 10-400 characters, no secrets.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"lesson":{"type":"string","minLength":10,"maxLength":400}},"required":["lesson"],"additionalProperties":false}`),
		resources:   lessonResources,
	},
	ToolPlanPhaseUpdate: {
		Name: ToolPlanPhaseUpdate, Risk: RiskLow, Remote: true, Project: true,
		Description: "Report progress on a phase of a project plan linked to this task: in_progress when you start it, done when it is finished, " +
			"skipped (with a note saying why) when it is not needed. Use the plan and phase ids from the task's Plans section.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"plan":{"type":"string"},"phase":{"type":"string"},` +
			`"status":{"type":"string","enum":["in_progress","done","skipped"]},"note":{"type":"string","maxLength":1000}},` +
			`"required":["plan","phase","status"],"additionalProperties":false}`),
		resources: planPhaseResources,
	},
	ToolPlanPropose: {
		Name: ToolPlanPropose, Risk: RiskLow, Remote: true, Project: true,
		Description: "Propose a plan for work on this project: a one-line title, a description of the goal and approach, and ordered phases " +
			"(each a small, reviewable step, with what done means). It is saved as a draft: a person reviews, edits and activates it before any task works on it.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":200},` +
			`"description":{"type":"string","maxLength":8000},"phases":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"object","properties":{` +
			`"title":{"type":"string","minLength":1,"maxLength":300},"detail":{"type":"string","maxLength":2000},"done_when":{"type":"string","maxLength":1000}},` +
			`"required":["title"],"additionalProperties":false}}},"required":["title","phases"],"additionalProperties":false}`),
		resources: planProposeResources,
	},
}

// Bounds of a proposed plan. They are tighter than what people can write: an agent's proposal is a
// starting point for review, and every linked plan ends up in a task's goal.
const (
	MaxProposedPhases      = 30
	MaxProposedTitle       = 200
	MaxProposedPhaseTitle  = 300
	MaxProposedDescription = 8000
	MaxProposedDetail      = 2000
	MaxProposedDoneWhen    = 1000
)

func planProposeResources(in json.RawMessage) (Resources, error) {
	v, err := decode[PlanProposeInput](in)
	if err != nil {
		return Resources{}, err
	}
	if t := strings.TrimSpace(v.Title); t == "" || len([]rune(t)) > MaxProposedTitle || strings.ContainsAny(t, "\r\n") {
		return Resources{}, fmt.Errorf("a plan needs a one-line title of 1-%d characters", MaxProposedTitle)
	}
	if len([]rune(v.Description)) > MaxProposedDescription {
		return Resources{}, fmt.Errorf("the description is at most %d characters", MaxProposedDescription)
	}
	if len(v.Phases) == 0 || len(v.Phases) > MaxProposedPhases {
		return Resources{}, fmt.Errorf("a plan has 1-%d phases", MaxProposedPhases)
	}
	for i, p := range v.Phases {
		if t := strings.TrimSpace(p.Title); t == "" || len([]rune(t)) > MaxProposedPhaseTitle || strings.ContainsAny(t, "\r\n") {
			return Resources{}, fmt.Errorf("phase %d needs a one-line title of 1-%d characters", i+1, MaxProposedPhaseTitle)
		}
		if len([]rune(p.Detail)) > MaxProposedDetail || len([]rune(p.DoneWhen)) > MaxProposedDoneWhen {
			return Resources{}, fmt.Errorf("phase %d: detail is at most %d characters and done_when at most %d", i+1, MaxProposedDetail, MaxProposedDoneWhen)
		}
	}
	return Resources{}, nil
}

// MaxPlanNote bounds the note an agent leaves on a plan phase.
const MaxPlanNote = 1000

func planPhaseResources(in json.RawMessage) (Resources, error) {
	v, err := decode[PlanPhaseInput](in)
	if err != nil {
		return Resources{}, err
	}
	if v.Plan == "" || v.Phase == "" {
		return Resources{}, fmt.Errorf("plan and phase are required")
	}
	switch v.Status {
	case "in_progress", "done", "skipped":
	default:
		return Resources{}, fmt.Errorf("status must be in_progress, done or skipped")
	}
	if len([]rune(v.Note)) > MaxPlanNote {
		return Resources{}, fmt.Errorf("a note is at most %d characters", MaxPlanNote)
	}
	return Resources{}, nil
}

// MaxLessonLen bounds a proposed lesson: lessons land in every future system prompt of the agent.
const MaxLessonLen = 400

func lessonResources(in json.RawMessage) (Resources, error) {
	v, err := decode[LessonInput](in)
	if err != nil {
		return Resources{}, err
	}
	n := len([]rune(strings.TrimSpace(v.Lesson)))
	if n < 10 || n > MaxLessonLen {
		return Resources{}, fmt.Errorf("a lesson must be 10-%d characters", MaxLessonLen)
	}
	return Resources{}, nil
}

func init() {
	t := catalog[ToolChangeRun]
	t.resources = func(in json.RawMessage) (Resources, error) {
		_, err := ParseChangePlan(in)
		return Resources{}, err
	}
	catalog[ToolChangeRun] = t
}

// LookupTool returns a tool spec from the catalog.
func LookupTool(name string) (ToolSpec, bool) {
	if t, ok := catalog[name]; ok {
		return t, ok
	}
	dynMu.RLock()
	defer dynMu.RUnlock()
	t, ok := dynamic[name]
	return t, ok
}

// MCP tools are discovered at run time from MCP servers the control plane connects to. They are
// named "mcp__<server>__<tool>", always run on the control plane, and carry the risk an admin
// assigned. Their inputs are opaque to policy: they are allowed or denied by name and risk only.
var (
	dynMu   sync.RWMutex
	dynamic = map[string]ToolSpec{}
	mcpRE   = regexp.MustCompile(`^mcp__[a-z0-9][a-z0-9-]{0,31}__[A-Za-z0-9_.-]{1,64}$`)
)

// MCPToolName builds a dynamic tool's name.
func MCPToolName(server, tool string) string { return "mcp__" + server + "__" + tool }

// IsMCPTool reports whether name is a well-formed MCP tool name.
func IsMCPTool(name string) bool { return mcpRE.MatchString(name) }

// DynamicTool describes an MCP tool for an agent's session.
type DynamicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Risk        Risk            `json:"risk"`
}

// SetDynamicTools replaces the registered MCP tools whose names start with prefix (one server's
// tools: "mcp__<server>__"). Inputs must be JSON objects; nothing else is checked.
func SetDynamicTools(prefix string, tools []DynamicTool) {
	dynMu.Lock()
	defer dynMu.Unlock()
	for name := range dynamic {
		if strings.HasPrefix(name, prefix) {
			delete(dynamic, name)
		}
	}
	for _, t := range tools {
		if !IsMCPTool(t.Name) || !strings.HasPrefix(t.Name, prefix) || !t.Risk.Valid() {
			continue
		}
		dynamic[t.Name] = ToolSpec{Name: t.Name, Description: t.Description, Risk: t.Risk, InputSchema: t.InputSchema, Remote: true,
			resources: func(in json.RawMessage) (Resources, error) {
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(in, &obj); err != nil {
					return Resources{}, fmt.Errorf("arguments must be a JSON object")
				}
				return Resources{}, nil
			}}
	}
}

// DynamicTools returns the registered MCP tools.
func DynamicTools() []ToolSpec {
	dynMu.RLock()
	defer dynMu.RUnlock()
	out := make([]ToolSpec, 0, len(dynamic))
	for _, t := range dynamic {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Catalog returns every tool, sorted by name.
func Catalog() []ToolSpec {
	out := make([]ToolSpec, 0, len(catalog))
	for _, t := range catalog {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
