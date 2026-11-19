// Package psremote plays a PlayStation 4 or 5 on the home network through
// Matinee, with Remote Play: matinee-psremote (games/psremote, built on
// chiaki-ng) pairs with the console and receives its picture and sound,
// and this package turns them into the RTP packets the game stream sends
// on to players (see gamestream.Remote). The stream stays between the
// console, the server and the player, on the home network: it never goes
// through Sony's servers or anyone else's. Only pairing talks to Sony,
// once, to learn the player's PSN account ID (psn.go).
package psremote

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Bridge runs matinee-psremote.
type Bridge struct {
	// Path is matinee-psremote's.
	Path string
}

// Available says whether this server has the bridge.
func (b *Bridge) Available() bool {
	if b == nil || b.Path == "" {
		return false
	}
	st, err := os.Stat(b.Path)
	return err == nil && !st.IsDir()
}

// Console is a paired PlayStation.
type Console struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Host is its address on the home network.
	Host string `json:"host"`
	PS5  bool   `json:"ps5"`
	MAC  string `json:"mac,omitempty"`
	// What pairing gave, which Remote Play needs: 32 hex digits each.
	RegistKey string `json:"registKey"`
	RPKey     string `json:"rpKey"`
	Target    int    `json:"target,omitempty"`
}

// Status is what a console says about itself.
type Status struct {
	// State is "ready" (on) or "standby" (in rest mode).
	State string `json:"state"`
	Name  string `json:"name"`
	PS5   bool   `json:"ps5"`
	// App is the game or app it's running, when it's on.
	App string `json:"app,omitempty"`
}

// ErrNoAnswer means no PlayStation answered at that address: it's off,
// unplugged, or the address is wrong.
var ErrNoAnswer = errors.New("no PlayStation answered at that address")

// AccountID turns a PSN account ID as people paste it (base64, as Sony's
// pages and other Remote Play apps show it, or 16 hex digits) into hex.
func AccountID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) == 16 {
		if _, err := hex.DecodeString(s); err == nil {
			return strings.ToLower(s), nil
		}
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != 8 {
		return "", errors.New("that isn't a PSN account ID: it's 12 letters and numbers ending in =")
	}
	return hex.EncodeToString(b), nil
}

// AccountIDFromUserID is the account ID of a PSN user id, the number
// Sony's sign-in gives: its 8 bytes, little-endian.
func AccountIDFromUserID(id uint64) string {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, id)
	return hex.EncodeToString(b)
}

// CheckHost says whether host is an address on the home network, so the
// server never sends Remote Play anywhere else.
func CheckHost(host string) error {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return errors.New("enter the console's IP address, such as 192.168.1.20")
	}
	if !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
		return errors.New("the console has to be on your home network")
	}
	return nil
}

func ps(c bool) string {
	if c {
		return "ps5"
	}
	return "ps4"
}

// run runs the bridge with args and the secrets in env, and decodes its
// JSON answer into v.
func (b *Bridge) run(ctx context.Context, timeout time.Duration, env []string, v any, args ...string) error {
	if !b.Available() {
		return errors.New("this server can't play PlayStation games")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.Path, args...)
	cmd.Env = append(os.Environ(), env...)
	var out, logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &logs
	runErr := cmd.Run()
	var answer struct {
		Error string `json:"error"`
	}
	line := bytes.TrimSpace(out.Bytes())
	if json.Unmarshal(line, &answer) == nil && answer.Error != "" {
		return errors.New(answer.Error)
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return errors.New("the console took too long to answer")
		}
		return fmt.Errorf("the PlayStation bridge failed: %v", runErr)
	}
	if v != nil {
		return json.Unmarshal(line, v)
	}
	return nil
}

// Status asks the console at host whether it's on.
func (b *Bridge) Status(ctx context.Context, host string) (Status, error) {
	var st Status
	if err := b.run(ctx, 5*time.Second, nil, &st, "status", host); err != nil {
		if strings.HasPrefix(err.Error(), "no PlayStation answered") {
			return st, ErrNoAnswer
		}
		return st, err
	}
	return st, nil
}

// Pair pairs with the console at host, with the code it shows under
// Settings → System → Remote Play → Pair Device (PS5), or Settings →
// Remote Play Connection Settings → Add Device (PS4). account is the
// PSN account ID, in hex (see AccountID).
func (b *Bridge) Pair(ctx context.Context, host string, ps5 bool, account, pin string) (Console, error) {
	var res struct {
		Name      string `json:"name"`
		MAC       string `json:"mac"`
		RegistKey string `json:"registKey"`
		RPKey     string `json:"rpKey"`
		Target    int    `json:"target"`
	}
	env := []string{"MATINEE_PS_ACCOUNT=" + account, "MATINEE_PS_PIN=" + strings.TrimSpace(pin)}
	if err := b.run(ctx, 45*time.Second, env, &res, "regist", host, ps(ps5)); err != nil {
		return Console{}, err
	}
	name := res.Name
	if name == "" {
		name = map[bool]string{true: "PS5", false: "PS4"}[ps5]
	}
	return Console{Name: name, Host: host, PS5: ps5, MAC: res.MAC, RegistKey: res.RegistKey, RPKey: res.RPKey, Target: res.Target}, nil
}

// Wake wakes the console from rest mode.
func (b *Bridge) Wake(ctx context.Context, c Console) error {
	return b.run(ctx, 10*time.Second, []string{"MATINEE_PS_REGIST_KEY=" + c.RegistKey}, nil, "wake", c.Host, ps(c.PS5))
}

// WakeAndWait wakes the console if it's resting and waits until it's on,
// which takes a PS5 about 20 seconds.
func (b *Bridge) WakeAndWait(ctx context.Context, c Console) error {
	st, err := b.Status(ctx, c.Host)
	if err == nil && st.State == "ready" {
		return nil
	}
	if err != nil && !errors.Is(err, ErrNoAnswer) {
		return err
	}
	if err := b.Wake(ctx, c); err != nil {
		return err
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		if st, err := b.Status(ctx, c.Host); err == nil && st.State == "ready" {
			// It answers a moment before Remote Play is ready.
			time.Sleep(3 * time.Second)
			return nil
		}
	}
	return errors.New("the PlayStation didn't wake up: check that \"Stay connected to the internet\" and \"Enable turning on PS5 from network\" are on in its rest mode settings")
}
