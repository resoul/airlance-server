package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/message"
)

type LinkPreviews interface {
	PreviewChat(ctx context.Context, client, code string) (message.Chat, error)
	PreviewUser(ctx context.Context, client, username string) (directory.Person, error)
}

type Links struct {
	Previews   LinkPreviews
	AppleAppID string
	Logger     *slog.Logger
}

type linkPage struct {
	Lang   string
	Title  string
	Detail string
	Open   template.URL
	Button string
}

type linkTexts struct {
	members, subscribers, open, join, invalid, busy, generic string
}

var linkCode = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

var linkTemplate = template.Must(template.New("link").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; }
body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
  font: 17px -apple-system, system-ui, sans-serif; text-align: center; padding: 24px; box-sizing: border-box; }
main { max-width: 420px; }
h1 { font-size: 26px; margin: 0 0 8px; overflow-wrap: anywhere; }
p { margin: 0 0 24px; opacity: .65; overflow-wrap: anywhere; }
a { display: inline-block; padding: 14px 28px; border-radius: 24px; background: #0a84ff; color: #fff;
  text-decoration: none; font-weight: 600; }
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
{{if .Detail}}<p>{{.Detail}}</p>{{end}}
{{if .Open}}<a href="{{.Open}}">{{.Button}}</a>{{end}}
</main>
</body>
</html>
`))

var linkLanguages = map[string]linkTexts{
	"en": {
		"Members: %d", "Subscribers: %d", "Open in Airlance", "Join in Airlance",
		"This link is not valid", "Too many requests, try again in a minute", "Something went wrong",
	},
	"ru": {
		"Участников: %d", "Подписчиков: %d", "Открыть в Airlance", "Вступить в Airlance",
		"Ссылка недействительна", "Слишком много запросов, попробуйте через минуту", "Что-то пошло не так",
	},
	"uk": {
		"Учасників: %d", "Підписників: %d", "Відкрити в Airlance", "Приєднатися в Airlance",
		"Посилання недійсне", "Забагато запитів, спробуйте за хвилину", "Щось пішло не так",
	},
	"es": {
		"Miembros: %d", "Suscriptores: %d", "Abrir en Airlance", "Unirse en Airlance",
		"Este enlace no es válido", "Demasiadas solicitudes, inténtalo en un minuto", "Algo salió mal",
	},
	"it": {
		"Membri: %d", "Iscritti: %d", "Apri in Airlance", "Unisciti su Airlance",
		"Questo link non è valido", "Troppe richieste, riprova tra un minuto", "Qualcosa è andato storto",
	},
	"ro": {
		"Membri: %d", "Abonați: %d", "Deschide în Airlance", "Alătură-te în Airlance",
		"Acest link nu este valid", "Prea multe cereri, încearcă peste un minut", "Ceva nu a mers bine",
	},
	"fr": {
		"Membres : %d", "Abonnés : %d", "Ouvrir dans Airlance", "Rejoindre dans Airlance",
		"Ce lien n’est pas valide", "Trop de requêtes, réessayez dans une minute", "Une erreur est survenue",
	},
	"de": {
		"Mitglieder: %d", "Abonnenten: %d", "In Airlance öffnen", "In Airlance beitreten",
		"Dieser Link ist ungültig", "Zu viele Anfragen, versuche es in einer Minute erneut", "Etwas ist schiefgelaufen",
	},
}

func (l Links) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /c/{code}", func(w http.ResponseWriter, r *http.Request) {
		l.chatPage(w, r, "c")
	})
	mux.HandleFunc("GET /join/{code}", func(w http.ResponseWriter, r *http.Request) {
		l.chatPage(w, r, "join")
	})
	mux.HandleFunc("GET /u/{code}", l.userPage)
	mux.HandleFunc("GET /.well-known/apple-app-site-association", l.appleAssociation)
}

func (l Links) chatPage(w http.ResponseWriter, r *http.Request, path string) {
	lang, texts := language(r)
	code := r.PathValue("code")

	if !linkCode.MatchString(code) {
		l.failure(w, r, lang, texts, message.ErrChatNotFound)

		return
	}

	chat, err := l.Previews.PreviewChat(r.Context(), clientOf(r), code)
	if err != nil {
		l.failure(w, r, lang, texts, err)

		return
	}

	detail := texts.members
	button := texts.join

	if chat.Kind == message.KindChannel {
		detail = texts.subscribers
	}

	if path == "c" {
		button = texts.open
	}

	render(w, l.Logger, http.StatusOK, linkPage{
		Lang:   lang,
		Title:  chat.Title,
		Detail: strings.TrimSpace(chat.About + "\n" + fmt.Sprintf(detail, chat.MemberCount)),
		Open:   appLink(path, code),
		Button: button,
	})
}

func (l Links) userPage(w http.ResponseWriter, r *http.Request) {
	lang, texts := language(r)
	code := r.PathValue("code")

	if !linkCode.MatchString(code) {
		l.failure(w, r, lang, texts, directory.ErrNotFound)

		return
	}

	person, err := l.Previews.PreviewUser(r.Context(), clientOf(r), code)
	if err != nil {
		l.failure(w, r, lang, texts, err)

		return
	}

	render(w, l.Logger, http.StatusOK, linkPage{
		Lang:   lang,
		Title:  strings.TrimSpace(person.FirstName + " " + person.LastName),
		Detail: "@" + person.Username,
		Open:   appLink("u", code),
		Button: texts.open,
	})
}

func (l Links) failure(w http.ResponseWriter, r *http.Request, lang string, texts linkTexts, err error) {
	status, title := http.StatusInternalServerError, texts.generic

	switch {
	case errors.Is(err, message.ErrChatNotFound), errors.Is(err, directory.ErrNotFound):
		status, title = http.StatusNotFound, texts.invalid
	case errors.Is(err, directory.ErrTooManySearches):
		status, title = http.StatusTooManyRequests, texts.busy
	default:
		l.Logger.ErrorContext(r.Context(), "link page failed", slog.Any("error", err))
	}

	render(w, l.Logger, status, linkPage{Lang: lang, Title: title})
}

func (l Links) appleAssociation(w http.ResponseWriter, _ *http.Request) {
	if l.AppleAppID == "" {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	body, err := json.Marshal(map[string]any{
		"applinks": map[string]any{
			"details": []map[string]any{{
				"appIDs": []string{l.AppleAppID},
				"components": []map[string]string{
					{"/": "/c/*"}, {"/": "/join/*"}, {"/": "/u/*"},
				},
			}},
		},
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func appLink(path, code string) template.URL {
	return template.URL("airlance://link/" + path + "/" + code) //nolint:gosec // the code matched linkCode
}

func render(w http.ResponseWriter, logger *slog.Logger, status int, page linkPage) {
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Robots-Tag", "noindex")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	w.WriteHeader(status)

	if err := linkTemplate.Execute(w, page); err != nil {
		logger.Error("link page not written", slog.Any("error", err))
	}
}

func language(r *http.Request) (string, linkTexts) {
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		base, _, _ := strings.Cut(strings.ToLower(tag), "-")

		if texts, ok := linkLanguages[base]; ok {
			return base, texts
		}
	}

	return "en", linkLanguages["en"]
}

func clientOf(r *http.Request) string {
	if address := r.Header.Get("CF-Connecting-IP"); address != "" {
		return address
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
