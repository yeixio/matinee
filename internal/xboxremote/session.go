package xboxremote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Console is an Xbox signed in to the account, with remote play on.
type Console struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type is "XboxSeriesX", "XboxSeriesS", "XboxOne", "XboxOneS" or
	// "XboxOneX".
	Type string `json:"type"`
	// State is "ready" (on), "standby" (sleep, which remote play wakes)
	// or "off".
	State string `json:"state"`
}

// deviceInfo says what's playing, which decides the picture: Windows
// gets 1080p.
const deviceInfo = `{"appInfo":{"env":{"clientAppId":"www.xbox.com","clientAppType":"browser","clientAppVersion":"21.1.98","clientSdkVersion":"8.5.3","httpEnvironment":"prod","sdkInstallId":""}},` +
	`"dev":{"hw":{"make":"Microsoft","model":"unknown","sdktype":"web"},"os":{"name":"windows","ver":"22631.2715","platform":"desktop"},` +
	`"displayInfo":{"dimensions":{"widthInPixels":1920,"heightInPixels":1080},"pixelDensity":{"dpiX":2,"dpiY":2}},"browser":{"browserName":"chrome","browserVersion":"119.0"}}}`

// gssv calls the remote play servers. A 204 (nothing yet) returns
// errNotYet.
func (c *Client) gssv(ctx context.Context, t *Tokens, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, t.Base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gssv-Client", "XboxComBrowser")
	req.Header.Set("X-MS-Device-Info", deviceInfo)
	req.Header.Set("Authorization", "Bearer "+t.GS)
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Xbox remote play: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	switch {
	case res.StatusCode == http.StatusNoContent:
		return errNotYet
	case res.StatusCode == http.StatusUnauthorized:
		return ErrSignedOut
	case res.StatusCode >= 300:
		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &e) == nil && e.Message != "" {
			return fmt.Errorf("Xbox remote play: %s", e.Message)
		}
		return fmt.Errorf("Xbox remote play answered %d", res.StatusCode)
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

var errNotYet = errors.New("not yet")

// Consoles lists the account's consoles that remote play can reach.
func (c *Client) Consoles(ctx context.Context, t *Tokens) ([]Console, error) {
	var res struct {
		Results []struct {
			DeviceName  string `json:"deviceName"`
			ServerID    string `json:"serverId"`
			PowerState  string `json:"powerState"`
			ConsoleType string `json:"consoleType"`
		} `json:"results"`
	}
	if err := c.gssv(ctx, t, "GET", "/v6/servers/home", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Console, 0, len(res.Results))
	for _, r := range res.Results {
		state := "off"
		switch r.PowerState {
		case "On":
			state = "ready"
		case "ConnectedStandby":
			state = "standby"
		}
		name := r.DeviceName
		if name == "" {
			name = "Xbox"
		}
		out = append(out, Console{ID: r.ServerID, Name: name, Type: r.ConsoleType, State: state})
	}
	return out, nil
}

// session is remote play being set up on a console, through Microsoft's
// servers: they start it, then pass the offer and addresses along.
type session struct {
	c    *Client
	t    *Tokens
	path string // "v5/sessions/home/<id>"
}

// registerWait is how long to wait before asking again when Microsoft
// can't reach the console yet; a var so tests can shorten it.
var registerWait = 10 * time.Second

// startSession asks the console to start remote play, signs it in, and
// waits until it's ready for the stream. A console that has just woken
// or restarted can take a little while to check in with Microsoft, which
// fails the start with WaitingForServerToRegister, so that is asked again
// a few times before giving up.
func (c *Client) startSession(ctx context.Context, t *Tokens, consoleID string) (*session, error) {
	for try := 1; ; try++ {
		s, err := c.startSessionOnce(ctx, t, consoleID)
		var nr notRegistered
		if !errors.As(err, &nr) {
			return s, err
		}
		if try == 4 {
			return nil, errors.New("the Xbox didn't start remote play: Microsoft can't reach the console. Restart the Xbox (Settings, General, Power options, Restart now), wait until it's signed in, and check that remote features are on in Settings, Devices and connections, Remote features")
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(registerWait):
		}
	}
}

// notRegistered is a start that failed because the console isn't checked
// in with Microsoft's servers (yet).
type notRegistered struct{ error }

func (c *Client) startSessionOnce(ctx context.Context, t *Tokens, consoleID string) (*session, error) {
	var res struct {
		SessionPath string `json:"sessionPath"`
	}
	err := c.gssv(ctx, t, "POST", "/v5/sessions/home/play", map[string]any{
		"clientSessionId": "", "titleId": "", "systemUpdateGroup": "", "serverId": consoleID, "fallbackRegionNames": []string{},
		"settings": map[string]any{"nanoVersion": "V3;WebrtcTransport.dll", "enableOptionalDataCollection": false, "enableTextToSpeech": false,
			"highContrast": 0, "locale": "en-US", "useIceConnection": false, "timezoneOffsetMinutes": 0, "sdkType": "web", "osName": "windows"},
	}, &res)
	if err != nil {
		return nil, err
	}
	if res.SessionPath == "" {
		return nil, errors.New("Xbox remote play didn't start a session")
	}
	s := &session{c: c, t: t, path: "/" + strings.TrimLeft(res.SessionPath, "/")}
	connected := false
	for {
		var st struct {
			State        string `json:"state"`
			ErrorDetails struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"errorDetails"`
		}
		if err := c.gssv(ctx, t, "GET", s.path+"/state", nil, &st); err != nil && !errors.Is(err, errNotYet) {
			s.stop()
			return nil, err
		}
		switch st.State {
		case "Provisioned":
			return s, nil
		case "ReadyToConnect":
			if !connected {
				tok, err := c.transferToken(ctx, t.Refresh)
				if err == nil {
					err = c.gssv(ctx, t, "POST", s.path+"/connect", map[string]string{"userToken": tok}, nil)
				}
				if err != nil {
					s.stop()
					return nil, err
				}
				connected = true
			}
		case "Failed":
			s.stop()
			err := consoleError(st.ErrorDetails.Code, st.ErrorDetails.Message)
			if strings.Contains(st.ErrorDetails.Message, "WaitingForServerToRegister") {
				err = notRegistered{err}
			}
			return nil, err
		}
		select {
		case <-ctx.Done():
			s.stop()
			return nil, errors.New("the Xbox didn't start remote play: is it on, or in Sleep with remote play turned on?")
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// consoleError says why a console didn't start, as people need to hear
// it.
func consoleError(code, message string) error {
	if message != "" {
		return errors.New("the Xbox didn't start remote play: " + message)
	}
	if code != "" {
		return errors.New("the Xbox didn't start remote play (" + code + "): check that it's on or in Sleep, and that remote features are on in its settings")
	}
	return errors.New("the Xbox didn't start remote play: check that it's on or in Sleep, and that remote features are on in its settings")
}

// exchange posts body to the session's path and waits for the console's
// answer, which comes as a JSON string in exchangeResponse.
func (s *session) exchange(ctx context.Context, path string, body any) (string, error) {
	if err := s.c.gssv(ctx, s.t, "POST", s.path+path, body, nil); err != nil {
		return "", err
	}
	for {
		var res struct {
			ExchangeResponse string `json:"exchangeResponse"`
		}
		err := s.c.gssv(ctx, s.t, "GET", s.path+path, nil, &res)
		if err == nil && res.ExchangeResponse != "" {
			return res.ExchangeResponse, nil
		}
		if err != nil && !errors.Is(err, errNotYet) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", errors.New("the Xbox didn't answer in time")
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// offerConfig is what this player can do, sent with the offer.
var offerConfig = map[string]any{
	"chatConfiguration": map[string]any{"bytesPerSample": 2, "expectedClipDurationMs": 20,
		"format": map[string]string{"codec": "opus", "container": "webm"}, "numChannels": 1, "sampleFrequencyHz": 24000},
	"chat":            map[string]int{"minVersion": 1, "maxVersion": 1},
	"control":         map[string]int{"minVersion": 1, "maxVersion": 3},
	"input":           map[string]int{"minVersion": 1, "maxVersion": 9},
	"message":         map[string]int{"minVersion": 1, "maxVersion": 1},
	"reliableinput":   map[string]int{"minVersion": 9, "maxVersion": 9},
	"unreliableinput": map[string]int{"minVersion": 9, "maxVersion": 9},
}

// answer sends the offer and returns the console's answer.
func (s *session) answer(ctx context.Context, offer string) (string, error) {
	raw, err := s.exchange(ctx, "/sdp", map[string]any{"messageType": "offer", "sdp": offer, "requestId": "1", "configuration": offerConfig})
	if err != nil {
		return "", err
	}
	var a struct {
		SDP    string `json:"sdp"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil || a.SDP == "" {
		return "", errors.New("the Xbox turned down the stream")
	}
	return a.SDP, nil
}

// remoteCandidate is an address the console can be reached at.
type remoteCandidate struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex any    `json:"sdpMLineIndex"`
}

// candidates sends this server's addresses (each a JSON string, as the
// console takes them) and returns the console's.
func (s *session) candidates(ctx context.Context, ours []string) ([]remoteCandidate, error) {
	raw, err := s.exchange(ctx, "/ice", map[string]any{"candidates": ours})
	if err != nil {
		return nil, err
	}
	var theirs []remoteCandidate
	if err := json.Unmarshal([]byte(raw), &theirs); err != nil {
		return nil, errors.New("the Xbox sent addresses Matinee can't read")
	}
	return theirs, nil
}

func (s *session) keepalive(ctx context.Context) error {
	return s.c.gssv(ctx, s.t, "POST", s.path+"/keepalive", map[string]any{}, nil)
}

// stop ends the session on the console.
func (s *session) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.c.gssv(ctx, s.t, "DELETE", s.path, nil, nil)
}
