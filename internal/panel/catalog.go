package panel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Curated service catalog, not a complete copy of the RKN URL registry.
// All additions are opt-in. The catalogue includes RKN restrictions and provider
// regional limits; neither inclusion nor VPN routing guarantees account access.
// See CATALOG_SOURCES.md for evidence and scope.
const xDomains = "x.com twitter.com t.co twimg.com twittercommunity.com twitteroauth.com twitter.map.fastly.net twtrdns.net twttr.com twttr.net twvid.com tweetdeck.com cms-twdigitalassets.com pscp.tv periscope.tv"
const grokDomains = "grok.com x.ai grok.x.com x.com twitter.com t.co twimg.com challenges.cloudflare.com"
const xGrokMarker = "x-grok-enabled-v1"

var additionalPresets = []Group{
	{ID: "grok", Name: "Grok / xAI", Domains: strings.Fields(grokDomains)},
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
	{ID: "tiktok", Name: "TikTok", Domains: strings.Fields("tiktok.com tiktokv.com tiktokv.eu tiktokv.us tiktokw.eu tiktokw.us tiktokcdn.com tiktokcdn-eu.com tiktokcdn-us.com tiktokeu-cdn.com tiktokrow-cdn.com tiktok-row.net tiktokd.net tiktokd.org muscdn.com musical.ly tik-tokapi.com ttlivecdn.com ttoverseaus.net ttwstatic.com tiktokcdn.com.akamaized.net tiktokcdn.com.edgesuite.net tiktokcdn-us.com.edgesuite.net tiktokv.com.edgekey.net")},
	{ID: "spotify", Name: "Spotify", Domains: strings.Fields("spotify.com scdn.co pscdn.co spotifycdn.com spotifycdn.net spotify.link spoti.fi audio-ak-spotify-com.akamaized.net audio4-ak-spotify-com.akamaized.net heads-ak-spotify-com.akamaized.net heads4-ak-spotify-com.akamaized.net spotify.map.fastly.net")},
	{ID: "netflix", Name: "Netflix", Domains: strings.Fields("netflix.com netflix.net nflxext.com nflximg.com nflximg.net nflxsearch.net nflxso.net nflxvideo.net")},
	{ID: "claude", Name: "Claude / Anthropic", Domains: strings.Fields("claude.ai claude.com anthropic.com claudeusercontent.com claude.dev claudemcpclient.com claudemcpcontent.com servd-anthropic-website.b-cdn.net")},
	{ID: "notion", Name: "Notion", Domains: strings.Fields("notion.so notion.com notion.site notion-static.com notionusercontent.com notion.new")},
	{ID: "miro", Name: "Miro", Domains: strings.Fields("miro.com mirostatic.com realtimeboard.com")},
	{ID: "canva", Name: "Canva", Domains: strings.Fields("canva.com")},
	{ID: "adobe", Name: "Adobe / Creative Cloud / Firefly", Domains: strings.Fields("adobe.com adobe.io adobelogin.com adobecc.com adobess.com adobeaemcloud.com adobeaemcloud.net")},
	{ID: "autodesk", Name: "Autodesk", Domains: strings.Fields("autodesk.com autodesk.net autodesk360.com")},
	{ID: "wrike", Name: "Wrike", Domains: strings.Fields("wrike.com")},
	{ID: "adult-beeg", Name: "Beeg · 18+", Domains: strings.Fields("beeg.com")},
	{ID: "adult-tube8", Name: "Tube8 · 18+", Domains: strings.Fields("tube8.com tube8.fr")},
	{ID: "adult-tnaflix", Name: "TNAFlix · 18+", Domains: strings.Fields("tnaflix.com")},
	{ID: "adult-youjizz", Name: "YouJizz · 18+", Domains: strings.Fields("youjizz.com yjcontentdelivery.com")},
	{ID: "adult-hclips", Name: "HClips · 18+", Domains: strings.Fields("hclips.com")},
	{ID: "adult-hqporner", Name: "HQPorner · 18+", Domains: strings.Fields("hqporner.com")},
	{ID: "adult-motherless", Name: "Motherless · 18+", Domains: strings.Fields("motherless.com")},
	{ID: "adult-porntrex", Name: "PornTrex · 18+", Domains: strings.Fields("porntrex.com")},
	{ID: "adult-chaturbate", Name: "Chaturbate · 18+", Domains: strings.Fields("chaturbate.com")},
	{ID: "adult-stripchat", Name: "Stripchat · 18+", Domains: strings.Fields("stripchat.com")},
	{ID: "adult-bongacams", Name: "BongaCams · 18+", Domains: strings.Fields("bongacams.com bcicdn.com bcrncdn.com bcvcdn.com bgicdn.com")},
	{ID: "adult-brazzers", Name: "Brazzers · 18+", Domains: strings.Fields("brazzers.com brazzers.xxx brazzersnetwork.com")},
	{ID: "adult-realitykings", Name: "Reality Kings · 18+", Domains: strings.Fields("realitykings.com rk.com")},
	{ID: "adult-naughtyamerica", Name: "Naughty America · 18+", Domains: strings.Fields("naughtyamerica.com")},
	{ID: "adult-bangbros", Name: "BangBros · 18+", Domains: strings.Fields("bangbros.com bangbrosnetwork.com")},
	{ID: "adult-mofos", Name: "Mofos · 18+", Domains: strings.Fields("mofos.com")},
	{ID: "adult-sxyprn", Name: "SxyPrn · 18+", Domains: strings.Fields("sxyprn.com")},
	{ID: "adult-fuq", Name: "Fuq · 18+", Domains: strings.Fields("fuq.com")},
	{ID: "adult-txxx", Name: "TXXX · 18+", Domains: strings.Fields("txxx.com")},
	{ID: "adult-drtuber", Name: "DrTuber · 18+", Domains: strings.Fields("drtuber.com")},
}

// Static notes are returned alongside state rather than inserted into user-owned
// groups. Existing domain lists, names, categories and enabled flags stay intact.
var catalogNotes = map[string]string{
	"tiktok":   "Ограничения самого сервиса. Регион TikTok может зависеть от SIM-карты и аккаунта; одного VPN может не хватить.",
	"spotify":  "Ограничения самого сервиса. Доступ также зависит от страны аккаунта и оплаты подписки.",
	"netflix":  "Ограничения самого сервиса. Нужна действующая подписка; Netflix может ограничивать VPN-адреса.",
	"claude":   "Региональные ограничения Anthropic. VPN не меняет правила регистрации, оплаты и доступа к аккаунту.",
	"notion":   "Ограничения самого сервиса. VPN не восстанавливает удалённые или отключённые рабочие пространства.",
	"miro":     "Ограничения самого сервиса. Доступ к аккаунту и организации определяется правилами Miro.",
	"canva":    "Ограничения самого сервиса. VPN не меняет страну аккаунта и платёжные ограничения.",
	"adobe":    "Частичные ограничения самого сервиса: продажи и отдельные функции. VPN не заменяет лицензию и не меняет оплату.",
	"autodesk": "Ограничения самого сервиса и лицензирования. VPN не восстанавливает отозванную лицензию.",
	"wrike":    "Ограничения самого сервиса. VPN не восстанавливает прекращённое обслуживание аккаунта.",
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

// The user explicitly requested these two groups enabled. This one-shot change
// merges dependencies into existing custom groups, then uses the normal apply
// transaction so DNS/rules failures roll back before the marker is committed.
func requestedXGrokState(st State) State {
	for _, spec := range []struct{ id, name, primary, alternative, domains string }{
		{"twitter", "X / Twitter", "x.com", "twitter.com", xDomains},
		{"grok", "Grok / xAI", "grok.com", "x.ai", grokDomains},
	} {
		index := -1
		for i, g := range st.Groups {
			if g.ID == spec.id {
				index = i
				break
			}
		}
		if index < 0 {
			for i, g := range st.Groups {
				for _, d := range g.Domains {
					if d == spec.primary || d == spec.alternative {
						index = i
						break
					}
				}
				if index >= 0 {
					break
				}
			}
		}
		if index < 0 {
			st.Groups = append(st.Groups, Group{ID: spec.id, Name: spec.name})
			index = len(st.Groups) - 1
		}
		g := &st.Groups[index]
		g.Enabled = true
		g.EnabledUntil = 0
		g.TimedPrevious = false
		g.Domains = append(append([]string{}, g.Domains...), strings.Fields(spec.domains)...)
	}
	return st
}

func (s *Server) enableRequestedXGrok() error {
	marker := filepath.Join(s.dir, xGrokMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if s.pendingRecovery() {
		return fmt.Errorf("завершите восстановление перед включением X и Grok")
	}
	revision := s.revision()
	st, err := s.state()
	if err != nil {
		return err
	}
	if _, err = s.apply(requestedXGrokState(st), revision); err != nil {
		return err
	}
	if err = atomicWrite(marker, []byte("X and Grok enabled once at user request\n")); err != nil {
		return err
	}
	fmt.Println("X / Twitter and Grok enabled with dependencies; later selections will be preserved")
	return nil
}
