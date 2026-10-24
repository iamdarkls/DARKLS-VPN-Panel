package panel

import (
	"context"
	"darkls-panel/internal/routerenv"
	"fmt"
	"strings"
	"time"
)

// Postconditions are measured after an action. Existence of a new PID is not
// proof that DNS or client traffic works. Unconfirmed actions remain failures.
func confirmRepair(check func() error, reason string) error {
	if err := check(); err != nil {
		return fmt.Errorf("Действие выполнено, восстановление не подтверждено (%s): %w", reason, err)
	}
	return nil
}
func (s *Server) confirmRecovery(c *Connectivity, reason string) error {
	recordRecovery(reason) // attempts, not claims of successful connectivity
	ctx, cancel := context.WithTimeout(s.networkContext(), 45*time.Second)
	defer cancel()
	scoped := s.networkOperation(ctx)
	err := confirmRepair(func() error {
		c.Xray = len(scoped.mainXrayPIDs()) > 0
		if !c.Xray {
			return fmt.Errorf("процесс Xray отсутствует")
		}
		c.DNSProcess = pidMatches("/tmp/dnsmasq-vpn.pid", "dnsmasq-vpn.conf")
		c.LocalDNS = false
		c.TunnelDNS = false
		c.Rules = false
		c.TunnelHTTPS = false
		if !c.DNSProcess {
			return fmt.Errorf("процесс VPN DNS отсутствует")
		}
		if e := testDNSContext(ctx, routerenv.HelperDNS); e != nil {
			return e
		}
		c.LocalDNS = true
		if e := testDNSContext(ctx, routerenv.TunnelDNS); e != nil {
			return e
		}
		c.TunnelDNS = true
		if e := scoped.checkRoutingOnly(); e != nil {
			return e
		}
		c.Rules = true
		if !c.Clock {
			return fmt.Errorf("ожидается синхронизация часов")
		}
		c.TrafficChecked = true
		started := time.Now()
		e := checkVPNConnectionContext(ctx)
		recordProbe(e, time.Since(started))
		c.TunnelHTTPS = e == nil
		return e
	}, reason)
	if err != nil {
		c.Errors["confirmation"] = s.scrub(err.Error())
		recordRecoveryConfirmation(fmt.Errorf("%s", c.Errors["confirmation"]))
		appendEvent("recovery_unconfirmed", s.scrub(err.Error()))
		return err
	}
	for _, key := range []string{"confirmation", "local_dns", "tunnel_dns", "rules", "tunnel_https", "incomplete"} {
		delete(c.Errors, key)
	}
	c.Complete = true
	recordRecoveryConfirmation(nil)
	appendEvent("recovery_confirmed", "DNS, правила и HTTPS подтверждены после восстановления: "+strings.Join(c.Actions, "; "))
	return nil
}
