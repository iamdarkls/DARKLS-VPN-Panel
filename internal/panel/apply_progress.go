package panel

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Progress is observable without taking mutation: a long-running apply must not
// block its own read-only status. It contains no domains, device IDs or secrets.
type ApplyStep struct {
	Name  string `json:"name"`
	State string `json:"state"`
}
type ApplyProgress struct {
	ID              string      `json:"id"`
	Phase           string      `json:"phase"`
	Started         int64       `json:"started"`
	Finished        int64       `json:"finished,omitempty"`
	Backup          string      `json:"backup,omitempty"`
	Disabled        bool        `json:"disabled"`
	Saved           bool        `json:"saved"`
	Verified        bool        `json:"verified"`
	RolledBack      bool        `json:"rolled_back"`
	RecoveryPending bool        `json:"recovery_pending"`
	Message         string      `json:"message"`
	Steps           []ApplyStep `json:"steps"`
}

func (s *Server) beginApply(id string) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	s.applyProgress = ApplyProgress{ID: id, Phase: "validation", Started: time.Now().Unix(), Steps: []ApplyStep{{"validation", "running"}, {"backup", "pending"}, {"saving", "pending"}, {"routing", "pending"}, {"verification", "pending"}}}
}
func (s *Server) applyStage(phase, backup string) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	p := &s.applyProgress
	for i := range p.Steps {
		if p.Steps[i].State == "running" {
			if phase == "rollback" {
				p.Steps[i].State = "error"
			} else {
				p.Steps[i].State = "done"
			}
		}
		if p.Steps[i].Name == phase {
			if phase == "routing" && s.disabled() {
				p.Steps[i].State = "skipped"
			} else {
				p.Steps[i].State = "running"
			}
		}
	}
	if phase == "rollback" {
		p.Steps = append(p.Steps, ApplyStep{"rollback", "running"})
	}
	p.Phase = phase
	if backup != "" {
		p.Backup = backup
	}
	if phase == "routing" {
		p.Saved = true
	}
}
func (s *Server) finishApply(err error) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	p := &s.applyProgress
	p.Finished = time.Now().Unix()
	p.RecoveryPending = s.pendingRecovery()
	p.Disabled = s.disabled()
	for i := range p.Steps {
		if p.Steps[i].State == "running" {
			if err == nil {
				p.Steps[i].State = "done"
			} else {
				p.Steps[i].State = "error"
			}
		}
	}
	if err == nil {
		p.Phase = "succeeded"
		p.Verified = true
		p.Message = "Настройки сохранены; файлы проверены, команды обновления DNS и правил завершились. Связь проверяется отдельно. Старый DNS-кеш приложений может потребовать переподключения."
		if p.Disabled {
			p.Message = "Настройки сохранены и файлы проверены. VPN выключен; маршрутизация не активировалась."
		}
		return
	}
	wasRollback := p.Phase == "rollback"
	p.Phase = "error"
	if wasRollback && !p.RecoveryPending {
		for i := range p.Steps {
			if p.Steps[i].Name == "rollback" {
				p.Steps[i].State = "done"
			}
		}
		p.RolledBack = true
		p.Saved = false
		p.Phase = "rolled_back"
		p.Message = "Изменения не применены; прежние файлы и маршрутизация восстановлены."
	} else if p.RecoveryPending {
		p.Message = "Восстановление не завершено. Журнал и резервная копия сохранены; откройте Контроль связи → Восстановление."
	} else {
		p.Message = "Проверка или подготовка не завершена; сохранение новых настроек не начиналось."
	}
}
func (s *Server) readApplyProgress() ApplyProgress {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	p := s.applyProgress
	p.Steps = append([]ApplyStep(nil), p.Steps...)
	return p
}

// Verify persisted results without making unrelated upstream outages roll back a local edit.
func (s *Server) verifyApplied(want State) error {
	got, e := s.state()
	if e != nil {
		return e
	}
	config, e := os.ReadFile(filepath.Join(s.root, "dnsmasq-vpn.conf"))
	if e != nil {
		return e
	}
	if !bytes.Equal(jsonBytes(got), jsonBytes(want)) || !bytes.Equal(config, dnsConfigForState(want)) {
		return errors.New("сохранённые файлы отличаются от подтверждённых настроек")
	}
	return nil
}
