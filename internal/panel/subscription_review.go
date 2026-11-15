package panel

import (
	"context"
	"errors"
	"reflect"
	"time"
)

// One short-lived review, protected by subscriptionMu. The exact outbound is
// kept only in process memory; neither the URL nor credentials go to the UI.
type subscriptionReview struct {
	ID      string   `json:"id,omitempty"`
	Profile string   `json:"profile,omitempty"`
	Changes []string `json:"changes"`

	config  subscriptionConfig
	out     map[string]any
	expires time.Time
}

func subscriptionChanges(current, next map[string]any) []string {
	changes := []string{}
	if outboundHash(current) == outboundHash(next) {
		return changes
	}
	oldHost, oldPort := outboundEndpoint(current)
	host, port := outboundEndpoint(next)
	if oldHost != host {
		changes = append(changes, "Адрес сервера")
	}
	if oldPort != port {
		changes = append(changes, "Порт")
	}
	oldMeta, newMeta := connectionMeta(current), connectionMeta(next)
	for _, item := range [][2]string{{"protocol", "Протокол"}, {"transport", "Транспорт"}, {"security", "Тип защиты"}} {
		if !reflect.DeepEqual(oldMeta[item[0]], newMeta[item[0]]) {
			changes = append(changes, item[1])
		}
	}
	// Other differences can include credentials; only disclose their category.
	if !reflect.DeepEqual(current["settings"], next["settings"]) && oldHost == host && oldPort == port {
		changes = append(changes, "Параметры профиля")
	}
	if !reflect.DeepEqual(current["streamSettings"], next["streamSettings"]) && reflect.DeepEqual(oldMeta, newMeta) {
		changes = append(changes, "Параметры транспорта")
	}
	if len(changes) == 0 {
		changes = append(changes, "Параметры профиля")
	}
	return changes
}

func (s *Server) previewSubscription(ctx context.Context, cfg subscriptionConfig) (subscriptionReview, error) {
	s.pendingSubscription = subscriptionReview{}
	if err := s.subscriptionBound(cfg); err != nil {
		return subscriptionReview{}, err
	}
	next, err := fetchSubscriptionOutbound(ctx, cfg)
	if err != nil {
		return subscriptionReview{}, err
	}
	current, err := s.connectionOutbound("current")
	if err != nil || outboundHash(current) != cfg.Bound {
		return subscriptionReview{}, errors.New("подключение изменилось; обновите привязку")
	}
	result := subscriptionReview{Profile: cfg.Profile, Changes: subscriptionChanges(current, next)}
	if len(result.Changes) > 0 {
		result.ID = token()
		result.config, result.out, result.expires = cfg, next, time.Now().Add(5*time.Minute)
		s.pendingSubscription = result
	}
	return result, nil
}
