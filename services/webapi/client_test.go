package webapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xenomit/server/services/player"
)

func TestMapPageServesPreloaderContract(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "preloader.swf"), []byte("FWS-pre"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.swf"), []byte("FWS-main"), 0o644); err != nil {
		t.Fatal(err)
	}
	accounts := player.New()
	accounts.Seed()
	api := New(&Server{Accounts: accounts, ClientRoot: dir, GamePort: "8080", ChatPort: "9338"})
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	token := loginToken(t, srv.URL)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/session", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var dosid string
	for _, c := range res.Cookies() {
		if c.Name == "dosid" {
			dosid = c.Value
		}
	}
	if dosid == "" {
		t.Fatal("session did not set dosid")
	}

	pageReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/indexInternal.es?action=internalMapRevolution", nil)
	pageReq.AddCookie(&http.Cookie{Name: "dosid", Value: dosid})
	page, err := http.DefaultClient.Do(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, page)
	if !strings.Contains(body, `flashembed("container"`) || !strings.Contains(body, "/spacemap/preloader.swf") {
		t.Fatalf("map page missing preloader embed: %s", body)
	}
	if !strings.Contains(body, `"userID":"1"`) || !strings.Contains(body, dosid) {
		t.Fatalf("map page missing session contract: %s", body)
	}

	swf, err := http.Get(srv.URL + "/spacemap/preloader.swf")
	if err != nil {
		t.Fatal(err)
	}
	if swf.StatusCode != 200 || readAll(t, swf) != "FWS-pre" {
		t.Fatalf("preloader status %d", swf.StatusCode)
	}
	mainSWF, err := http.Get(srv.URL + "/spacemap/main.swf?__cv=xenomit")
	if err != nil {
		t.Fatal(err)
	}
	if mainSWF.StatusCode != 200 || readAll(t, mainSWF) != "FWS-main" {
		t.Fatalf("main.swf status %d", mainSWF.StatusCode)
	}

	maps, err := http.Get(srv.URL + "/spacemap/xml/maps.php")
	if err != nil {
		t.Fatal(err)
	}
	xml := readAll(t, maps)
	if !strings.Contains(xml, `<map id="1"`) || !strings.Contains(xml, `<gameserverIP>127.0.0.1:8080</gameserverIP>`) {
		t.Fatalf("maps.php: %s", xml)
	}

	screen, _ := http.NewRequest(http.MethodPost, srv.URL+"/flashAPI/loadingScreen.php", strings.NewReader("action=setClientBrowserConfig"))
	screen.AddCookie(&http.Cookie{Name: "dosid", Value: dosid})
	screen.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loaded, err := http.DefaultClient.Do(screen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAll(t, loaded), `"sessionID":"`+dosid+`"`) {
		t.Fatal("loading screen did not return the session")
	}
}

func loginToken(t *testing.T, base string) string {
	t.Helper()
	res, err := http.Post(base+"/login", "application/json", strings.NewReader(`{"username":"pilot","password":"pilot"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct{ Token string }
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" {
		t.Fatal("no token")
	}
	return body.Token
}

func readAll(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
