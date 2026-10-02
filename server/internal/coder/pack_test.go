// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goakili/akili/server/internal/gitid"
)

var agentID = gitid.Identity{Name: "Akili (coder-1)", Email: "coder-1@agents.example.com"}

func gitRun(t *testing.T, dir string, env []string, stdin string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append([]string{"HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "PATH=" + os.Getenv("PATH")}, env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func identityEnv(id gitid.Identity) []string {
	return []string{"GIT_AUTHOR_NAME=" + id.Name, "GIT_AUTHOR_EMAIL=" + id.Email, "GIT_COMMITTER_NAME=" + id.Name, "GIT_COMMITTER_EMAIL=" + id.Email}
}

// repo returns a repository with one commit per identity, oldest first, all touching big.txt.
func repo(t *testing.T, ids ...[]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitRun(t, dir, nil, "", "init", "-q", "-b", "main")
	var big strings.Builder
	for i := range 2000 {
		big.WriteString("line " + strings.Repeat("x", i%40) + "\n")
	}
	for i, env := range ids {
		content := big.String() + strings.Repeat("change\n", i+1)
		if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, nil, "", "add", "big.txt")
		gitRun(t, dir, env, "", "commit", "-q", "-m", "commit")
	}
	return dir
}

func countingCheck(n *int) func(string, []byte) error {
	check := identityCheck(agentID)
	return func(id string, c []byte) error {
		*n++
		return check(id, c)
	}
}

func TestInspectPackFromGit(t *testing.T) {
	agent := identityEnv(agentID)
	dir := repo(t, agent, agent, agent)
	pack := gitRun(t, dir, nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
	var n int
	if err := inspectPack(bytes.NewReader(pack), countingCheck(&n)); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("inspected %d commits, want 3", n)
	}
}

func TestSpoofedCommitsAreRefused(t *testing.T) {
	agent := identityEnv(agentID)
	ceo := gitid.Identity{Name: "CEO", Email: "ceo@example.com"}
	authorOnly := append(identityEnv(agentID), "GIT_AUTHOR_EMAIL=ceo@example.com")
	committerOnly := append(identityEnv(agentID), "GIT_COMMITTER_EMAIL=ceo@example.com")
	for name, env := range map[string][]string{"author and committer": identityEnv(ceo), "author": authorOnly, "committer": committerOnly} {
		t.Run(name, func(t *testing.T) {
			dir := repo(t, agent, env, agent)
			pack := gitRun(t, dir, nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
			err := inspectPack(bytes.NewReader(pack), identityCheck(agentID))
			if err == nil || !strings.Contains(err.Error(), "ceo@example.com") {
				t.Fatalf("spoofed commit accepted: %v", err)
			}
		})
	}
}

func TestEmailIsCaseInsensitive(t *testing.T) {
	upper := gitid.Identity{Name: agentID.Name, Email: strings.ToUpper(agentID.Email)}
	dir := repo(t, identityEnv(upper))
	pack := gitRun(t, dir, nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
	if err := inspectPack(bytes.NewReader(pack), identityCheck(agentID)); err != nil {
		t.Fatal(err)
	}
}

func TestThinPackIsRefused(t *testing.T) {
	agent := identityEnv(agentID)
	dir := repo(t, agent, agent)
	pack := gitRun(t, dir, nil, "HEAD\n^HEAD~1\n", "pack-objects", "--stdout", "--revs", "--thin", "-q")
	if err := inspectPack(bytes.NewReader(pack), identityCheck(agentID)); !errors.Is(err, errThinPack) {
		t.Fatalf("thin pack: %v", err)
	}
}

func TestEmptyPushHasNoCommits(t *testing.T) {
	if err := inspectPack(strings.NewReader(""), func(string, []byte) error { return errors.New("called") }); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedPacksAreRefused(t *testing.T) {
	agent := identityEnv(agentID)
	pack := gitRun(t, repo(t, agent), nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
	for name, body := range map[string][]byte{
		"not a pack":        []byte("NOPE\x00\x00\x00\x02\x00\x00\x00\x01"),
		"truncated":         pack[:len(pack)/2],
		"missing trailer":   pack[:len(pack)-packTrailerLen],
		"unknown version":   append([]byte("PACK\x00\x00\x00\x09"), pack[8:]...),
		"more than present": append(append([]byte("PACK\x00\x00\x00\x02\x00\x00\x00\x09"), pack[12:len(pack)-20]...), make([]byte, 20)...),
	} {
		t.Run(name, func(t *testing.T) {
			if err := inspectPack(bytes.NewReader(body), identityCheck(agentID)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func objHeader(typ int, size int) []byte {
	b := []byte{byte(typ<<4) | byte(size&0x0f)}
	size >>= 4
	for size > 0 {
		b[len(b)-1] |= 0x80
		b = append(b, byte(size&0x7f))
		size >>= 7
	}
	return b
}

func ofsDelta(rel int64) []byte {
	b := []byte{byte(rel & 0x7f)}
	for rel >>= 7; rel > 0; rel >>= 7 {
		rel--
		b = append([]byte{0x80 | byte(rel&0x7f)}, b...)
	}
	return b
}

func deflate(b []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}

func varint(n int) []byte {
	var b []byte
	for {
		c := byte(n & 0x7f)
		n >>= 7
		if n == 0 {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

// deltaPack is a pack of a genuine commit followed by an OFS_DELTA commit that copies the base's
// tree line and inserts a new author/committer block.
func deltaPack(base, insert string) []byte {
	treeLine := base[:strings.Index(base, "\n")+1]
	delta := append(varint(len(base)), varint(len(treeLine)+len(insert))...)
	delta = append(delta, 0x80|0x10, byte(len(treeLine))) // copy offset 0, size len(treeLine)
	for rest := insert; rest != ""; {
		n := min(len(rest), 127) // an insert instruction carries at most 127 bytes
		delta = append(delta, byte(n))
		delta = append(delta, rest[:n]...)
		rest = rest[n:]
	}
	var p bytes.Buffer
	p.WriteString("PACK")
	_ = binary.Write(&p, binary.BigEndian, uint32(2))
	_ = binary.Write(&p, binary.BigEndian, uint32(2))
	first := int64(p.Len())
	p.Write(objHeader(objCommit, len(base)))
	p.Write(deflate([]byte(base)))
	second := int64(p.Len())
	p.Write(objHeader(objOfsDelta, len(delta)))
	p.Write(ofsDelta(second - first))
	p.Write(deflate(delta))
	sum := sha1.Sum(p.Bytes())
	p.Write(sum[:])
	return p.Bytes()
}

func TestDeltaCommitsAreInspected(t *testing.T) {
	base := "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\nauthor Akili (coder-1) <coder-1@agents.example.com> 1700000000 +0000\n" +
		"committer Akili (coder-1) <coder-1@agents.example.com> 1700000000 +0000\n\nfirst\n"
	ok := "author Akili (coder-1) <coder-1@agents.example.com> 1700000001 +0000\ncommitter Akili (coder-1) <coder-1@agents.example.com> 1700000001 +0000\n\nsecond\n"
	spoof := "author CEO <ceo@example.com> 1700000001 +0000\ncommitter Akili (coder-1) <coder-1@agents.example.com> 1700000001 +0000\n\nsecond\n"
	var n int
	if err := inspectPack(bytes.NewReader(deltaPack(base, ok)), countingCheck(&n)); err != nil || n != 2 {
		t.Fatalf("genuine delta commit: n=%d err=%v", n, err)
	}
	if err := inspectPack(bytes.NewReader(deltaPack(base, spoof)), identityCheck(agentID)); err == nil || !strings.Contains(err.Error(), "ceo@example.com") {
		t.Fatalf("spoofed delta commit accepted: %v", err)
	}
}

func TestAmbiguousIdentLinesAreRefused(t *testing.T) {
	for _, line := range []string{
		"author Akili <coder-1@agents.example.com> <ceo@example.com> 1700000000 +0000",
		"author Akili coder-1@agents.example.com 1700000000 +0000",
	} {
		commit := "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n" + line + "\ncommitter Akili <coder-1@agents.example.com> 1700000000 +0000\n\nm\n"
		if err := identityCheck(agentID)(strings.Repeat("a", 40), []byte(commit)); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
	if err := identityCheck(agentID)(strings.Repeat("a", 40), []byte("tree x\n\nno identity\n")); err == nil {
		t.Fatal("commit without author accepted")
	}
}

// TestVerifiedPackHoldsBackTrailer: a refused pack never reaches the forge whole.
func TestVerifiedPackHoldsBackTrailer(t *testing.T) {
	agent := identityEnv(agentID)
	good := gitRun(t, repo(t, agent, agent), nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
	v := newVerifiedPack(bytes.NewReader(good), identityCheck(agentID))
	out, err := io.ReadAll(v)
	if err != nil || !bytes.Equal(out, good) || v.Refused() != nil {
		t.Fatalf("good pack: err=%v refused=%v identical=%v", err, v.Refused(), bytes.Equal(out, good))
	}

	bad := gitRun(t, repo(t, agent, identityEnv(gitid.Identity{Name: "CEO", Email: "ceo@example.com"})), nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
	v = newVerifiedPack(bytes.NewReader(bad), identityCheck(agentID))
	out, err = io.ReadAll(v)
	if err == nil || v.Refused() == nil {
		t.Fatal("spoofed pack passed through")
	}
	if len(out) > len(bad)-packTrailerLen {
		t.Fatalf("the trailer of a refused pack was released (%d of %d bytes)", len(out), len(bad))
	}
}

func TestAdvertiseNoThin(t *testing.T) {
	adv := pkt("# service=git-receive-pack\n") + "0000" +
		pkt(oldSHA+" refs/heads/main\x00report-status delete-refs ofs-delta\n") + pkt(newSHA+" refs/heads/dev\n") + "0000"
	var out bytes.Buffer
	if err := advertiseNoThin(&out, strings.NewReader(adv)); err != nil {
		t.Fatal(err)
	}
	want := pkt("# service=git-receive-pack\n") + "0000" +
		pkt(oldSHA+" refs/heads/main\x00report-status delete-refs ofs-delta no-thin\n") + pkt(newSHA+" refs/heads/dev\n") + "0000"
	if out.String() != want {
		t.Fatalf("got  %q\nwant %q", out.String(), want)
	}
	empty := pkt("# service=git-receive-pack\n") + "0000" + pkt(zeroSHA+" capabilities^{}\x00report-status\n") + "0000"
	out.Reset()
	_ = advertiseNoThin(&out, strings.NewReader(empty))
	if !strings.Contains(out.String(), "report-status no-thin\n") {
		t.Fatalf("empty repository: %q", out.String())
	}
}
