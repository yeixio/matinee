package psremote

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Pairing needs the PSN account ID of the person playing. Sony doesn't
// show it anywhere, so, as other Remote Play apps do, Matinee has the
// person sign in to PlayStation on Sony's own page, the way Sony's Remote
// Play app does. The page ends on a "redirect" address with a one-time
// code; pasted back into Matinee, the code is traded for the account ID.
// Matinee keeps only the ID: never the password, and not the sign-in
// either, which it uses once and drops.

// The sign-in Sony's Remote Play app for Windows uses.
const (
	psnClientID     = "ba495a24-818c-472b-b12d-ff231c1b5745"
	psnClientSecret = "mvaiZkRsAsI1IBkY"
	psnRedirect     = "https://remoteplay.dl.playstation.net/remoteplay/redirect"
	psnAuthorize    = "https://auth.api.sonyentertainmentnetwork.com/2.0/oauth/authorize"
	psnToken        = "https://auth.api.sonyentertainmentnetwork.com/2.0/oauth/token"
)

// SignInURL is Sony's sign-in page.
func SignInURL() string {
	q := url.Values{}
	q.Set("service_entity", "urn:service-entity:psn")
	q.Set("response_type", "code")
	q.Set("client_id", psnClientID)
	q.Set("redirect_uri", psnRedirect)
	q.Set("scope", "psn:clientapp")
	q.Set("request_locale", "en_US")
	q.Set("ui", "pr")
	q.Set("service_logo", "ps")
	q.Set("layout_type", "popup")
	q.Set("smcid", "remoteplay")
	q.Set("prompt", "always")
	q.Set("PlatformPrivacyWs1", "minimal")
	return psnAuthorize + "?" + q.Encode()
}

// CodeFromRedirect finds the one-time code in the address Sony's page
// ended on.
func CodeFromRedirect(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return "", errors.New("paste the whole address from the address bar")
	}
	code := u.Query().Get("code")
	if code == "" {
		return "", errors.New("that address has no sign-in code: paste the address of the page that says \"redirect\"")
	}
	return code, nil
}

// SignIn trades the one-time code for the account ID, in hex.
func SignIn(ctx context.Context, client *http.Client, code string) (string, error) {
	client = cmp.Or(client, &http.Client{Timeout: 20 * time.Second})
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", psnRedirect)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, psnToken, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(psnClientID, psnClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := doJSON(client, req, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", errors.New("PlayStation's sign-in gave no token: sign in again")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, psnToken+"/"+url.PathEscape(tok.AccessToken), nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(psnClientID, psnClientSecret)
	var info struct {
		UserID string `json:"user_id"`
	}
	if err := doJSON(client, req, &info); err != nil {
		return "", err
	}
	id, err := strconv.ParseUint(info.UserID, 10, 64)
	if err != nil {
		return "", errors.New("PlayStation's sign-in didn't say who you are")
	}
	return AccountIDFromUserID(id), nil
}

func doJSON(client *http.Client, req *http.Request, v any) error {
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach PlayStation's sign-in: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest {
			return errors.New("PlayStation didn't take that sign-in: codes work once and only for a few minutes, so sign in again")
		}
		return fmt.Errorf("PlayStation's sign-in answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
