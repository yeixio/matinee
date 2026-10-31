package main

import (
	"bufio"
	"net"
	"os"
	"strings"
)

// readEnvFile reads matinee.env: KEY=value lines handed to the server, the
// same settings as the Linux packages' /etc/matinee/matinee.env. Blank lines
// and lines starting with # are skipped. A missing file is no settings.
func readEnvFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var env []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		env = append(env, strings.TrimSpace(k)+"="+v)
	}
	return env
}

// lookupEnv finds a setting in env, the last one winning as it does for the server.
func lookupEnv(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, _ := strings.Cut(kv, "="); k == key {
			v = val
		}
	}
	return v
}

// localURL is where this computer reaches a server listening on addr
// (MATINEE_ADDR, such as :7171 or 0.0.0.0:8080).
func localURL(addr string) string {
	if addr == "" {
		addr = ":7171"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://localhost:7171"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

const envFileTemplate = `# Matinee server settings, one KEY=value per line. Quit Matinee from the
# tray icon and open it again after changing them.
# All settings: https://matinee.yeix.io/docs/server/configuration/
#
# MATINEE_ADDR=:7171
# MATINEE_SERVER_NAME=Matinee
`

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
