package link

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHConn is an interactive SSH session.
type SSHConn struct {
	*stream
	client  *ssh.Client
	session *ssh.Session
}

// DialSSH opens an SSH session with a pseudo-terminal of type termType.
// Authentication tries the SSH agent, then key files in ~/.ssh, then
// keyboard-interactive and password (asked through h.Prompt).
func DialSSH(host string, port int, user, termType string, h Handlers) (*SSHConn, error) {
	if host == "" {
		return nil, fmt.Errorf("no host name entered")
	}
	if user == "" {
		return nil, fmt.Errorf("no user name entered")
	}
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	hostKeyCB, err := hostKeyCallback(h)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods(h),
		HostKeyCallback: hostKeyCB,
		Timeout:         waitTimeout,
		// Ask the host for a key of a type already in known_hosts. A host
		// usually has several keys (Ed25519, ECDSA, RSA); if known_hosts holds
		// one type (for example saved by the system's ssh) and we asked for
		// another, it would look like the key had changed.
		HostKeyAlgorithms: knownKeyAlgorithms(addr),
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		if errors.Is(err, ErrCancelled) || strings.Contains(err.Error(), ErrCancelled.Error()) {
			return nil, ErrCancelled
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, authFailed(host, user)
		}
		return nil, fmt.Errorf("ssh %s: %w", addr, netReason(err))
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	if termType == "" {
		termType = "d200"
	}
	if err := sess.RequestPty(termType, 24, 80, modes); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("ssh pty: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("ssh shell: %w", err)
	}
	c := &SSHConn{client: client, session: sess}
	desc := "ssh " + user + "@" + host
	if port != 22 {
		desc += ":" + strconv.Itoa(port)
	}
	c.stream = newStream(h, stdin, func() error {
		sess.Close()
		return client.Close()
	}, desc)
	go func() {
		_ = sess.Wait()
		pw.CloseWithError(errors.New("SSH session ended"))
	}()
	c.stream.start(pr)
	return c, nil
}

// Break sends an RFC 4335 break request.
func (c *SSHConn) Break(d time.Duration) error {
	payload := ssh.Marshal(struct{ Ms uint32 }{uint32(d / time.Millisecond)})
	_, err := c.session.SendRequest("break", false, payload)
	return err
}

func sshDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh")
}

func authMethods(h Handlers) []ssh.AuthMethod {
	var methods []ssh.AuthMethod
	var signers []ssh.Signer

	// SSH agent (macOS/Linux).
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			if s, err := agent.NewClient(conn).Signers(); err == nil {
				signers = append(signers, s...)
			}
		}
	}
	// Key files.
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		data, err := os.ReadFile(filepath.Join(sshDir(), name))
		if err != nil {
			continue
		}
		s, err := ssh.ParsePrivateKey(data)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) && h.Prompt != nil {
			pass, perr := h.Prompt("Passphrase for ~/.ssh/"+name+" (Esc to skip):", false)
			if perr == nil && pass != "" {
				s, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(pass))
			}
		}
		if err == nil && s != nil {
			signers = append(signers, s)
		}
	}
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if h.Prompt != nil {
		methods = append(methods,
			ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i, q := range questions {
					if instruction != "" && i == 0 {
						q = instruction + " " + q
					}
					a, err := h.Prompt(strings.TrimSpace(q), echos[i])
					if err != nil {
						return nil, err
					}
					answers[i] = a
				}
				return answers, nil
			}),
			ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
				return h.Prompt("Password:", false)
			}), 3),
		)
	}
	return methods
}

// authFailed explains a refused login. The usual cause is a computer that
// only accepts keys the built-in SSH can't use: security keys (YubiKey and
// other FIDO keys, "sk-" key types), or keys named only in ~/.ssh/config.
// The system's own ssh can use those, through the Local program connection.
func authFailed(host, user string) error {
	msg := "SSH login refused for " + user + "@" + host + "."
	if usesSecurityKeys() {
		msg += " Your SSH keys include a security key (e.g. a YubiKey), which the built-in SSH" +
			" can't use. Choose Connection: Local program with the command  ssh " + user + "@" + host +
			"  (or your ~/.ssh/config name for it)."
	} else {
		msg += " The built-in SSH tries your SSH agent, ~/.ssh/id_ed25519, id_ecdsa and id_rsa," +
			" then a password. If you log in with other keys or settings from ~/.ssh/config," +
			" use Connection: Local program with the command  ssh " + user + "@" + host + "."
	}
	return errors.New(msg)
}

// usesSecurityKeys reports whether ~/.ssh holds any security-key public keys.
func usesSecurityKeys() bool {
	files, _ := filepath.Glob(filepath.Join(sshDir(), "*.pub"))
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil && strings.HasPrefix(string(data), "sk-") {
			return true
		}
	}
	return false
}

// knownKeyAlgorithms lists the host key algorithms matching the key types
// known_hosts holds for addr ("host:port"), or nil (the library's default
// order) for a host not yet known.
func knownKeyAlgorithms(addr string) []string {
	check, err := knownhosts.New(filepath.Join(sshDir(), "known_hosts"))
	if err != nil {
		return nil
	}
	remote := &net.TCPAddr{IP: net.IPv4zero}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			remote = &net.TCPAddr{IP: ip, Port: atoiPort(port)}
		}
	}
	var ke *knownhosts.KeyError
	if err := check(addr, remote, probeKey{}); !errors.As(err, &ke) || len(ke.Want) == 0 {
		return nil
	}
	var algos []string
	seen := map[string]bool{}
	add := func(a ...string) {
		for _, x := range a {
			if !seen[x] {
				seen[x] = true
				algos = append(algos, x)
			}
		}
	}
	for _, k := range ke.Want {
		switch t := k.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			add(ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			add(t)
		}
	}
	return algos
}

func atoiPort(s string) int { n, _ := strconv.Atoi(s); return n }

// probeKey is a key no host has, used to ask knownhosts which keys it holds.
type probeKey struct{}

func (probeKey) Type() string                        { return "probe" }
func (probeKey) Marshal() []byte                     { return []byte("dasher200-probe") }
func (probeKey) Verify([]byte, *ssh.Signature) error { return errors.New("probe key") }

// hostKeyCallback checks ~/.ssh/known_hosts. An unknown host is shown to
// the user to accept (and is then remembered); a changed key is refused.
func hostKeyCallback(h Handlers) (ssh.HostKeyCallback, error) {
	dir := sshDir()
	path := filepath.Join(dir, "known_hosts")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.MkdirAll(dir, 0o700)
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			f.Close()
		}
	}
	check, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		if len(ke.Want) > 0 {
			w := ke.Want[0]
			return fmt.Errorf("HOST KEY FOR %s HAS CHANGED - refusing to connect. It now sends %s %s,"+
				" but line %d of %s has a different key. If the computer was reinstalled or"+
				" its keys regenerated, delete that line (or run: ssh-keygen -R %s) and connect again.",
				hostname, key.Type(), ssh.FingerprintSHA256(key), w.Line, w.Filename, knownhosts.Normalize(hostname))
		}
		if h.Prompt == nil {
			return fmt.Errorf("unknown host key for %s", hostname)
		}
		q := fmt.Sprintf("New host %s, %s key %s. Trust it? Type yes:",
			hostname, key.Type(), ssh.FingerprintSHA256(key))
		ans, perr := h.Prompt(q, true)
		if perr != nil || !strings.EqualFold(strings.TrimSpace(ans), "yes") {
			return ErrCancelled
		}
		line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
		f, ferr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if ferr == nil {
			var b bytes.Buffer
			b.WriteString(line + "\n")
			_, _ = f.Write(b.Bytes())
			f.Close()
		}
		return nil
	}, nil
}
