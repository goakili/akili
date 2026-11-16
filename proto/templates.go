// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package proto

// commonDenyCommands are refused under every template, even with approval.
var commonDenyCommands = []string{
	"rm -rf /*", "rm -rf / *", "*mkfs*", "*dd if=*of=/dev/*", "*:(){*", "*shutdown*", "*reboot*",
	"*halt*", "*poweroff*", "*> /dev/sd*", "*chmod -R 777 /*",
}

// commonDenyPaths lists the default agent state dir; the agent also refuses its actual one, wherever it is.
var commonDenyPaths = []string{
	"/etc/shadow", "/etc/gshadow", "/etc/sudoers", "/etc/sudoers.d/**", "/root/.ssh/**", "/home/*/.ssh/**",
	"/proc/**", "/sys/**", "/dev/**", "/boot/**",
	"/var/lib/akili-agent/*.json", "/var/lib/akili-agent/.ssh/**", "**/.akili-agent/**",
}

// agentContainers are the agent's own containers: restarting them cuts the agent off mid-task.
var agentContainers = []string{"akili*"}

// criticalUnits must never be restarted by an agent: losing them loses the host or the agent.
var criticalUnits = []string{"sshd.service", "ssh.service", "akili-agent.service", "systemd-*", "dbus*", "networking.service", "NetworkManager.service"}

var readHostTools = []string{ToolServiceStatus, ToolJournalLogs, ToolDiskUsage, ToolProcessList, ToolDockerPS, ToolDockerLogs,
	ToolCertCheck, ToolPackageUpdates, ToolNetProbe}

var (
	miabiReadTools = []string{ToolMiabiWorkspaces, ToolMiabiOverview, ToolMiabiAlerts, ToolMiabiEvents, ToolMiabiDatabases, ToolMiabiDBBackups,
		ToolMiabiTraffic, ToolMiabiEnv, ToolMiabiCronJobs, ToolMiabiPipelines, ToolMiabiApps, ToolMiabiStatus, ToolMiabiDeployments, ToolMiabiReleases, ToolMiabiLogs, ToolMiabiDeployLogs}
	miabiWriteTools = []string{ToolMiabiDeploy, ToolMiabiRollback, ToolMiabiRestart, ToolMiabiAlert, ToolMiabiDBBackup, ToolMiabiDBRestore,
		ToolMiabiEnvSet, ToolMiabiScale, ToolMiabiMaintenance, ToolMiabiCanary, ToolMiabiStackRestart, ToolMiabiCronRun, ToolMiabiPipelineRun}
)

// PolicyTemplates are seeded on first start as starting points for admins to copy and edit.
func PolicyTemplates() []Policy {
	return []Policy{
		{
			Name:    "read-only",
			Version: 8,
			// plan_propose only creates a draft a person must activate, so read-only agents can plan work.
			Tools:   Rule{Allow: []string{ToolFSRead, ToolFSList, ToolSearch, ToolHostInfo, ToolGitStatus, ToolGitDiff, ToolLessonPropose, ToolPlanPropose, ToolAskUser, ToolReviewSubmit}},
			Paths:   Rule{Allow: []string{"$WORKDIR/**", "/var/log/**", "/etc/**"}, Deny: commonDenyPaths},
			MaxRisk: RiskLow,
		},
		{
			Name:       "operator-safe",
			Version:    9,
			Tools:      Rule{Allow: append(append([]string{ToolFSRead, ToolFSList, ToolSearch, ToolHostInfo, ToolShell, ToolHTTPFetch, ToolLessonPropose, ToolAskUser, ToolReviewSubmit}, readHostTools...), miabiReadTools...)},
			Apps:       Rule{Allow: []string{"*"}},
			Services:   Rule{Allow: []string{"*"}},
			Containers: Rule{Allow: []string{"*"}},
			Paths:      Rule{Allow: []string{"$WORKDIR/**", "/var/log/**", "/etc/**", "/tmp/**"}, Deny: commonDenyPaths},
			Commands: Rule{
				Allow: []string{
					"df *", "df", "du *", "free *", "free", "uptime", "uname *", "hostname", "whoami", "id",
					"ps *", "ps", "top -bn1*", "ls *", "ls", "cat /var/log/*", "tail *", "head *", "grep *",
					"systemctl status *", "systemctl is-active *", "systemctl list-units*", "journalctl *",
					"docker ps*", "docker logs *", "docker stats --no-stream*", "docker inspect *",
					"ss -*", "ip addr*", "ip route*", "ping -c *", "dig *", "nslookup *", "curl -sI *",
					"openssl x509 *", "lsblk*", "mount", "uptime *",
					"kubectl get *", "kubectl describe *", "kubectl logs *", "kubectl top *", "kubectl version*",
					"kubectl config current-context", "kubectl config get-contexts", "kubectl cluster-info", "kubectl api-resources*",
				},
				Deny: append([]string{"kubectl get secret*", "kubectl get * secret*", "kubectl describe secret*", "kubectl describe * secret*", "*secret/*"}, commonDenyCommands...),
			},
			Domains: Rule{Allow: []string{"*"}},
			MaxRisk: RiskHigh,
		},
		{
			Name:    "operator",
			Version: 7,
			Tools: Rule{Allow: append(append(append([]string{ToolFSRead, ToolFSList, ToolSearch, ToolHostInfo, ToolHTTPFetch, ToolShell, ToolFSEdit, ToolFSWrite,
				ToolServiceRestart, ToolDockerRestart, ToolChangeRun, ToolLessonPropose, ToolAskUser, ToolReviewSubmit}, readHostTools...), miabiReadTools...), miabiWriteTools...)},
			Apps:       Rule{Allow: []string{"*"}},
			Paths:      Rule{Allow: []string{"$WORKDIR/**", "/var/log/**", "/etc/**", "/tmp/**", "/var/tmp/**", "/opt/**", "/srv/**"}, Deny: commonDenyPaths},
			Commands:   Rule{Allow: []string{"*"}, Deny: append([]string{"*sudo su*", "*sudo -i*", "*sudo bash*", "*sudo sh*", "*passwd*", "*visudo*", "*iptables -F*"}, commonDenyCommands...)},
			Domains:    Rule{Allow: []string{"*"}},
			Services:   Rule{Allow: []string{"*"}, Deny: criticalUnits},
			Containers: Rule{Allow: []string{"*"}, Deny: agentContainers},
			Terminal:   true,
			MaxRisk:    RiskHigh,
			// Direct writes and shell on a production host always need a human; prefer change_run.
			RequireApproval: []string{ToolShell, ToolFSWrite, ToolFSEdit},
			AllowShellMeta:  true,
		},
		{
			Name:    "developer",
			Version: 4,
			Tools:   Rule{Allow: []string{"*"}},
			Paths:   Rule{Allow: []string{"$WORKDIR/**", "/tmp/**"}, Deny: commonDenyPaths},
			Commands: Rule{
				Allow: []string{"*"},
				Deny:  append([]string{"*sudo *", "*git push*--force*", "*git push -f*"}, commonDenyCommands...),
			},
			Domains:        Rule{Allow: []string{"*"}},
			MaxRisk:        RiskHigh,
			AllowShellMeta: true,
		},
		{
			Name:            "full-with-approval",
			Version:         6,
			Apps:            Rule{Allow: []string{"*"}},
			Services:        Rule{Allow: []string{"*"}, Deny: criticalUnits},
			Containers:      Rule{Allow: []string{"*"}, Deny: agentContainers},
			Terminal:        true,
			Tools:           Rule{Allow: []string{"*"}},
			Paths:           Rule{Allow: []string{"/**"}, Deny: commonDenyPaths},
			Commands:        Rule{Allow: []string{"*"}, Deny: commonDenyCommands},
			Domains:         Rule{Allow: []string{"*"}},
			MaxRisk:         RiskCritical,
			RequireApproval: []string{ToolShell, ToolFSWrite, ToolFSEdit},
			AllowShellMeta:  true,
		},
		{
			// Disposable or development hosts only; autonomy, critical approvals and the common deny lists still apply.
			Name:           "full-no-approval",
			Version:        6,
			Apps:           Rule{Allow: []string{"*"}},
			Services:       Rule{Allow: []string{"*"}, Deny: criticalUnits},
			Containers:     Rule{Allow: []string{"*"}, Deny: agentContainers},
			Terminal:       true,
			Tools:          Rule{Allow: []string{"*"}},
			Paths:          Rule{Allow: []string{"/**"}, Deny: commonDenyPaths},
			Commands:       Rule{Allow: []string{"*"}, Deny: commonDenyCommands},
			Domains:        Rule{Allow: []string{"*"}},
			MaxRisk:        RiskCritical,
			AllowShellMeta: true,
		},
	}
}
