// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command akili-license signs and inspects Akili Enterprise license tokens. The signing keypair is
// created outside this repository (the private akili-keygen project); only the public key ships,
// baked into release binaries. This tool is never shipped.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/enterprise"
	"github.com/goakili/akili/server/internal/enterprise/license"
	"github.com/goakili/akili/server/internal/models"
)

const usage = `Usage: akili-license <command> [flags]

Commands:
  sign      sign a license token (private key from AKILI_LICENSE_SIGNING_KEY or --key-file)
  inspect   verify a token (argument or stdin) against --public-key and print its claims
  flags     list the feature flags a license can grant
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "sign":
		err = sign(os.Args[2:])
	case "inspect":
		err = inspect(os.Args[2:])
	case "flags":
		for _, f := range enterprise.AllFlags {
			fmt.Printf("%-20s %s\n", f.Name, f.Desc)
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "akili-license:", err)
		os.Exit(1)
	}
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyFile := fs.String("key-file", "", "file holding the base64 private key (default: AKILI_LICENSE_SIGNING_KEY)")
	customer := fs.String("customer", "", "customer name (required)")
	id := fs.String("id", "", "license id (default: generated)")
	days := fs.Int("days", 365, "term in days from --start")
	start := fs.String("start", "", "start date YYYY-MM-DD (default: today)")
	grace := fs.Int("grace", 30, "grace days after expiry with full function")
	flags := fs.String("flags", "all", `comma-separated feature flags, or "all"`)
	agents := fs.Int("agents", -1, "licensed agents (-1 = unlimited)")
	installID := fs.String("install-id", "", "bind to the deployment with this Install ID (Settings → License); empty = any")
	bindURL := fs.String("url", "", "also bind to the deployment with this public URL (empty = any)")
	_ = fs.Parse(args)

	if strings.TrimSpace(*customer) == "" {
		return errors.New("--customer is required")
	}
	key, err := privateKey(*keyFile)
	if err != nil {
		return err
	}
	notBefore := time.Now().UTC().Truncate(24 * time.Hour)
	if *start != "" {
		if notBefore, err = time.Parse("2006-01-02", *start); err != nil {
			return fmt.Errorf("--start: %w", err)
		}
	}
	granted, err := parseFlags(*flags)
	if err != nil {
		return err
	}
	if *id == "" {
		*id = models.NewID("lic")
	}
	tok, err := license.Sign(key, license.Claims{
		LicenseID: *id, Customer: strings.TrimSpace(*customer), Edition: enterprise.EditionEnterprise,
		InstallID: strings.TrimSpace(*installID), URL: *bindURL,
		Flags: granted, Limits: map[string]int{enterprise.LimitAgents: *agents},
		NotBefore: notBefore, NotAfter: notBefore.AddDate(0, 0, *days), GraceDays: *grace, IssuedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	fmt.Println(tok)
	return nil
}

func privateKey(file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		return strings.TrimSpace(string(b)), err
	}
	if k := strings.TrimSpace(os.Getenv("AKILI_LICENSE_SIGNING_KEY")); k != "" {
		return k, nil
	}
	return "", errors.New("no private key: set AKILI_LICENSE_SIGNING_KEY or pass --key-file")
}

func parseFlags(s string) ([]string, error) {
	if strings.TrimSpace(s) == "all" {
		out := make([]string, 0, len(enterprise.AllFlags))
		for _, f := range enterprise.AllFlags {
			out = append(out, f.Name)
		}
		return out, nil
	}
	var out []string
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !enterprise.IsKnownFlag(f) {
			return nil, fmt.Errorf("unknown flag %q (see: akili-license flags)", f)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, errors.New("--flags is empty")
	}
	return out, nil
}

func inspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	pub := fs.String("public-key", "", "base64 public key to verify against (required)")
	_ = fs.Parse(args)
	if *pub == "" {
		return errors.New("--public-key is required")
	}
	tok := strings.TrimSpace(fs.Arg(0))
	if tok == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		tok = strings.TrimSpace(string(b))
	}
	c, err := license.Verify(*pub, tok)
	if err != nil {
		return err
	}
	out := struct {
		license.Claims
		State license.State `json:"state"`
	}{c, license.Evaluate(c, time.Now()).State}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
