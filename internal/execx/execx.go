// Package execx запускает внешние утилиты без shell.
//
// Для nft/ip/docker требуется root. Если приложение запущено не от root,
// команда автоматически получает префикс "sudo -n" (как требует ТЗ).
// Пароль sudo не запрашивается: приложение должно запускаться либо от root,
// либо в окружении с passwordless sudo.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner — исполнитель внешних команд.
type Runner struct {
	Sudo    bool
	Timeout time.Duration
}

// New создаёт Runner. sudo включается, когда euid != 0.
func New(sudo bool, timeout time.Duration) *Runner {
	return &Runner{Sudo: sudo, Timeout: timeout}
}

// CommandError содержит команду, исходную ошибку и stderr.
type CommandError struct {
	Cmd    string
	Err    error
	Stderr string
}

func (e *CommandError) Error() string {
	msg := e.Cmd + ": " + e.Err.Error()
	if s := strings.TrimSpace(e.Stderr); s != "" {
		if len(s) > 500 {
			s = s[:500] + "…"
		}
		msg += " (" + s + ")"
	}
	return msg
}

func (e *CommandError) Unwrap() error { return e.Err }

// Run выполняет команду без sudo.
func (r *Runner) Run(ctx context.Context, name string, args ...string) (string, error) {
	return r.runTimeout(ctx, false, 0, name, args...)
}

// RunSudo выполняет команду с sudo, если приложение не root.
func (r *Runner) RunSudo(ctx context.Context, name string, args ...string) (string, error) {
	return r.runTimeout(ctx, true, 0, name, args...)
}

// RunTimeout — команда без sudo с индивидуальным таймаутом.
func (r *Runner) RunTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	return r.runTimeout(ctx, false, timeout, name, args...)
}

// RunSudoTimeout — команда с sudo и индивидуальным таймаутом.
func (r *Runner) RunSudoTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	return r.runTimeout(ctx, true, timeout, name, args...)
}

func (r *Runner) runTimeout(ctx context.Context, needSudo bool, timeout time.Duration, name string, args ...string) (string, error) {
	bin, err := Resolve(name)
	if err != nil {
		return "", &CommandError{Cmd: name, Err: err}
	}

	cmdArgs := append([]string(nil), args...)
	if needSudo && r.Sudo {
		sudo, serr := Resolve("sudo")
		if serr != nil {
			return "", &CommandError{Cmd: "sudo", Err: serr}
		}
		cmdArgs = append([]string{"-n", bin}, cmdArgs...)
		bin = sudo
	}

	if timeout <= 0 {
		timeout = r.Timeout
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, bin, cmdArgs...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	out := normalize(stdout.String())
	if runErr != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return out, &CommandError{
				Cmd:    bin + " " + strings.Join(cmdArgs, " "),
				Err:    fmt.Errorf("таймаут %s", timeout),
				Stderr: stderr.String(),
			}
		}
		return out, &CommandError{
			Cmd:    bin + " " + strings.Join(cmdArgs, " "),
			Err:    runErr,
			Stderr: stderr.String(),
		}
	}
	return out, nil
}

// Resolve ищет исполняемый файл, учитывая типичные каталоги sbin,
// которые могут отсутствовать в PATH при запуске без login-shell.
func Resolve(name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		if isExecutable(name) {
			return name, nil
		}
		return "", fmt.Errorf("%w: %s", exec.ErrNotFound, name)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, candidate := range fallbacks(name) {
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: %s", exec.ErrNotFound, name)
}

var fallbackDirs = map[string][]string{
	"nft":       {"/sbin/nft", "/usr/sbin/nft", "/usr/bin/nft"},
	"ip":        {"/sbin/ip", "/usr/sbin/ip", "/usr/bin/ip", "/bin/ip"},
	"docker":    {"/usr/bin/docker", "/usr/local/bin/docker", "/snap/bin/docker"},
	"accel-cmd": {"/usr/bin/accel-cmd", "/usr/sbin/accel-cmd"},
	"systemctl": {"/usr/bin/systemctl", "/bin/systemctl"},
	"sudo":      {"/usr/bin/sudo", "/bin/sudo"},
	"git":       {"/usr/bin/git"},
	"lsmod":     {"/sbin/lsmod", "/usr/sbin/lsmod", "/bin/lsmod"},
}

func fallbacks(name string) []string {
	if v, ok := fallbackDirs[name]; ok {
		return v
	}
	return []string{
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
		filepath.Join("/bin", name),
		filepath.Join("/usr/sbin", name),
		filepath.Join("/sbin", name),
	}
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimRight(s, "\n")
}
