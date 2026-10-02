// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package enterprise is the edition seam: one interface, EE, that gates licensed features. A
// Community build links a deny-all stub; a build with the `enterprise` tag links the implementation
// that verifies signed licenses. Safety features (policies, approvals, audit, the kill switch) are
// never gated here.
package enterprise

import (
	"context"
	"errors"
	"time"
)

// Editions.
const (
	EditionCommunity  = "community"
	EditionEnterprise = "enterprise"
)

// Entitlement flags a license may grant. Services gate a feature with Require(flag), so adding a
// paid feature never changes the license format.
const (
	FlagTeams              = "teams"
	FlagCustomRoles        = "custom_roles"
	FlagSAML               = "saml"
	FlagSCIM               = "scim"
	FlagApprovalGovernance = "approval_governance"
	FlagChangeWindows      = "change_windows"
	FlagAuditRetention     = "audit_retention"
	FlagAuditArchive       = "audit_archive"
	FlagDLP                = "dlp"
	FlagCloudModels        = "cloud_models"
	FlagCloudKMS           = "cloud_kms"
	FlagPolicyGit          = "policy_git"
	FlagChangeTickets      = "change_tickets"
	FlagTeamBudgets        = "team_budgets"
	FlagFleetRollouts      = "fleet_rollouts"
)

// FlagInfo describes one entitlement flag.
type FlagInfo struct {
	Name string `json:"name"`
	Desc string `json:"description"`
}

// AllFlags is the ordered list of flags a license may grant.
var AllFlags = []FlagInfo{
	{FlagTeams, "Teams and agent groups with scoped permissions and delegated admins"},
	{FlagCustomRoles, "Custom roles scoped to agent groups"},
	{FlagSAML, "SAML single sign-on"},
	{FlagSCIM, "SCIM user and group provisioning"},
	{FlagApprovalGovernance, "Approval quorum, four-eyes, approver groups and escalation"},
	{FlagChangeWindows, "Change windows and freeze calendars"},
	{FlagAuditRetention, "Audit retention rules and legal hold"},
	{FlagAuditArchive, "Write-once audit archive, signed exports and evidence packs"},
	{FlagDLP, "Data loss prevention and model routing by data class"},
	{FlagCloudModels, "Bedrock, Azure OpenAI and Vertex with cloud IAM"},
	{FlagCloudKMS, "AWS KMS, GCP KMS, Azure Key Vault and HSM"},
	{FlagPolicyGit, "Policies and skills managed in Git"},
	{FlagChangeTickets, "ServiceNow and Jira change tickets"},
	{FlagTeamBudgets, "Budgets per team and project, chargeback reports"},
	{FlagFleetRollouts, "Fleet-wide rollouts in batches and agent update channels"},
}

// IsKnownFlag reports whether name is an entitlement flag.
func IsKnownFlag(name string) bool {
	for _, f := range AllFlags {
		if f.Name == name {
			return true
		}
	}
	return false
}

// LimitAgents is the number of agents a license covers (-1 = unlimited).
const LimitAgents = "agents"

// StateBindingMismatch reports an authentic license issued for another deployment.
const StateBindingMismatch = "binding_mismatch"

// Entitlements is what the installed license grants right now.
type Entitlements struct {
	Edition string `json:"edition"`
	// State is valid, grace, degraded, none or binding_mismatch.
	State     string `json:"state"`
	Customer  string `json:"customer,omitempty"`
	LicenseID string `json:"license_id,omitempty"`
	// InstallID identifies this deployment; customers quote it to get a license bound to it.
	InstallID string `json:"install_id"`
	// LicenseInstallID and URL are the bindings the installed license carries, if any.
	LicenseInstallID string `json:"license_install_id,omitempty"`
	URL              string `json:"url,omitempty"`
	// Licensable is false for a Community build or one without a license public key.
	Licensable   bool            `json:"licensable"`
	BindingError string          `json:"binding_error,omitempty"`
	Flags        map[string]bool `json:"flags"`
	Limits       map[string]int  `json:"limits"`
	NotAfter     *time.Time      `json:"not_after,omitempty"`
	GraceEnds    *time.Time      `json:"grace_ends,omitempty"`
}

var (
	// ErrLicenseRequired: the feature needs an Enterprise license and none is active.
	ErrLicenseRequired = errors.New("this feature needs an Akili Enterprise license")
	// ErrEntitlementDenied: a license is active but does not include the feature.
	ErrEntitlementDenied = errors.New("your Akili Enterprise license does not include this feature")
	// ErrLicenseExpired: past the grace period the feature keeps running but cannot be reconfigured.
	ErrLicenseExpired = errors.New("the Akili Enterprise license has expired: this feature keeps running but its settings are read-only until the license is renewed")
	// ErrCommunityEdition: this binary cannot activate a license.
	ErrCommunityEdition = errors.New("this build is the Community edition and cannot activate a license")
	// ErrNoPublicKey: an Enterprise build without a license public key cannot verify licenses.
	ErrNoPublicKey = errors.New("this build has no license public key, so it cannot verify licenses")
	// ErrBindingMismatch: the license was issued for another deployment (Install ID or URL).
	ErrBindingMismatch = errors.New("this license is bound to another deployment")
)

// EE gates licensed features. Has and Require answer "may this feature run"; Mutable and
// RequireMutable also require the license to be within its term or grace period, so a lapsed
// license freezes configuration without switching anything off.
type EE interface {
	Entitlements() Entitlements
	Has(flag string) bool
	Mutable(flag string) bool
	Require(flag string) error
	RequireMutable(flag string) error
	Install(ctx context.Context, token string) (Entitlements, error)
	Remove(ctx context.Context) error
}

func emptyEntitlements(e Entitlements) Entitlements {
	if e.Edition == "" {
		e.Edition = EditionCommunity
	}
	if e.Flags == nil {
		e.Flags = map[string]bool{}
	}
	if e.Limits == nil {
		e.Limits = map[string]int{}
	}
	return e
}
