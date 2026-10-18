package main

var categoryNames = map[string]string{"video": "Видео", "ai": "Искусственный интеллект", "social": "Соцсети", "messaging": "Мессенджеры", "music": "Музыка", "reading": "Чтение и авторы", "news": "Новости и СМИ", "tools": "Сервисы и инструменты", "work": "Работа и дизайн", "adult": "18+", "custom": "Мои группы"}

func validCategory(c string) bool { _, ok := categoryNames[c]; return ok }
func inferCategory(id string) string {
	if len(id) > 6 && id[:6] == "adult-" {
		return "adult"
	}
	switch id {
	case "youtube", "tiktok", "netflix":
		return "video"
	case "chatgpt", "gemini", "claude", "grok":
		return "ai"
	case "instagram", "twitter", "facebook", "linkedin", "snapchat":
		return "social"
	case "telegram", "discord", "signal", "viber", "whatsapp", "facetime":
		return "messaging"
	case "soundcloud", "spotify":
		return "music"
	case "medium", "wattpad", "ficbook", "patreon":
		return "reading"
	case "meduza", "bbc", "dw", "svoboda", "currenttime", "voa", "rain", "google-news":
		return "news"
	case "notion", "miro", "canva", "adobe", "autodesk", "wrike":
		return "work"
	case "grammarly", "speedtest", "chess", "rutracker", "rutor":
		return "tools"
	}
	return "custom"
}
