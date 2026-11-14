package gamehost

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

	"github.com/coder/websocket"
)

// How Matinee talks to a game host, over HTTP with the host's token as a
// bearer token:
//
//	GET /v1/games              the installed games: {"games": [Game, …]}
//	GET /v1/play/{appId}?width=&height=&fps=
//	                           a WebSocket that plays the game until either
//	                           side closes it
//
// On the play socket the host first sends a Hello as text, then binary
// messages of a kind byte and an RTP packet, as the direct stream does:
// KindVideo (H.264, packetization mode 1, keyframes with their SPS and
// PPS) and KindAudio (Opus, 48 kHz stereo). Matinee sends the player's
// input messages as they come (1 port lo hi: buttons, 3 port axis v: a
// stick; see gamestream.Session.input; 4 to 8: the keyboard and mouse,
// see kbm.go). Closing the socket ends the game.

const (
	KindVideo = 0x10
	KindAudio = 0x11
)

// Hello is the host's first message on the play socket: the picture it
// sends.
type Hello struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	FPS    float64 `json:"fps"`
	// Encoder is what encodes the picture, such as h264_vaapi, for the
	// server's logs.
	Encoder string `json:"encoder,omitempty"`
}

// Client is Matinee's side of a game host.
type Client struct {
	// URL is the host's address, such as http://steam:7174.
	URL   string
	Token string
	HTTP  *http.Client
}

// ErrBusy means the host is already playing a game.
var ErrBusy = errors.New("the game host is already playing a game")

func (c *Client) url(path string) string {
	return strings.TrimRight(c.URL, "/") + path
}

func (c *Client) header() http.Header {
	h := http.Header{}
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+c.Token)
	}
	return h
}

// Games lists the games installed on the host.
func (c *Client) Games(ctx context.Context) ([]Game, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/v1/games"), nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.header()
	resp, err := cmp.Or(c.HTTP, http.DefaultClient).Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't reach the game host: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the game host answered %s", resp.Status)
	}
	var body struct {
		Games []Game `json:"games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Games, nil
}

// Play starts a game on the host and returns its socket and the picture
// it sends. The caller closes the socket to end the game.
func (c *Client) Play(ctx context.Context, appID, width, height int, fps float64) (*websocket.Conn, Hello, error) {
	q := url.Values{}
	q.Set("width", strconv.Itoa(width))
	q.Set("height", strconv.Itoa(height))
	q.Set("fps", strconv.FormatFloat(fps, 'f', -1, 64))
	u := c.url("/v1/play/"+strconv.Itoa(appID)) + "?" + q.Encode()
	u = "ws" + strings.TrimPrefix(u, "http")
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPHeader: c.header(), HTTPClient: c.HTTP})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusConflict {
			return nil, Hello{}, ErrBusy
		}
		if resp != nil {
			return nil, Hello{}, fmt.Errorf("the game host answered %s", resp.Status)
		}
		return nil, Hello{}, fmt.Errorf("can't reach the game host: %w", err)
	}
	conn.SetReadLimit(1 << 20)
	// The host sends its Hello once the game and its capture have started,
	// which can take a while for a big game.
	hctx, hcancel := context.WithTimeout(ctx, 90*time.Second)
	defer hcancel()
	typ, b, err := conn.Read(hctx)
	if err != nil {
		conn.CloseNow()
		return nil, Hello{}, fmt.Errorf("the game host didn't start the game: %w", err)
	}
	var h Hello
	if typ != websocket.MessageText || json.Unmarshal(b, &h) != nil || h.Width <= 0 || h.Height <= 0 {
		conn.CloseNow()
		return nil, Hello{}, errors.New("the game host sent something unexpected")
	}
	return conn, h, nil
}
