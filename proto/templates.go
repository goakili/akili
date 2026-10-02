// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

// Built-in policy templates seeded on first start. They are starting points; admins copy and edit.

// commonDenyCommands are refused under every template, even with approval.
var commonDenyCommands = []string{
	"rm -rf /*", "rm -rf / *", "*mkfs*", "*dd if=*of=/dev/*", "*:(){*", "*shutdown*", "*reboot*",
	"*halt*", "*poweroff*", "*> /dev/sd*", "*chmod -R 777 /*",
}

// commonDenyPaths protect credentials and system state on every template. The agent's state files
// (its private key) are listed for the default state directory; the agent additionally refuses its
// actual state directory, wherever it is, outside the workdir.
var commonDenyPaths = []string{
	"/etc/shadow", "/etc/gshadow", "/etc/sudoers", "/etc/sudoers.d/**", "/root/.ssh/**", "/home/*/.ssh/**",
	"/proc/**", "/sys/**", "/dev/**", "/boot/**",
	"/var/lib/akili-agent/*.json", "/var/lib/akili-agent/.ssh/**", "**/.akili-agent/**",
}

// agentContainers are the agent's own containers: restarting them cuts the agent off mid-task.
var agentContainers = []string{"akili*"}

// criticalUnits must never be restarted by an agent: losing them loses the host or the agent.
var criticalUnits = []string{"sshd.service", "ssh.service", "akili-agent.service", "systemd-*", "dbus*", "networking.service", "NetworkManager.service"}

// readHostTools are the read-only operator tools.
var readHostTools = []string{ToolServiceStatus, ToolJournalLogs, ToolDiskUsage, ToolProcessList, ToolDockerPS, ToolDockerLogs,
	ToolCertCheck, ToolPackageUpdates, ToolNetProbe}

// miabiReadTools inspect Miabi apps; miabiWriteTools change them.
var (
	miabiReadTools = []string{ToolMiabiWorkspaces, ToolMiabiOverview, ToolMiabiAlerts, ToolMiabiEvents, ToolMiabiDatabases, ToolMiabiDBBackups,
		ToolMiabiTraffic, ToolMiabiEnv, ToolMiabiCronJobs, ToolMiabiPipelines, ToolMiabiApps, ToolMiabiStatus, ToolMiabiDeployments, ToolMiabiReleases, ToolMiabiLogs, ToolMiabiDeployLogs}
	miabiWriteTools = []string{ToolMiabiDeploy, ToolMiabiRollback, ToolMiabiRestart, ToolMiabiAlert, ToolMiabiDBBackup, ToolMiabiDBRestore,
		ToolMiabiEnvSet, ToolMiabiScale, ToolMiabiMaintenance, ToolMiabiCanary, ToolMiabiStackRestart, ToolMiabiCronRun, ToolMiabiPipelineRun}
)

// PolicyTemplates returns the built-in templates.
func PolicyTemplates() []Policy {
	return []Policy{
		{
			Name:    "read-only",
			Version: 5,
			Tools:   Rule{Allow: []string{ToolFSRead, ToolFSList, ToolSearch, ToolHostInfo, ToolGitStatus, ToolGitDiff, ToolLessonPropose}},
			Paths:   Rule{Allow: []string{"$WORKDIR/**", "/var/log/**", "/etc/**"}, Deny: commonDenyPaths},
			MaxRisk: RiskLow,
		},
		{
			Name:       "operator-safe",
			Version:    7,
			Tools:      Rule{Allow: append(append([]string{ToolFSRead, ToolFSList, ToolSearch, ToolHostInfo, ToolShell, ToolHTTPFetch, ToolLessonPropose}, readHostTools...), miabiReadTools...)},
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
					// Kubernetes, read-only.
					"kubectl get *", "kubectl describe *", "kubectl logs *", "kubectl top *", "kubectl version*",
					"kubectl config current-context", "kubectl config get-contexts", "kubectl cluster-info", "kubectl api-resources*",
				},
				// Secrets hold credentials: never readable by the agent.
				Deny: append([]string{"kubectl get secret*", "kubectl get * secret*", "kubectl describe secret*", "kubectl describe * secret*", "*secret/*"}, commonDenyCommands...),
			},
			Domains: Rule{Allow: []string{"*"}},
			MaxRisk: RiskHigh,
		},
		{
			// Runs production hosts: read-only tools freely, changes through approved change plans
			// (change_run) or individually approved restarts, and a recorded terminal for admins.
			Name:    "operator",
			Version: 5,
			Tools: Rule{Allow: append(append(append([]string{ToolFSRead, ToolFSList, ToolSearch, ToolHostInfo, ToolHTTPFetch, ToolShell, ToolFSEdit, ToolFSWrite,
				ToolServiceRestart, ToolDockerRestart, ToolChangeRun, ToolLessonPropose}, readHostTools...), miabiReadTools...), miabiWriteTools...)},
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
			// Everything, with no approvals required by the policy. Use only on disposable or
			// development hosts. Tools still run only up to the agent's autonomy level (use L3 so
			// high-risk calls such as shell run unattended), critical actions still need a human,
			// and the common deny lists below still apply.
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
