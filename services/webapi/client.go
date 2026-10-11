package webapi

import (
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/protocol/policy"
	"github.com/xenomit/server/services/player"
	"github.com/xenomit/server/services/world"
)

func init() {
	_ = mime.AddExtensionType(".swf", "application/x-shockwave-flash")
}

func (s *Server) launcher(w http.ResponseWriter, r *http.Request) {
	pre, mainSWF := s.swfPresent()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := launcherHTML
	page = strings.ReplaceAll(page, "{{GAME}}", html.EscapeString(s.gameEndpoint(r)))
	page = strings.ReplaceAll(page, "{{CHAT}}", html.EscapeString(s.chatEndpoint(r)))
	page = strings.ReplaceAll(page, "{{PRE}}", presence(pre))
	page = strings.ReplaceAll(page, "{{MAIN}}", presence(mainSWF))
	page = strings.ReplaceAll(page, "{{ROOT}}", html.EscapeString(s.ClientRoot))
	_, _ = w.Write([]byte(page))
}

func (s *Server) mapPage(w http.ResponseWriter, r *http.Request) {
	snap, err := s.sessionFromRequest(r)
	if err != nil {
		s.launcher(w, r)
		return
	}
	origin := requestOrigin(r)
	vars := map[string]string{
		"lang":             "en",
		"userID":           fmt.Sprint(snap.UserID),
		"sessionID":        snap.SessionID,
		"factionID":        fmt.Sprint(snap.FactionID),
		"pid":              "1",
		"mapID":            fmt.Sprint(snap.MapID),
		"cdn":              origin + "/",
		"host":             origin + "/",
		"dynamicHost":      origin + "/",
		"chatHost":         s.chatHost(r),
		"basePath":         "spacemap",
		"gameclientPath":   "spacemap",
		"gameclientHash":   "xenomit",
		"resourcesXmlHash": "",
		"gameXmlHash":      "",
		"languageXmlHash":  "",
		"loadingClaim":     "LOADING",
		"display2d":        "2",
		"autoStartEnabled": "1",
		"allowChat":        "1",
		"useHash":          "0",
		"browser":          "Chrome",
	}
	raw, err := json.Marshal(vars)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	src := origin + "/spacemap/preloader.swf"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.ReplaceAll(mapHTML, "{{SRC}}", src)
	page = strings.ReplaceAll(page, "{{VARS}}", string(raw))
	_, _ = w.Write([]byte(page))
}

func (s *Server) loadingScreen(w http.ResponseWriter, r *http.Request) {
	snap, err := s.sessionFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"userID":    fmt.Sprint(snap.UserID),
		"sessionID": snap.SessionID,
		"factionID": fmt.Sprint(snap.FactionID),
		"mapID":     fmt.Sprint(snap.MapID),
		"lang":      "en",
	})
}

func (s *Server) mapsPHP(w http.ResponseWriter, r *http.Request) {
	addr := s.gameEndpoint(r)
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<maps>\n")
	for _, m := range world.DefaultMaps() {
		fmt.Fprintf(&b, "  <map id=\"%d\" name=\"%s\"><gameserverIP>%s</gameserverIP></map>\n",
			m.ID, xmlEscape(m.Name), xmlEscape(addr))
	}
	b.WriteString("</maps>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) crossdomain(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write([]byte(strings.TrimRight(policy.Response, "\x00")))
}

func (s *Server) spacemap(w http.ResponseWriter, r *http.Request) {
	if s.ClientRoot == "" {
		http.Error(w, "CLIENT_ROOT is not set", http.StatusNotFound)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/spacemap/")
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") {
		http.NotFound(w, r)
		return
	}
	root, err := filepath.Abs(s.ClientRoot)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	full, err = filepath.Abs(full)
	if err != nil || !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

func (s *Server) sessionFromRequest(r *http.Request) (model.Snapshot, error) {
	if c, err := r.Cookie("dosid"); err == nil && c.Value != "" {
		return s.Accounts.BySession(c.Value)
	}
	if sid := r.URL.Query().Get("sessionId"); sid != "" {
		return s.Accounts.BySession(sid)
	}
	return model.Snapshot{}, player.ErrSession
}

func (s *Server) swfPresent() (preloader, main bool) {
	if s.ClientRoot == "" {
		return false, false
	}
	_, err1 := os.Stat(filepath.Join(s.ClientRoot, "preloader.swf"))
	_, err2 := os.Stat(filepath.Join(s.ClientRoot, "main.swf"))
	return err1 == nil, err2 == nil
}

func (s *Server) gameEndpoint(r *http.Request) string {
	if s.GamePublic != "" {
		return s.GamePublic
	}
	return net.JoinHostPort(s.chatHost(r), s.portOr(s.GamePort, "8080"))
}

func (s *Server) chatEndpoint(r *http.Request) string {
	return net.JoinHostPort(s.chatHost(r), s.portOr(s.ChatPort, "9338"))
}

func (s *Server) chatHost(r *http.Request) string {
	host := r.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if name, _, err := net.SplitHostPort(host); err == nil {
		return name
	}
	return host
}

func (s *Server) portOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:8088"
	}
	return scheme + "://" + host
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func presence(ok bool) string {
	if ok {
		return "present"
	}
	return "missing"
}

const launcherHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>xenomit</title>
<style>
  body { margin: 0; background: #07090d; color: #d7deea; font: 16px/1.45 "Segoe UI", sans-serif; }
  main { max-width: 40rem; margin: 4rem auto; padding: 0 1.25rem; }
  h1 { font-weight: 560; letter-spacing: 0.04em; }
  form { display: grid; gap: 0.75rem; margin: 1.5rem 0; }
  input { background: #10151c; color: inherit; border: 1px solid #2a3544; padding: 0.6rem 0.7rem; }
  button { background: #d7deea; color: #07090d; border: 0; padding: 0.65rem 0.8rem; cursor: pointer; }
  .meta { color: #8ea0b8; }
  .err { color: #ffb4a8; min-height: 1.2em; }
</style>
</head>
<body>
<main>
  <h1>xenomit</h1>
  <p class="meta">Game socket {{GAME}} · chat {{CHAT}}</p>
  <p class="meta">preloader.swf {{PRE}} · main.swf {{MAIN}} · directory {{ROOT}}</p>
  <form id="login">
    <input name="username" placeholder="username" autocomplete="username" required>
    <input name="password" type="password" placeholder="password" autocomplete="current-password" required>
    <button type="submit">Play</button>
  </form>
  <p class="err" id="err"></p>
  <p class="meta">Seed account is pilot / pilot. After login the map page embeds spacemap/preloader.swf, which loads spacemap/main.swf.</p>
</main>
<script>
document.getElementById("login").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const err = document.getElementById("err");
  err.textContent = "";
  const body = Object.fromEntries(new FormData(ev.target));
  const login = await fetch("/login", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)});
  const sessionIn = await login.json();
  if (!login.ok) { err.textContent = sessionIn.error || "login failed"; return; }
  const opened = await fetch("/session", {method: "POST", headers: {"Authorization": "Bearer " + sessionIn.token}});
  const session = await opened.json();
  if (!opened.ok) { err.textContent = session.error || "session failed"; return; }
  location.href = "/indexInternal.es?action=internalMapRevolution";
});
</script>
</body>
</html>
`

const mapHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>xenomit</title>
<style>html,body,#container{margin:0;height:100%;background:#000;}</style>
</head>
<body>
<div id="container"></div>
<script>
function flashembed(id, opts, vars) {
  var el = document.getElementById(id);
  var q = new URLSearchParams(vars).toString();
  var embed = document.createElement("embed");
  embed.setAttribute("src", opts.src);
  embed.setAttribute("width", "100%");
  embed.setAttribute("height", "100%");
  embed.setAttribute("type", "application/x-shockwave-flash");
  embed.setAttribute("flashvars", q);
  embed.setAttribute("wmode", "direct");
  el.appendChild(embed);
}
flashembed("container", {"src": "{{SRC}}", "width": "100%", "height": "100%", "id": "preloader", "wmode": "direct", "bgcolor": "#000000"}, {{VARS}});
</script>
</body>
</html>
`
