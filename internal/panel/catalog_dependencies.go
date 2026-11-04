package panel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const dependenciesMarker = "service-dependencies-20261104-v1"

var catalogDependencies = map[string]string{
	"adobe":                "adobecontent.io adobeoobe.com adobe.io adobecc.com adobess.com typekit.net typekit.com",
	"adult-bangbros":       "bangbrosnetwork.com",
	"adult-beeg":           "beeg.com",
	"adult-bongacams":      "bcicdn.com bcrncdn.com bcvcdn.com bgicdn.com",
	"adult-brazzers":       "brazzer.com",
	"adult-chaturbate":     "chaturbate.com",
	"adult-drtuber":        "drtuber.com",
	"adult-eporner":        "",
	"adult-fuq":            "fuq.com",
	"adult-hclips":         "hclips.com",
	"adult-hqporner":       "hqporner.com",
	"adult-mofos":          "mofos.com",
	"adult-motherless":     "",
	"adult-naughtyamerica": "",
	"adult-pornhub":        "pornhub-deutsch.net porngub.com",
	"adult-porntrex":       "porntrex.com",
	"adult-realitykings":   "realitykings.com",
	"adult-redtube":        "redtube.com.br",
	"adult-spankbang":      "sb-cd.com",
	"adult-stripchat":      "",
	"adult-sxyprn":         "sxyprn.com",
	"adult-tnaflix":        "tnaflix.com",
	"adult-tube8":          "tube8.com",
	"adult-txxx":           "txxx.com",
	"adult-xhamster":       "xhamster.desi xhamster18.desi xhamster19.com xhamster2.com xhamster3.com xhamsterlive.com xhapp.live xhreal5.com widgets.stripst.com",
	"adult-xnxx":           "xnxx3.com",
	"adult-xvideos":        "xvideos-ar.com xvideos-india.com",
	"adult-youjizz":        "yjcontentdelivery.com",
	"adult-youporn":        "youporngay.com",
	"autodesk":             "autodesk360.com autodesk.net",
	"bbc":                  "bbc.net.uk bbcmedia.co.uk bbc.mp-pxcdn.com bbcfmt.s.llnwi.net aod-pod-uk-live.akamaized.net as-dash-uk-live.akamaized.net as-hls-uk-live.akamaized.net vod-dash-uk-live.akamaized.net vod-dash-ww-live.akamaized.net vod-hls-uk-live.akamaized.net vod-thumb-uk-live.akamaized.net vod-thumb-ww-live.akamaized.net",
	"canva":                "canva.com canvastatus.com",
	"chatgpt":              "chat.com chatgpt.livekit.cloud host.livekit.cloud turn.livekit.cloud oaistatsig.com cdn.openaimerge.com cdn.workos.com forwarder.workos.com frontend-apps-multi-region.workos.com images.workoscdn.com setup.workos.com workos.imgix.net challenges.cloudflare.com openai.com.cdn.cloudflare.net openaiapi-site.azureedge.net openaiassets.blob.core.windows.net production-openaicom-storage.azureedge.net openaicom.imgix.net intercom.io intercomcdn.com",
	"chess":                "chesscomfiles.com",
	"claude":               "claude.app claude.new clau.de challenges.cloudflare.com",
	"currenttime":          "",
	"discord":              "discord-activities.com discordactivities.com discord.gift discord.gifts discord.new discordstatus.com discord-attachments-uploads-prd.storage.googleapis.com hammerandchisel.ssl.zendesk.com",
	"dw":                   "deutschewelle.h-cdn.com",
	"facebook":             "fb.me accountkit.com",
	"facetime":             "",
	"ficbook":              "fic.fan ficador.com ficmania.com teinon.net",
	"gemini":               "bard.google.com ai.studio makersuite.google.com ai.google.dev generativeai.google geller-pa.googleapis.com proactivebackend-pa.googleapis.com robinfrontend-pa.googleapis.com alkalicore-pa.clients6.google.com alkalimakersuite-pa.clients6.google.com webchannel-alkalimakersuite-pa.clients6.google.com geminiweb-pa.clients.google.com geminiweb-pa.clients6.google.com appsgenaiserver-pa.clients.google.com appsgenaiserver-pa.clients6.google.com appsgenaiserver-pa.googleapis.com www.googleapis.com apis.google.com www.google.com play.google.com ogs.google.com jnn-pa.googleapis.com waa-pa.clients6.google.com ogads-pa.clients6.google.com csp.withgoogle.com fonts.googleapis.com maps.googleapis.com optimizationguide-pa.googleapis.com streetviewpixels-pa.googleapis.com content-autofill.googleapis.com i.ytimg.com yt3.ggpht.com lh3.google.com accounts.youtube.com",
	"google-news":          "",
	"grammarly":            "",
	"grok":                 "grokipedia.com",
	"instagram":            "igcdn.com igtv.com facebook.com facebook.net challenges.cloudflare.com",
	"linkedin":             "l-0005.dc-msedge.net l-0005.l-msedge.net e122475.dscg.akamaiedge.net",
	"medium":               "medium.systems",
	"meduza":               "",
	"miro":                 "mirostatic.com realtimeboard.com",
	"netflix":              "netflix.com.edgesuite.net netflixdnstest0.com netflixdnstest1.com",
	"notion":               "notion-status.com",
	"patreon":              "live-patreon-marketing.pantheonsite.io",
	"rain":                 "",
	"rutor":                "",
	"rutracker":            "rutracker.cr rutracker.me rutracker.nl rutracker.wiki rutrk.org",
	"signal":               "whispersystems.org",
	"snapchat":             "snap.com sc-gw.com snap-dev.net snapkit.co impala-media-production.s3.amazonaws.com",
	"soundcloud":           "soundcloud.cloud",
	"speedtest":            "",
	"spotify":              "audio-ak-spotify-com.akamaized.net audio4-ak-spotify-com.akamaized.net heads-ak-spotify-com.akamaized.net heads4-ak-spotify-com.akamaized.net spotify.map.fastly.net",
	"svoboda":              "",
	"telegram":             "tdesktop.com telegram.space graph.org usercontent.dev",
	"tiktok":               "tiktokcdn.com.akamaized.net tiktokcdn.com.edgesuite.net tiktokcdn-us.com.edgesuite.net tiktokv.com.edgekey.net",
	"twitter":              "x.com twitter.com twimg.com",
	"viber":                "viber.co vbcdn.com vbcdn.net",
	"voa":                  "",
	"wattpad":              "",
	"whatsapp":             "",
	"wrike":                "wrike.com",
	"youtube":              "ggpht.com youtubeembeddedplayer.googleapis.com wide-youtube.l.google.com youtube-ui.l.google.com",
}

func init() {
	for _, groups := range [][]Group{presets, additionalPresets} {
		for i := range groups {
			groups[i].Domains = append(groups[i].Domains, strings.Fields(catalogDependencies[groups[i].ID])...)
			groups[i].Domains = uniqueCatalogDomains(groups[i].Domains)
		}
	}
}

func uniqueCatalogDomains(domains []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, d := range domains {
		if !seen[d] {
			out = append(out, d)
			seen[d] = true
		}
	}
	return out
}

// Merge only once so later manual removals stay removed. Never infer a service
// from a shared CDN; identify custom groups by the canonical service's main host.
func requestedDependenciesState(st State) State {
	for _, specs := range [][]Group{presets, additionalPresets} {
		for _, spec := range specs {
			index := -1
			for i, g := range st.Groups {
				if g.ID == spec.ID {
					index = i
					break
				}
			}
			if index < 0 {
				for i, g := range st.Groups {
					for _, d := range g.Domains {
						if d == spec.Domains[0] {
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
				if spec.ID != "gemini" {
					continue
				}
				st.Groups = append(st.Groups, spec)
				continue
			}
			g := &st.Groups[index]
			g.Domains = uniqueCatalogDomains(append(append([]string{}, g.Domains...), spec.Domains...))
		}
	}
	return st
}

func (s *Server) upgradeServiceDependencies() error {
	marker := filepath.Join(s.dir, dependenciesMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if s.pendingRecovery() {
		return fmt.Errorf("завершите восстановление перед обновлением зависимостей")
	}
	revision := s.revision()
	st, err := s.state()
	if err != nil {
		return err
	}
	if _, err = s.apply(requestedDependenciesState(st), revision); err != nil {
		return err
	}
	return atomicWrite(marker, []byte("Service dependencies updated once; later edits preserved\n"))
}
