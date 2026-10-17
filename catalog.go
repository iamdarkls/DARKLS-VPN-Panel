package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Curated service catalog, not a complete copy of the RKN URL registry.
// All additions are opt-in. Do not include a service merely because it withdrew
// from Russia itself. See CATALOG_SOURCES.md for evidence and scope.
var additionalPresets = []Group{
	{ID: "facebook", Name: "Facebook / Messenger", Domains: strings.Fields("facebook.com facebook.net fb.com fbcdn.net fbsbx.com messenger.com m.me")},
	{ID: "linkedin", Name: "LinkedIn", Domains: strings.Fields("linkedin.com licdn.com lnkd.in")},
	{ID: "signal", Name: "Signal", Domains: strings.Fields("signal.org signal.art signal.group signal.link signal.me signal.tube signalusers.org")},
	{ID: "viber", Name: "Viber", Domains: strings.Fields("viber.com viber.me vb.me")},
	{ID: "whatsapp", Name: "WhatsApp", Domains: strings.Fields("whatsapp.com whatsapp.net wa.me")},
	{ID: "snapchat", Name: "Snapchat", Domains: strings.Fields("snapchat.com snapchat.net sc-cdn.net sc-static.net snapkit.com")},
	{ID: "facetime", Name: "FaceTime (домены)", Domains: strings.Fields("facetime.apple.com facetime-apple-com.akadns.net ess.apple.com identity.apple.com push.apple.com")},
	{ID: "soundcloud", Name: "SoundCloud", Domains: strings.Fields("soundcloud.com sndcdn.com")},
	{ID: "patreon", Name: "Patreon", Domains: strings.Fields("patreon.com patreonusercontent.com patreoncommunity.com")},
	{ID: "grammarly", Name: "Grammarly", Domains: strings.Fields("grammarly.com grammarly.io grammarly.ai grammarly.net")},
	{ID: "medium", Name: "Medium", Domains: strings.Fields("medium.com mediumusercontent.com miro.medium.com")},
	{ID: "wattpad", Name: "Wattpad", Domains: strings.Fields("wattpad.com wattpad.net")},
	{ID: "ficbook", Name: "Фикбук", Domains: strings.Fields("ficbook.net")},
	{ID: "chess", Name: "Chess.com", Domains: strings.Fields("chess.com chesscomfiles.com")},
	{ID: "speedtest", Name: "Speedtest / Ookla", Domains: strings.Fields("speedtest.net speedtestcustom.com ooklaserver.net ookla.com")},
	{ID: "rutracker", Name: "RuTracker (сайт)", Domains: strings.Fields("rutracker.org rutracker.net rutracker.cc")},
	{ID: "rutor", Name: "RuTor (сайт)", Domains: strings.Fields("rutor.info rutor.org")},
	{ID: "google-news", Name: "Google Новости", Domains: strings.Fields("news.google.com news.google.ru")},
	{ID: "meduza", Name: "Медуза", Domains: strings.Fields("meduza.io")},
	{ID: "bbc", Name: "BBC", Domains: strings.Fields("bbc.com bbc.co.uk bbci.co.uk bbcimg.co.uk")},
	{ID: "dw", Name: "Deutsche Welle", Domains: strings.Fields("dw.com dw.de dwcdn.net")},
	{ID: "svoboda", Name: "Радио Свобода / RFE-RL", Domains: strings.Fields("svoboda.org rferl.org")},
	{ID: "currenttime", Name: "Настоящее Время", Domains: strings.Fields("currenttime.tv")},
	{ID: "voa", Name: "Голос Америки", Domains: strings.Fields("golosameriki.com voanews.com")},
	{ID: "rain", Name: "Телеканал Дождь", Domains: strings.Fields("tvrain.tv tvrain.ru")},
	{ID: "adult-pornhub", Name: "Pornhub · 18+", Domains: strings.Fields("pornhub.com pornhub.org pornhubpremium.com phncdn.com phprcdn.com")},
	{ID: "adult-xvideos", Name: "XVideos · 18+", Domains: strings.Fields("xvideos.com xvideos-cdn.com xv-ru.com")},
	{ID: "adult-xnxx", Name: "XNXX · 18+", Domains: strings.Fields("xnxx.com xnxx.net xnxx.tv xnxx-cdn.com")},
	{ID: "adult-xhamster", Name: "xHamster · 18+", Domains: strings.Fields("xhamster.com xhamster.xxx xhamster18.com xhcdn.com xhpingcdn.com")},
	{ID: "adult-redtube", Name: "RedTube · 18+", Domains: strings.Fields("redtube.com redtubepremium.com rdtcdn.com")},
	{ID: "adult-youporn", Name: "YouPorn · 18+", Domains: strings.Fields("youporn.com youpornpremium.com youpornru.com ypncdn.com")},
	{ID: "adult-spankbang", Name: "SpankBang · 18+", Domains: strings.Fields("spankbang.com spankbang.party sb-cd.com")},
	{ID: "adult-eporner", Name: "Eporner · 18+", Domains: strings.Fields("eporner.com")},
}

func addCatalogGroups(st State) (State, int) {
	ids, domains := map[string]bool{}, map[string]bool{}
	for _, g := range st.Groups {
		ids[g.ID] = true
		for _, d := range g.Domains {
			domains[d] = true
		}
	}
	added := 0
	for _, p := range additionalPresets {
		// Keep user-owned groups and matching services as they are, including ones
		// already added under a custom name. Shared CDN suffixes do not imply a match.
		if ids[p.ID] || domains[p.Domains[0]] {
			continue
		}
		p.Enabled = false
		p.Domains = append([]string{}, p.Domains...)
		st.Groups = append(st.Groups, p)
		ids[p.ID] = true
		added++
	}
	return st, added
}

func (s *Server) updateCatalog() error {
	release, err := s.pause()
	if err != nil {
		return err
	}
	defer release()
	if _, e := os.Stat(filepath.Join(s.dir, "apply-journal.json")); e == nil {
		return fmt.Errorf("unfinished operation; start the panel and retry")
	}
	st, err := s.state()
	if err != nil {
		return err
	}
	updated, count := addCatalogGroups(st)
	if count == 0 {
		fmt.Println("Каталог уже обновлён; текущий выбор сохранён")
		return nil
	}
	updated, err = validate(updated)
	if err != nil {
		return err
	}
	id, err := s.backup()
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(updated)); err != nil {
		return err
	}
	s.prune()
	fmt.Printf("Добавлено выключенных групп: %d. Текущий выбор сохранён. Копия: %s\n", count, id)
	return nil
}

// Apply the screenshot-confirmed dependency once; later user removals stay
// intentional and are not undone by every maintenance update.
func (s *Server) repairInstagram() error {
	marker := filepath.Join(s.dir, "instagram-fbsbx-migrated")
	if _, e := os.Stat(marker); e == nil {
		return nil
	}
	st, e := s.state()
	if e != nil {
		return e
	}
	changed := false
	for i, g := range st.Groups {
		if g.ID != "instagram" {
			continue
		}
		found := false
		for _, d := range g.Domains {
			if d == "fbsbx.com" {
				found = true
				break
			}
		}
		if !found {
			st.Groups[i].Domains = append(st.Groups[i].Domains, "fbsbx.com")
			changed = true
		}
	}
	if changed {
		if _, e = s.apply(st, s.revision()); e != nil {
			return e
		}
	}
	return atomicWrite(marker, []byte("1\n"))
}
