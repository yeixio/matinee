// Package pcremote plays a gaming PC on the home network through Matinee:
// the PC runs Sunshine (https://github.com/LizardByte/Sunshine), the free
// game streaming host for Windows, Linux and macOS, and Matinee is its
// client, as the Moonlight apps are. This package pairs with the PC and
// starts Steam's Big Picture there over Sunshine's HTTPS API, and
// matinee-pcremote (games/pcremote, built on moonlight-common-c) receives
// the picture and sound, which go on to players through the game stream
// (see gamestream.Remote). Everything stays on the home network.
//
// It's for a PC someone already plays Steam on. A server can instead have
// a game host of its own (internal/gamehost), such as a Steam container.
package pcremote

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is Sunshine's HTTP port; its HTTPS port is 5 below it, and
// its own web page 1 above.
const DefaultPort = 47989

// Identity is Matinee's own certificate, which Sunshine knows it by once
// paired.
type Identity struct {
	Cert string `json:"cert"` // PEM
	Key  string `json:"key"`  // PEM
	// UniqueID is sent with each request, as Moonlight does.
	UniqueID string `json:"uniqueId"`
}

// NewIdentity makes a new certificate.
func NewIdentity() (Identity, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return Identity{}, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Matinee"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(20, 0, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return Identity{}, err
	}
	id := make([]byte, 8)
	rand.Read(id)
	return Identity{
		Cert:     string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		Key:      string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: must(x509.MarshalPKCS8PrivateKey(key))})),
		UniqueID: strings.ToUpper(hex.EncodeToString(id)),
	}, nil
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func (id Identity) parse() (*x509.Certificate, *rsa.PrivateKey, error) {
	cb, _ := pem.Decode([]byte(id.Cert))
	kb, _ := pem.Decode([]byte(id.Key))
	if cb == nil || kb == nil {
		return nil, nil, errors.New("Matinee's certificate is damaged: remove the PC and pair it again")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("Matinee's key isn't RSA")
	}
	return cert, key, nil
}

// PC is a paired gaming PC.
type PC struct {
	Name string `json:"name"`
	// Host is its address on the home network, and Port Sunshine's HTTP
	// port there.
	Host string `json:"host"`
	Port int    `json:"port"`
	// HTTPSPort is Sunshine's HTTPS port, which it said when paired.
	HTTPSPort int `json:"httpsPort,omitempty"`
	// Cert is the PC's own certificate (PEM), which pairing gave.
	Cert string `json:"cert"`
	MAC  string `json:"mac,omitempty"`
}

func (pc PC) port() int {
	if pc.Port > 0 {
		return pc.Port
	}
	return DefaultPort
}

// Info is what a PC says about itself (Sunshine's serverinfo).
type Info struct {
	Name       string `xml:"hostname" json:"name"`
	AppVersion string `xml:"appversion" json:"version"`
	GfeVersion string `xml:"GfeVersion" json:"-"`
	HTTPSPort  int    `xml:"HttpsPort" json:"-"`
	MAC        string `xml:"mac" json:"-"`
	// Codecs is the codecs it can send (ServerCodecModeSupport).
	Codecs int `xml:"ServerCodecModeSupport" json:"-"`
	// Paired says whether it knows Matinee: only asked over HTTPS.
	Paired bool `xml:"-" json:"paired"`
	// CurrentGame is the app it's streaming, by its id, or 0.
	CurrentGame int    `xml:"currentgame" json:"-"`
	State       string `xml:"state" json:"-"`
	PairStatus  int    `xml:"PairStatus" json:"-"`
}

// Busy says whether it's streaming to someone.
func (i Info) Busy() bool { return strings.HasSuffix(i.State, "_BUSY") }

// App is something Sunshine can start.
type App struct {
	ID    int    `xml:"ID" json:"id"`
	Title string `xml:"AppTitle" json:"title"`
}

// Client talks to Sunshine as Matinee.
type Client struct {
	Identity Identity
	// Timeout is for one request, apart from pairing's wait for the PIN.
	Timeout time.Duration
}

// CheckHost says whether host is an address on the home network, so the
// server only ever talks to a PC there.
func CheckHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("enter the PC's address, such as 192.168.1.30")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLoopback() {
			return errors.New("the PC has to be on your home network")
		}
		return nil
	}
	// A name on the home network, such as gaming-pc.local or gaming-pc.
	if strings.ContainsAny(host, "/:@?# ") {
		return errors.New("enter only the PC's address or name, such as 192.168.1.30")
	}
	return nil
}

// SplitAddress takes "host" or "host:port" as people type it.
func SplitAddress(addr string) (string, int, error) {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	addr = strings.TrimSuffix(addr, "/")
	host, port := addr, DefaultPort
	if h, p, err := net.SplitHostPort(addr); err == nil {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 5 || n > 65535 {
			return "", 0, errors.New("that port isn't right: Sunshine's is 47989 unless you changed it")
		}
		host, port = h, n
		// Sunshine's web page is one above its port: take that too.
		if n == DefaultPort+1 {
			port = DefaultPort
		}
	}
	if err := CheckHost(host); err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 10 * time.Second
}

// root is every answer's outer element.
type root struct {
	StatusCode    int    `xml:"status_code,attr"`
	StatusMessage string `xml:"status_message,attr"`
	Paired        int    `xml:"paired"`
	PlainCert     string `xml:"plaincert"`
	ChallengeResp string `xml:"challengeresponse"`
	PairingSecret string `xml:"pairingsecret"`
	SessionURL    string `xml:"sessionUrl0"`
	GameSession   int    `xml:"gamesession"`
	Resume        int    `xml:"resume"`
	Cancel        int    `xml:"cancel"`
	Info
	Apps []App `xml:"App"`
}

func (c *Client) query(extra url.Values) string {
	q := url.Values{}
	q.Set("uniqueid", c.Identity.UniqueID)
	uuid := make([]byte, 16)
	rand.Read(uuid)
	q.Set("uuid", hex.EncodeToString(uuid))
	for k, v := range extra {
		q[k] = v
	}
	return q.Encode()
}

// get asks over plain HTTP, as pairing does.
func (c *Client) get(ctx context.Context, host string, port int, path string, q url.Values, timeout time.Duration) (root, error) {
	u := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + path + "?" + c.query(q)
	tr := lanTransport()
	defer tr.CloseIdleConnections()
	return c.do(ctx, &http.Client{Timeout: timeout, Transport: tr}, u)
}

// lanTransport talks straight to the PC: never through a proxy, and not
// through the server's shared transport, which counts sites that don't
// answer as the internet being down.
func lanTransport() *http.Transport {
	return &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second}
}

// getTLS asks over HTTPS, with Matinee's certificate, of a PC whose own
// certificate must be the one pairing gave.
func (c *Client) getTLS(ctx context.Context, pc PC, httpsPort int, path, rawQuery string) (root, error) {
	cert, key, err := c.Identity.parse()
	if err != nil {
		return root{}, err
	}
	pb, _ := pem.Decode([]byte(pc.Cert))
	if pb == nil {
		return root{}, errors.New("the PC isn't paired")
	}
	pinned := pb.Bytes
	tr := lanTransport()
	tr.TLSClientConfig = &tls.Config{
		Certificates:       []tls.Certificate{{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert}},
		InsecureSkipVerify: true, // checked below, against the certificate pairing gave
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || !bytes.Equal(raw[0], pinned) {
				return errors.New("the PC's certificate changed: pair it again")
			}
			return nil
		},
	}
	defer tr.CloseIdleConnections()
	if httpsPort == 0 {
		httpsPort = pc.HTTPSPort
	}
	if httpsPort == 0 {
		httpsPort = pc.port() - 5
	}
	u := "https://" + net.JoinHostPort(pc.Host, strconv.Itoa(httpsPort)) + path + "?" + rawQuery
	return c.do(ctx, &http.Client{Timeout: c.timeout(), Transport: tr}, u)
}

func (c *Client) do(ctx context.Context, hc *http.Client, u string) (root, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return root{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return root{}, unreachable(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return root{}, unreachable(err)
	}
	var r root
	if err := xml.Unmarshal(body, &r); err != nil {
		if resp.StatusCode != http.StatusOK {
			return root{}, fmt.Errorf("the PC answered %s", resp.Status)
		}
		return root{}, errors.New("the PC's answer wasn't Sunshine's")
	}
	if r.StatusCode == 401 {
		return r, ErrNotPaired
	}
	if r.StatusCode != 0 && r.StatusCode != 200 {
		msg := r.StatusMessage
		if msg == "" {
			msg = "error " + strconv.Itoa(r.StatusCode)
		}
		return r, errors.New("the PC said: " + msg)
	}
	return r, nil
}

// ErrNotPaired means the PC doesn't know Matinee (any more).
var ErrNotPaired = errors.New("the PC doesn't know Matinee: pair it again")

// ErrNoAnswer means nothing answered as Sunshine at that address.
var ErrNoAnswer = errors.New("Sunshine didn't answer at that address: check that it's running on the PC and that its firewall allows it")

func unreachable(err error) error {
	var ne net.Error
	var oe *net.OpError
	if errors.As(err, &ne) && ne.Timeout() || errors.As(err, &oe) {
		return ErrNoAnswer
	}
	if strings.Contains(err.Error(), "certificate changed") {
		return errors.New("the PC's certificate changed: pair it again")
	}
	return err
}

// Find asks whatever is at host and port whether it's Sunshine, without
// pairing: for checking the address before pairing.
func (c *Client) Find(ctx context.Context, host string, port int) (Info, error) {
	r, err := c.get(ctx, host, port, "/serverinfo", nil, c.timeout())
	if err != nil {
		return Info{}, err
	}
	if r.AppVersion == "" {
		return Info{}, errors.New("that isn't Sunshine")
	}
	return r.Info, nil
}

// Info asks a paired PC how it is.
func (c *Client) Info(ctx context.Context, pc PC) (Info, error) {
	r, err := c.getTLS(ctx, pc, 0, "/serverinfo", c.query(nil))
	if err != nil {
		return Info{}, err
	}
	info := r.Info
	info.Paired = r.PairStatus == 1
	if !info.Paired {
		return info, ErrNotPaired
	}
	return info, nil
}

// Apps lists what the PC can start.
func (c *Client) Apps(ctx context.Context, pc PC) ([]App, error) {
	r, err := c.getTLS(ctx, pc, 0, "/applist", c.query(nil))
	if err != nil {
		return nil, err
	}
	return r.Apps, nil
}

// SteamApp picks the app that starts Steam's Big Picture, which Sunshine
// sets up on its own: by its title. Without one, it's the desktop, where
// Steam can be opened by hand.
func SteamApp(apps []App) (App, bool) {
	best, rank := App{}, 0
	for _, a := range apps {
		t := strings.ToLower(a.Title)
		r := 0
		switch {
		case strings.Contains(t, "steam") && strings.Contains(t, "big picture"):
			r = 3
		case strings.Contains(t, "steam"):
			r = 2
		case t == "desktop":
			r = 1
		}
		if r > rank {
			best, rank = a, r
		}
	}
	return best, rank > 0
}

// Launch has the PC start app (or go back to it, if it's already
// running), to stream at width × height and fps, and returns the stream's
// address. key and keyID encrypt the player's input; launchQuery is what
// matinee-pcremote asks for (Bridge.LaunchQuery).
func (c *Client) Launch(ctx context.Context, pc PC, app int, width, height, fps int, key []byte, keyID uint32, launchQuery string) (string, Info, error) {
	info, err := c.Info(ctx, pc)
	if err != nil {
		return "", info, err
	}
	path := "/launch"
	if info.CurrentGame != 0 {
		if info.CurrentGame == app {
			path = "/resume"
		} else if _, err := c.getTLS(ctx, pc, info.HTTPSPort, "/cancel", c.query(nil)); err != nil {
			return "", info, err
		}
	}
	q := url.Values{}
	q.Set("appid", strconv.Itoa(app))
	q.Set("mode", fmt.Sprintf("%dx%dx%d", width, height, fps))
	q.Set("additionalStates", "1")
	q.Set("sops", "0")
	q.Set("rikey", hex.EncodeToString(key))
	q.Set("rikeyid", strconv.FormatUint(uint64(keyID), 10))
	// The sound plays on the player, not on the PC too.
	q.Set("localAudioPlayMode", "0")
	q.Set("surroundAudioInfo", strconv.Itoa(2|0x3<<16)) // stereo
	// Up to four controllers, as the game stream has.
	q.Set("remoteControllersBitmap", "15")
	q.Set("gcmap", "15")
	q.Set("gcpersist", "0")
	r, err := c.getTLS(ctx, pc, info.HTTPSPort, path, c.query(q)+launchQuery)
	if err != nil {
		return "", info, err
	}
	if path == "/launch" && r.GameSession == 0 || path == "/resume" && r.Resume == 0 {
		return "", info, errors.New("the PC couldn't start the game")
	}
	if r.SessionURL == "" {
		// Older hosts don't say: the stream is at the PC's address.
		r.SessionURL = "rtsp://" + net.JoinHostPort(pc.Host, "48010")
	}
	return r.SessionURL, info, nil
}

// Quit ends what the PC is streaming.
func (c *Client) Quit(ctx context.Context, pc PC) error {
	_, err := c.getTLS(ctx, pc, 0, "/cancel", c.query(nil))
	return err
}

// Unpair has the PC forget Matinee.
func (c *Client) Unpair(ctx context.Context, pc PC) error {
	_, err := c.getTLS(ctx, pc, 0, "/unpair", c.query(nil))
	return err
}

// NewPIN makes the 4-digit code the person types into Sunshine's page.
func NewPIN() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(10000))
	return fmt.Sprintf("%04d", n.Int64())
}

// Pair pairs with the PC at host and port. Sunshine waits until someone
// types pin into its web page (PIN, on https://<pc>:47990), so this takes
// as long as that does, up to wait.
func (c *Client) Pair(ctx context.Context, host string, port int, pin string, wait time.Duration) (PC, error) {
	cert, key, err := c.Identity.parse()
	if err != nil {
		return PC{}, err
	}
	info, err := c.Find(ctx, host, port)
	if err != nil {
		return PC{}, err
	}
	step := func(q url.Values, timeout time.Duration) (root, error) {
		q.Set("devicename", "Matinee")
		q.Set("updateState", "1")
		r, err := c.get(ctx, host, port, "/pair", q, timeout)
		if err != nil {
			return r, err
		}
		if r.Paired != 1 {
			return r, errPairing
		}
		return r, nil
	}
	fail := func(err error) (PC, error) {
		c.get(context.WithoutCancel(ctx), host, port, "/unpair", nil, 5*time.Second)
		return PC{}, err
	}

	// 1: our certificate, and a salt; the PC answers with its own once
	// the PIN has been typed in.
	salt := random(16)
	aesKey := pinKey(salt, pin, info.AppVersion)
	r, err := step(url.Values{"phrase": {"getservercert"}, "salt": {hex.EncodeToString(salt)},
		"clientcert": {hex.EncodeToString([]byte(c.Identity.Cert))}}, wait)
	if err != nil {
		if errors.Is(err, errPairing) {
			return fail(errors.New("the PC didn't take the pairing: is another device pairing with it now?"))
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return fail(errors.New("nobody typed the PIN into Sunshine in time: try again"))
		}
		return fail(err)
	}
	certPEM, err := hex.DecodeString(r.PlainCert)
	if err != nil {
		return fail(errors.New("the PC sent a damaged certificate"))
	}
	pb, _ := pem.Decode(certPEM)
	if pb == nil {
		return fail(errors.New("the PC sent a damaged certificate"))
	}
	serverCert, err := x509.ParseCertificate(pb.Bytes)
	if err != nil {
		return fail(errors.New("the PC sent a damaged certificate"))
	}
	serverKey, ok := serverCert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fail(errors.New("the PC's certificate isn't RSA"))
	}

	// 2: a challenge to the PC, encrypted with the PIN.
	clientChallenge := random(16)
	r, err = step(url.Values{"clientchallenge": {hex.EncodeToString(ecb(aesKey, clientChallenge, true))}}, c.timeout())
	if err != nil {
		return fail(pairErr(err))
	}
	resp, err := hex.DecodeString(r.ChallengeResp)
	if err != nil || len(resp) < 48 || len(resp)%16 != 0 {
		return fail(errors.New("the PC's answer to pairing was damaged"))
	}
	resp = ecb(aesKey, resp, false)
	serverHash, serverChallenge := resp[:32], resp[32:48]

	// 3: our answer to its challenge.
	clientSecret := random(16)
	h := sha256.Sum256(concat(serverChallenge, cert.Signature, clientSecret))
	r, err = step(url.Values{"serverchallengeresp": {hex.EncodeToString(ecb(aesKey, h[:], true))}}, c.timeout())
	if err != nil {
		return fail(pairErr(err))
	}
	secret, err := hex.DecodeString(r.PairingSecret)
	if err != nil || len(secret) <= 16 {
		return fail(errors.New("the PC's answer to pairing was damaged"))
	}
	serverSecret, serverSig := secret[:16], secret[16:]
	sh := sha256.Sum256(serverSecret)
	if rsa.VerifyPKCS1v15(serverKey, crypto.SHA256, sh[:], serverSig) != nil {
		return fail(errors.New("the PC's answer to pairing wasn't signed by it"))
	}
	// The PC proves it knew the PIN.
	want := sha256.Sum256(concat(clientChallenge, serverCert.Signature, serverSecret))
	if !bytes.Equal(want[:], serverHash) {
		return fail(errors.New("the PIN didn't match: type the one Matinee shows into Sunshine"))
	}

	// 4: our secret, signed.
	ch := sha256.Sum256(clientSecret)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, ch[:])
	if err != nil {
		return fail(err)
	}
	if _, err := step(url.Values{"clientpairingsecret": {hex.EncodeToString(concat(clientSecret, sig))}}, c.timeout()); err != nil {
		return fail(pairErr(err))
	}

	pc := PC{Name: info.Name, Host: host, Port: port, HTTPSPort: info.HTTPSPort, Cert: string(certPEM), MAC: info.MAC}
	if pc.Name == "" {
		pc.Name = host
	}
	// 5: the same over HTTPS, which checks both certificates.
	r, err = c.getTLS(ctx, pc, info.HTTPSPort, "/pair", c.query(url.Values{"devicename": {"Matinee"}, "updateState": {"1"}, "phrase": {"pairchallenge"}}))
	if err != nil || r.Paired != 1 {
		if err == nil {
			err = errPairing
		}
		return fail(pairErr(err))
	}
	return pc, nil
}

var errPairing = errors.New("the PC stopped the pairing")

func pairErr(err error) error {
	if errors.Is(err, errPairing) {
		return errors.New("the PC stopped the pairing: check the PIN and try again")
	}
	return err
}

// pinKey is the AES key pairing encrypts with: from the salt and PIN,
// hashed with SHA-256 by any host of version 7 or later (Sunshine is),
// SHA-1 before.
func pinKey(salt []byte, pin, version string) []byte {
	b := concat(salt, []byte(pin))
	major, _ := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	if major > 0 && major < 7 {
		h := sha1Sum(b)
		return h[:16]
	}
	h := sha256.Sum256(b)
	return h[:16]
}

// ecb encrypts or decrypts b, a whole number of blocks, block by block,
// as the pairing protocol does.
func ecb(key, b []byte, encrypt bool) []byte {
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(b))
	for i := 0; i+16 <= len(b); i += 16 {
		if encrypt {
			block.Encrypt(out[i:], b[i:i+16])
		} else {
			block.Decrypt(out[i:], b[i:i+16])
		}
	}
	return out
}

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func concat(bs ...[]byte) []byte {
	var out []byte
	for _, b := range bs {
		out = append(out, b...)
	}
	return out
}
