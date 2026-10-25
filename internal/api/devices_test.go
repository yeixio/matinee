package api

import (
	"bufio"
	"bytes"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// openapi.yaml documents exactly the routes the server has.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	var served []string
	for _, rt := range (&Server{}).routes() {
		served = append(served, rt.pattern)
	}
	pathLine := regexp.MustCompile(`^  (/\S*):$`)
	methodLine := regexp.MustCompile(`^    (get|post|put|delete|patch):`)
	var documented []string
	path := ""
	sc := bufio.NewScanner(bytes.NewReader(openAPI))
	for sc.Scan() {
		line := sc.Text()
		if line == "components:" {
			break
		}
		if m := pathLine.FindStringSubmatch(line); m != nil {
			path = m[1]
		} else if m := methodLine.FindStringSubmatch(line); m != nil && path != "" {
			documented = append(documented, strings.ToUpper(m[1])+" "+path)
		}
	}
	sort.Strings(served)
	sort.Strings(documented)
	if strings.Join(served, "\n") != strings.Join(documented, "\n") {
		t.Fatalf("openapi.yaml is out of date.\nserved:\n  %s\ndocumented:\n  %s",
			strings.Join(served, "\n  "), strings.Join(documented, "\n  "))
	}
}

func TestDeviceSignIn(t *testing.T) {
	admin, _ := newTestServer(t)
	var login struct{ Token string }
	if code := admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	admin.token = login.Token

	var info struct {
		Product    string
		APIVersion int
		NeedsSetup bool
	}
	if code := admin.do("GET", "/api/v1/server", nil, &info); code != 200 || info.Product != "matinee" || info.APIVersion != APIVersion || info.NeedsSetup {
		t.Fatalf("server info: %d %+v", code, info)
	}

	// The phone asks for a code without signing in.
	phone := &client{t: t, base: admin.base}
	if code := phone.do("POST", "/api/v1/auth/device", map[string]string{}, nil); code != http.StatusBadRequest {
		t.Fatalf("a device must name itself, got %d", code)
	}
	var start struct {
		DeviceCode, UserCode, VerificationURL, VerificationURLComplete string
		ExpiresIn, Interval                                            int
	}
	if code := phone.do("POST", "/api/v1/auth/device", map[string]string{"deviceName": "Mike's iPhone", "platform": "iOS"}, &start); code != 200 {
		t.Fatalf("start: %d", code)
	}
	if !regexp.MustCompile(`^[A-Z2-9]{4}-[A-Z2-9]{4}$`).MatchString(start.UserCode) || start.DeviceCode == "" ||
		!strings.HasSuffix(start.VerificationURL, "/link") || start.ExpiresIn <= 0 || start.Interval <= 0 {
		t.Fatalf("start: %+v", start)
	}

	poll := func() (int, string) {
		var res struct{ Status, Token string }
		code := phone.do("POST", "/api/v1/auth/device/token", map[string]string{"deviceCode": start.DeviceCode}, &res)
		return code, res.Token
	}
	if code, _ := poll(); code != http.StatusAccepted {
		t.Fatalf("before approval: %d", code)
	}

	// Signing in someone else's device needs a signed-in person.
	typed := strings.ToLower(strings.ReplaceAll(start.UserCode, "-", " "))
	if code := phone.do("POST", "/api/v1/auth/device/"+typed+"/approve", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("approve without signing in: %d", code)
	}
	var pending struct{ DeviceName, Platform string }
	if code := admin.do("GET", "/api/v1/auth/device/"+strings.ReplaceAll(typed, " ", "%20"), nil, &pending); code != 200 ||
		pending.DeviceName != "Mike's iPhone" || pending.Platform != "ios" {
		t.Fatalf("look up code: %d %+v", code, pending)
	}
	if code := admin.do("POST", "/api/v1/auth/device/NOPE-NOPE/approve", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown code: %d", code)
	}
	if code := admin.do("POST", "/api/v1/auth/device/"+start.UserCode+"/approve", nil, nil); code != http.StatusNoContent {
		t.Fatalf("approve: %d", code)
	}
	if code := admin.do("POST", "/api/v1/auth/device/"+start.UserCode+"/approve", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a code is approved once, got %d", code)
	}

	code, token := poll()
	if code != 200 || token == "" {
		t.Fatalf("after approval: %d %q", code, token)
	}
	if code, _ := poll(); code != http.StatusGone {
		t.Fatalf("a token is handed out once, got %d", code)
	}
	phone.token = token
	var me store.User
	if code := phone.do("GET", "/api/v1/me", nil, &me); code != 200 || me.Username != "admin" {
		t.Fatalf("phone signed in as %d %+v", code, me)
	}

	// The phone shows up among the admin's devices, and can be signed out from the browser.
	var devices []store.Session
	admin.do("GET", "/api/v1/devices", nil, &devices)
	if len(devices) != 2 {
		t.Fatalf("devices: %+v", devices)
	}
	var phoneID string
	for _, d := range devices {
		if d.DeviceName == "Mike's iPhone" {
			phoneID = d.ID
			if d.Current || d.Platform != "ios" {
				t.Fatalf("phone device: %+v", d)
			}
		} else if !d.Current {
			t.Fatalf("the asking device should be current: %+v", d)
		}
	}
	if code := admin.do("DELETE", "/api/v1/devices/"+phoneID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("sign out phone: %d", code)
	}
	if code := phone.do("GET", "/api/v1/me", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("signed-out phone still works: %d", code)
	}

	// A declined code tells the device so.
	phone.token = ""
	phone.do("POST", "/api/v1/auth/device", map[string]string{"deviceName": "Living room TV"}, &start)
	if code := admin.do("POST", "/api/v1/auth/device/"+start.UserCode+"/deny", nil, nil); code != http.StatusNoContent {
		t.Fatalf("deny: %d", code)
	}
	if code, _ := poll(); code != http.StatusForbidden {
		t.Fatalf("declined: %d", code)
	}
}

func TestPasswordLoginNamesDevice(t *testing.T) {
	c, _ := newTestServer(t)
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	var login struct{ Token string }
	body := map[string]string{"username": "admin", "password": "correct horse", "deviceName": "Pixel 9", "platform": "android"}
	if code := c.do("POST", "/api/v1/auth/login", body, &login); code != 200 {
		t.Fatalf("login: %d", code)
	}
	c.token = login.Token
	var devices []store.Session
	c.do("GET", "/api/v1/devices", nil, &devices)
	found := false
	for _, d := range devices {
		found = found || d.DeviceName == "Pixel 9" && d.Platform == "android" && d.Current
	}
	if !found {
		t.Fatalf("devices: %+v", devices)
	}
}
