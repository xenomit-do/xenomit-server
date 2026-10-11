package webapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/services/admin"
	"github.com/xenomit/server/services/player"
	"github.com/xenomit/server/services/world"
)

func TestAdminGiveSpawnAndForbid(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accounts := player.New()
	accounts.Seed()
	log := events.NewLog()
	ctl := world.New(world.Deps{Log: log, Save: accounts, GrantItem: accounts.AddItem, Refresh: accounts.Snapshot})
	ctl.Start(ctx)
	ops := adminops.New(ctl, accounts, log)
	api := New(&Server{Accounts: accounts, World: ctl, Ops: ops, AdminToken: "secret"})
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	bobToken := registerAndLogin(t, srv.URL, "bob", "Bob")
	res := post(t, srv.URL+"/admin/give", bobToken, `{"userId":1,"credits":1}`)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("bob give status %d", res.StatusCode)
	}
	res.Body.Close()

	pilot := login(t, srv.URL, "pilot", "pilot")
	res = post(t, srv.URL+"/admin/give", pilot, `{"name":"Bob","credits":777,"uridium":3,"item":"lf-3"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("give %d %s", res.StatusCode, readBody(t, res))
	}
	res.Body.Close()
	hangar := get(t, srv.URL+"/hangar", bobToken)
	var snap model.Snapshot
	if err := json.NewDecoder(hangar.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	hangar.Body.Close()
	if snap.Credits != 10777 || snap.Uridium != 3 || snap.Damage != model.ShipByID(1).Damage+40 {
		t.Fatalf("hangar credits=%d uri=%d dmg=%d", snap.Credits, snap.Uridium, snap.Damage)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin/spawn", strings.NewReader(`{"mapId":1,"kind":"box","x":1500,"y":1500,"name":"Stash","loot":{"credits":4242,"uridium":9,"item":"lf-3"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var spawned struct {
		MapID  int
		NetIDs []int32
		Error  string
	}
	if err := json.NewDecoder(res.Body).Decode(&spawned); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(spawned.NetIDs) != 1 {
		t.Fatalf("spawn %d %+v", res.StatusCode, spawned)
	}
	mapRes := getToken(t, srv.URL+"/admin/maps/1", "secret")
	var inspected struct {
		Entities []model.EntityInfo
	}
	if err := json.NewDecoder(mapRes.Body).Decode(&inspected); err != nil {
		t.Fatal(err)
	}
	mapRes.Body.Close()
	var found bool
	for _, e := range inspected.Entities {
		if e.NetID == spawned.NetIDs[0] && e.Kind == "box" && e.Loot != nil && e.Loot.Credits == 4242 && e.Loot.Item == "lf-3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("spawned box missing: %+v", inspected.Entities)
	}
	res = postToken(t, srv.URL+"/admin/despawn", "secret", `{"mapId":1,"netId":`+strconv.FormatInt(int64(spawned.NetIDs[0]), 10)+`}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("despawn %d", res.StatusCode)
	}
	res.Body.Close()
}

func registerAndLogin(t *testing.T, base, user, pilot string) string {
	t.Helper()
	res, err := http.Post(base+"/register", "application/json", strings.NewReader(`{"username":"`+user+`","password":"`+user+`","pilotName":"`+pilot+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register %d", res.StatusCode)
	}
	return login(t, base, user, user)
}

func login(t *testing.T, base, user, password string) string {
	t.Helper()
	res, err := http.Post(base+"/login", "application/json", strings.NewReader(`{"username":"`+user+`","password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Token string
		Error string
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || body.Token == "" {
		t.Fatalf("login %d %s", res.StatusCode, body.Error)
	}
	return body.Token
}

func post(t *testing.T, url, token, jsonBody string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func postToken(t *testing.T, url, adminToken, jsonBody string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", adminToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func get(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func getToken(t *testing.T, url, adminToken string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Admin-Token", adminToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}
