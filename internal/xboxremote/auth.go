// Package xboxremote plays an Xbox Series X|S or Xbox One at home through
// Matinee, with the console's remote play: Microsoft's servers start the
// session and swap the addresses, then the console streams its picture and
// sound straight to this server over WebRTC on the home network, and this
// package turns them into the RTP packets the game stream sends on to
// players (see gamestream.Remote). The controller goes back the same way.
//
// Signing in is Microsoft's own device sign-in: the admin opens
// microsoft.com/link and types the code Matinee shows. Matinee keeps the
// sign-in's refresh token, the only thing that lets it start remote play
// again later, on the server; never the password.
//
// The protocol is as Greenlight and xbox-xcloud-player (both MIT, by
// UnknownSKL) speak it: xbox.com's own player's.
package xboxremote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The sign-in xbox.com's own player uses.
const (
	clientID      = "1f907974-e22b-4810-a9de-d9647380c97e"
	signInScope   = "xboxlive.signin openid profile offline_access"
	transferScope = "service::http://Passport.NET/purpose::PURPOSE_XBOX_CLOUD_CONSOLE_TRANSFER_TOKEN"
)

// Endpoints are Microsoft's addresses, which tests replace.
type Endpoints struct {
	Login    string // login.microsoftonline.com
	Live     string // login.live.com
	UserAuth string // user.auth.xboxlive.com
	XSTS     string // xsts.auth.xboxlive.com
	XHome    string // xhome.gssv-play-prod.xboxlive.com
	XCCS     string // xccs.xboxlive.com, which turns consoles on
}

// Microsoft is where Xbox Live is.
var Microsoft = Endpoints{
	Login:    "https://login.microsoftonline.com",
	Live:     "https://login.live.com",
	UserAuth: "https://user.auth.xboxlive.com",
	XSTS:     "https://xsts.auth.xboxlive.com",
	XHome:    "https://xhome.gssv-play-prod.xboxlive.com",
	XCCS:     "https://xccs.xboxlive.com",
}

// Client talks to Xbox Live.
type Client struct {
	HTTP *http.Client
	End  Endpoints

	mu    sync.Mutex
	cache map[string]*Tokens // by refresh token
}

// New is a client of Microsoft's servers.
func New() *Client { return &Client{End: Microsoft} }

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// DeviceCode is a sign-in waiting for the admin to type Code at URL.
type DeviceCode struct {
	Code     string    `json:"code"`
	URL      string    `json:"url"`
	Expires  time.Time `json:"expires"`
	device   string
	interval time.Duration
}

// StartSignIn asks Microsoft for a sign-in code.
func (c *Client) StartSignIn(ctx context.Context) (*DeviceCode, error) {
	var res struct {
		UserCode        string `json:"user_code"`
		DeviceCode      string `json:"device_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	form := url.Values{"client_id": {clientID}, "scope": {signInScope}}
	if err := c.postForm(ctx, c.End.Login+"/consumers/oauth2/v2.0/devicecode", form, &res); err != nil {
		return nil, err
	}
	if res.UserCode == "" || res.DeviceCode == "" {
		return nil, errors.New("Microsoft didn't give a sign-in code")
	}
	d := &DeviceCode{Code: res.UserCode, URL: res.VerificationURI, device: res.DeviceCode,
		Expires: time.Now().Add(time.Duration(res.ExpiresIn) * time.Second), interval: time.Duration(res.Interval) * time.Second}
	if d.URL == "" {
		d.URL = "https://www.microsoft.com/link"
	}
	if d.interval <= 0 {
		d.interval = 5 * time.Second
	}
	return d, nil
}

// ErrSignInDeclined means the admin said no on Microsoft's page, or took
// too long.
var ErrSignInDeclined = errors.New("the Microsoft sign-in was declined or ran out of time")

// WaitSignIn waits for the admin to finish d on Microsoft's page, and
// returns the refresh token to keep.
func (c *Client) WaitSignIn(ctx context.Context, d *DeviceCode) (string, error) {
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {clientID}, "device_code": {d.device}}
	interval := d.interval
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(d.Expires) {
			return "", ErrSignInDeclined
		}
		var res tokenAnswer
		err := c.postForm(ctx, c.End.Login+"/consumers/oauth2/v2.0/token", form, &res)
		var oe *oauthError
		switch {
		case err == nil && res.RefreshToken != "":
			return res.RefreshToken, nil
		case errors.As(err, &oe) && oe.Code == "authorization_pending":
		case errors.As(err, &oe) && oe.Code == "slow_down":
			interval += 5 * time.Second
		case errors.As(err, &oe):
			return "", ErrSignInDeclined
		case err != nil:
			return "", err
		default:
			return "", errors.New("Microsoft's sign-in didn't give Matinee a way to stay signed in")
		}
	}
}

type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Tokens are what playing needs, from a refresh token.
type Tokens struct {
	// Refresh is the refresh token to keep from now on: Microsoft may
	// hand out a new one each time.
	Refresh  string
	Gamertag string
	// Live is the Xbox Live authorization ("XBL3.0 x=…"), for turning
	// consoles on; empty when Microsoft didn't give one.
	Live string
	// GS is the remote play token, for Base, the remote play servers
	// nearest the account.
	GS      string
	Base    string
	expires time.Time
}

// ErrSignedOut means the sign-in no longer works: Microsoft ended it, or
// the account's password changed. The admin signs in again.
var ErrSignedOut = errors.New("the Xbox sign-in has ended: sign in to Xbox again")

// Tokens signs in with refresh: kept for as long as Microsoft allows.
func (c *Client) Tokens(ctx context.Context, refresh string) (*Tokens, error) {
	c.mu.Lock()
	t := c.cache[refresh]
	c.mu.Unlock()
	if t != nil && time.Until(t.expires) > 5*time.Minute {
		return t, nil
	}
	var tok tokenAnswer
	form := url.Values{"client_id": {clientID}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}, "scope": {signInScope}}
	if err := c.postForm(ctx, c.End.Login+"/consumers/oauth2/v2.0/token", form, &tok); err != nil {
		var oe *oauthError
		if errors.As(err, &oe) {
			return nil, ErrSignedOut
		}
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refresh
	}

	var user xblToken
	err := c.postJSON(ctx, c.End.UserAuth+"/user/authenticate", map[string]any{
		"Properties":   map[string]any{"AuthMethod": "RPS", "RpsTicket": "d=" + tok.AccessToken, "SiteName": "user.auth.xboxlive.com"},
		"RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT",
	}, xblHeaders, &user)
	if err != nil {
		return nil, err
	}
	gamertag, liveAuth := "", ""
	var live xblToken
	if c.xsts(ctx, user.Token, "http://xboxlive.com", &live) == nil && len(live.DisplayClaims.XUI) > 0 {
		gamertag = live.DisplayClaims.XUI[0].Gamertag
		liveAuth = "XBL3.0 x=" + live.DisplayClaims.XUI[0].UserHash + ";" + live.Token
	}
	var gssv xblToken
	if err := c.xsts(ctx, user.Token, "http://gssv.xboxlive.com/", &gssv); err != nil {
		return nil, err
	}

	var home struct {
		GSToken           string `json:"gsToken"`
		DurationInSeconds int    `json:"durationInSeconds"`
		OfferingSettings  struct {
			Regions []struct {
				BaseURI   string `json:"baseUri"`
				IsDefault bool   `json:"isDefault"`
			} `json:"regions"`
		} `json:"offeringSettings"`
	}
	err = c.postJSON(ctx, c.End.XHome+"/v2/login/user", map[string]string{"token": gssv.Token, "offeringId": "xhome"},
		map[string]string{"x-gssv-client": "XboxComBrowser"}, &home)
	if err != nil {
		return nil, fmt.Errorf("Xbox remote play wouldn't sign in: %w", err)
	}
	base := ""
	for _, r := range home.OfferingSettings.Regions {
		if base == "" || r.IsDefault {
			base = r.BaseURI
		}
	}
	if home.GSToken == "" || base == "" {
		return nil, errors.New("Xbox remote play isn't offered for this account")
	}
	dur := time.Duration(home.DurationInSeconds) * time.Second
	if dur <= 0 {
		dur = time.Hour
	}
	t = &Tokens{Refresh: tok.RefreshToken, Gamertag: gamertag, Live: liveAuth, GS: home.GSToken, Base: strings.TrimRight(base, "/"), expires: time.Now().Add(dur)}
	c.mu.Lock()
	if c.cache == nil {
		c.cache = map[string]*Tokens{}
	}
	delete(c.cache, refresh)
	c.cache[t.Refresh] = t
	c.mu.Unlock()
	return t, nil
}

// transferToken is what the console takes to know who's playing.
func (c *Client) transferToken(ctx context.Context, refresh string) (string, error) {
	var tok tokenAnswer
	form := url.Values{"client_id": {clientID}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}, "scope": {transferScope}}
	if err := c.postForm(ctx, c.End.Live+"/oauth20_token.srf", form, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", errors.New("Microsoft wouldn't let the console sign in")
	}
	return tok.AccessToken, nil
}

type xblToken struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		XUI []struct {
			Gamertag string `json:"gtg"`
			UserHash string `json:"uhs"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

var xblHeaders = map[string]string{"x-xbl-contract-version": "1"}

func (c *Client) xsts(ctx context.Context, user, party string, out *xblToken) error {
	err := c.postJSON(ctx, c.End.XSTS+"/xsts/authorize", map[string]any{
		"Properties":   map[string]any{"SandboxId": "RETAIL", "UserTokens": []string{user}},
		"RelyingParty": party, "TokenType": "JWT",
	}, xblHeaders, out)
	var he *httpError
	if errors.As(err, &he) && he.Status == http.StatusUnauthorized {
		// XErr 2148916233: the Microsoft account has no Xbox profile.
		if strings.Contains(he.Body, "2148916233") {
			return errors.New("that Microsoft account has no Xbox profile yet: sign in on the Xbox once first")
		}
		if strings.Contains(he.Body, "2148916238") {
			return errors.New("that's a child account: an adult in its Microsoft family has to allow it first")
		}
	}
	return err
}

type oauthError struct {
	Code string `json:"error"`
	Desc string `json:"error_description"`
}

func (e *oauthError) Error() string { return "Microsoft sign-in: " + e.Code }

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("Microsoft answered %d", e.Status) }

func (c *Client) postForm(ctx context.Context, u string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, out)
}

func (c *Client) postJSON(ctx context.Context, u string, body any, headers map[string]string, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Microsoft: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var oe oauthError
		if json.Unmarshal(b, &oe) == nil && oe.Code != "" {
			return &oe
		}
		return &httpError{Status: res.StatusCode, Body: string(b)}
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}
