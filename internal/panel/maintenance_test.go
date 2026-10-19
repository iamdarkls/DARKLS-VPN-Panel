package panel

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogTrimmingKeepsTailAndAppendDescriptor(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.log")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	_, _ = f.WriteString(strings.Repeat("old line\n", 1000) + "LAST ERROR\n")
	if e = trimLog(p, 1024, 512); e != nil {
		t.Fatal(e)
	}
	_, _ = f.WriteString("still logging\n")
	b, _ := os.ReadFile(p)
	if len(b) > 540 || !strings.HasSuffix(string(b), "LAST ERROR\nstill logging\n") {
		t.Fatal(len(b), string(b))
	}
	if e = trimLog(filepath.Join(t.TempDir(), "absent"), 1024, 512); e != nil {
		t.Fatal(e)
	}
}
func goodChains() (string, string, string) {
	nat := "-A DV_NAT -p udp --dport 53 -j REDIRECT --to-ports 10535\n-A DV_NAT -p tcp --dport 53 -j REDIRECT --to-ports 10535\n"
	udp := "-A DV_UDP -p udp --dport 53 -j RETURN\n"
	for _, set := range []string{"darkls_vpn", "darkls_tg"} {
		nat += "-A DV_NAT -p tcp -m set --match-set " + set + " dst -j REDIRECT --to-ports 12345\n"
		udp += "-A DV_UDP -p udp -m set --match-set " + set + " dst -j TPROXY --on-ip 127.0.0.1 --on-port 12346 --tproxy-mark 0x1000000/0x1000000\n"
	}
	in := "-A DV_IN -p udp --dport 443 -m set --match-set darkls_yt dst -j REJECT --reject-with icmp-port-unreachable\n-A DV_IN -p udp -m mark --mark 0x1000000/0x1000000 -j ACCEPT\n-A DV_IN -p udp --dport 10535 -j ACCEPT\n-A DV_IN -p tcp -m multiport --dports 10535,12345 -j ACCEPT\n"
	return nat, udp, in
}
func TestRouterDetectsPartialRulesWithoutDependingOnOptionOrder(t *testing.T) {
	n, u, i := goodChains()
	if e := validateRouterRules(n, u, i); e != nil {
		t.Fatal(e)
	}
	if e := validateRouterRules(strings.ReplaceAll(n, "--to-ports 12345", "--to-ports 9"), u, i); e == nil {
		t.Fatal("wrong TCP port accepted")
	}
	if e := validateRouterRules(n, strings.ReplaceAll(u, "--on-port 12346", "--on-port 9"), i); e == nil {
		t.Fatal("wrong UDP port accepted")
	}
	if e := validateRouterRules(n, u, strings.ReplaceAll(i, "-j REJECT", "-j ACCEPT")); e == nil {
		t.Fatal("lost QUIC rejection accepted")
	}
}
func TestServiceClassifiesChallengeSeparatelyFromOutage(t *testing.T) {
	resp := &http.Response{StatusCode: 403, Header: http.Header{"Cf-Mitigated": []string{"challenge"}}}
	if r := classifyServiceResponse("chatgpt.com", resp, nil); r.State != "challenge" {
		t.Fatal(r)
	}
	resp.Header = http.Header{}
	if r := classifyServiceResponse("chatgpt.com", resp, nil); r.State != "http_error" {
		t.Fatal(r)
	}
	resp.StatusCode = 302
	if r := classifyServiceResponse("chatgpt.com", resp, nil); r.State != "responded" {
		t.Fatal(r)
	}
	if r := classifyServiceResponse("chatgpt.com", nil, context.DeadlineExceeded); r.State != "timeout" {
		t.Fatal(r)
	}
	if r := classifyServiceResponse("chatgpt.com", nil, errors.New("TLS failed")); r.State != "connection_error" {
		t.Fatal(r)
	}
}
func TestServiceRejectsArbitraryTargetsWithoutShellOrNetwork(t *testing.T) {
	s, r := fixture(t)
	cookie, csrf := loginTest(t, s)
	before := len(r.calls)
	w := request(s, "POST", "/api/service-probe", `{"service":"http://192.168.50.1"}`, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 400 || len(r.calls) != before {
		t.Fatal(w.Code, r.calls)
	}
}
func TestCoreRejectsUnverifiedBinaryBeforeStoppingVPN(t *testing.T) {
	s, r := fixture(t)
	p := filepath.Join(t.TempDir(), "xray")
	os.WriteFile(p, []byte("bad"), 0700)
	if e := s.upgradeCore(p); e == nil {
		t.Fatal("invalid core accepted")
	}
	for _, c := range r.calls {
		if strings.Contains(c, "stop") || strings.Contains(c, "killall") {
			t.Fatal(c)
		}
	}
}
func TestCoreRecoveryRefusesCorruptPersistentBackup(t *testing.T) {
	s, r := fixture(t)
	if e := s.restoreCore(coreJournal{Backup: filepath.Join(t.TempDir(), "missing"), Hash: "bad"}); e == nil {
		t.Fatal("missing backup accepted")
	}
	if len(r.calls) != 0 {
		t.Fatal("existing VPN stopped before backup validation")
	}
}

type recoveryRunner struct {
	failStart bool
	calls     []string
}

func (r *recoveryRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, line)
	if name == "pidof" {
		return nil, errors.New("not running")
	}
	if name == "sh" && len(args) > 1 && args[1] == "start" && r.failStart {
		return nil, errors.New("simulated startup failure")
	}
	return []byte("OK"), nil
}
func TestInterruptedCoreRecoveryRestoresVerifiedBinaryAndKeepsJournalOnFailure(t *testing.T) {
	for _, failStart := range []bool{false, true} {
		s, _ := fixture(t)
		r := &recoveryRunner{failStart: failStart}
		s.runner = r
		backup := filepath.Join(t.TempDir(), "previous-xray")
		want := []byte("verified old binary")
		os.WriteFile(backup, want, 0600)
		hash, _ := fileHash(backup)
		j := coreJournal{Backup: backup, Hash: hash}
		journal := s.corePath("xray-update-journal.json")
		atomicWrite(journal, jsonBytes(j))
		os.WriteFile(filepath.Join(s.root, "xray"), []byte("partial new core"), 0700)
		e := s.recoverCoreJournal()
		if (e != nil) != failStart {
			t.Fatal(failStart, e)
		}
		got, _ := os.ReadFile(filepath.Join(s.root, "xray"))
		if string(got) != string(want) {
			t.Fatal("old core not restored")
		}
		_, e = os.Stat(journal)
		if failStart && e != nil {
			t.Fatal("recovery journal removed before successful restart")
		}
		if !failStart && !os.IsNotExist(e) {
			t.Fatal("completed journal not removed")
		}
	}
}
