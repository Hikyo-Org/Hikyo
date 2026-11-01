package hostupgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
)

type command struct {
	path      string
	args      []string
	env       []string
	directory string
	runtime   bool
	uid, gid  uint32
	rootKey   string
	// stderr receives the child's error output when set; otherwise it is
	// discarded because it can contain operational configuration.
	stderr io.Writer
	// stdin and stdout pass an operator's terminal through to an interactive
	// child. stdout unset means bounded capture for coordinator parsing.
	stdin  io.Reader
	stdout io.Writer
	// credential is an optional second private file handed to a runtime
	// child as descriptor 4, owned by the runtime user like the root key.
	credential string
}

type boundedOutput struct{ bytes []byte }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(b.bytes)+len(p) > 1<<20 {
		return 0, errors.New("command output exceeds 1 MiB")
	}
	b.bytes = append(b.bytes, p...)
	return len(p), nil
}

// Host exposes fixed deployment operations only. Root-owned configuration is
// authoritative. The caller must authenticate releases and drive a durable
// journal before any migration operation.
type Host struct {
	config       Config
	uid, gid     uint32
	env          map[string]string
	run          func(context.Context, command) ([]byte, error)
	client       *http.Client
	cgroup       string
	databasePath string
	// Internal path injection is for isolated adapter acceptance tests only.
	systemdDirectory string
	runtimeDirectory string
}

func New(c Config) (*Host, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("automatic host upgrades currently require Linux systemd")
	}
	if os.Geteuid() != 0 {
		return nil, errors.New("automatic host upgrades require sudo hikyo upgrade")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	resetUmask()
	u, err := user.Lookup(c.User)
	if err != nil {
		return nil, err
	}
	g, err := user.LookupGroup(c.Group)
	if err != nil {
		return nil, err
	}
	// 31 bits: the IDs are later passed to os.Chown and compared with
	// fileOwner as int, so they must fit a signed 32-bit integer everywhere.
	uid, err := strconv.ParseUint(u.Uid, 10, 31)
	if err != nil || uid == 0 {
		return nil, errors.New("invalid runtime UID")
	}
	gid, err := strconv.ParseUint(g.Gid, 10, 31)
	if err != nil || gid == 0 {
		return nil, errors.New("invalid runtime GID")
	}
	return &Host{config: c, uid: uint32(uid), gid: uint32(gid), run: runCommand, client: &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (h *Host) Config() Config  { return h.config }
func (h *Host) RuntimeUID() int { return int(h.uid) }
func (h *Host) RuntimeGID() int { return int(h.gid) }
func (h *Host) Environment() map[string]string {
	result := make(map[string]string, len(h.env))
	for k, v := range h.env {
		result[k] = v
	}
	return result
}

// CheckRunning verifies the configured binary and the active systemd process
// against an independently authenticated digest. It only reads identity and
// health; it never starts, stops or fences the service.
func (h *Host) CheckRunning(ctx context.Context, digest string) error {
	if h == nil || !validDigest(digest) {
		return errors.New("running release proof requires an exact binary SHA-256")
	}
	if err := trustedFile(h.config.Binary); err != nil {
		return err
	}
	actual, err := fileDigest(h.config.Binary)
	if err != nil || actual != digest {
		return errors.Join(err, errors.New("installed binary differs from authenticated source release"))
	}
	if err := h.checkCandidate(ctx, digest, true); err != nil {
		return err
	}
	actual, err = fileDigest(h.config.Binary)
	if err != nil || actual != digest {
		return errors.Join(err, errors.New("installed binary changed during running release proof"))
	}
	return nil
}

// PrepareCacheDirectory confines executable preparation to root-owned private
// storage outside the serving application's writable data and custody paths.
// It creates directories only; no service or database operation is performed.
func (h *Host) PrepareCacheDirectory(path string) error {
	if h == nil || !safePath(path) {
		return errors.New("security download cache requires a clean absolute path")
	}
	for _, protected := range []string{h.config.WorkingDirectory, h.config.PublicDirectory, h.config.InstallationDirectory, h.config.CustodyDirectory} {
		if within(protected, path) || within(path, protected) {
			return errors.New("security download cache overlaps runtime data or upgrade custody")
		}
	}
	parent := path
	for {
		if _, err := os.Lstat(parent); err == nil {
			if err := trustedDirectory(parent); err != nil {
				return err
			}
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent = filepath.Dir(parent)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if err := trustedDirectory(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0700 {
		return errors.Join(err, errors.New("security download cache must have owner-only directory permissions"))
	}
	return nil
}

// LocalBundleDirectory reads the installed deployment's existing evidence
// locator. The returned path is not authority; callers independently verify the
// signed bundle against their pinned root and installed rollback floor.
func (h *Host) LocalBundleDirectory() (string, error) {
	bundle := h.env["HIKYO_UPGRADE_BUNDLE"]
	runtimeFile := filepath.Join(h.config.StateDirectory, "runtime.env")
	if _, err := os.Lstat(runtimeFile); err == nil {
		if err := trustedFile(runtimeFile); err != nil {
			return "", err
		}
		info, err := os.Stat(runtimeFile)
		if err != nil || info.Size() > 1<<20 {
			return "", errors.Join(err, errors.New("native runtime evidence environment exceeds 1 MiB"))
		}
		data, err := os.ReadFile(runtimeFile)
		if err != nil {
			return "", err
		}
		values, err := ParseEnvironmentFile(data)
		if err != nil {
			return "", err
		}
		if value, present := values["HIKYO_UPGRADE_BUNDLE"]; present {
			bundle = value
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if bundle == "" {
		return "", errors.New("native source proof unavailable: installed deployment has no local signed upgrade bundle")
	}
	if !safePath(bundle) || !within(h.config.PublicDirectory, bundle) || bundle == h.config.PublicDirectory {
		return "", errors.New("native source bundle must be inside the configured public upgrade directory")
	}
	if err := trustedDirectory(filepath.Join(h.config.StateDirectory, "downloads")); err != nil {
		return "", fmt.Errorf("native source proof unavailable: retain the installed release's trusted offline archive cache: %w", err)
	}
	return bundle, nil
}

// Preflight is read-only and must run before maintenance. PostgreSQL and HA
// require a deployment-wide writer proof this single-unit adapter cannot give.
func (h *Host) Preflight(ctx context.Context) error {
	for _, p := range []string{h.config.EnvironmentFile, h.config.RootKeyFile, h.config.Binary, "/usr/bin/systemctl"} {
		if err := trustedFile(p); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(h.config.EnvironmentFile)
	if err != nil {
		return err
	}
	h.env, err = ParseEnvironmentFile(data)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(h.env["HIKYO_DB"], "sqlite:") || h.env["HIKYO_DB"] == "sqlite:" {
		return errors.New("automatic systemd upgrade currently supports a local SQLite installation only")
	}
	dbPath := strings.TrimPrefix(h.env["HIKYO_DB"], "sqlite:")
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(h.config.WorkingDirectory, dbPath)
	}
	if !safePath(dbPath) || !within(h.config.WorkingDirectory, dbPath) {
		return errors.New("SQLite database must be a plain file under the configured working directory")
	}
	infoDB, err := os.Lstat(dbPath)
	if err != nil {
		return err
	}
	if !infoDB.Mode().IsRegular() {
		return errors.New("SQLite database must be a regular file without symlinks")
	}
	h.databasePath = dbPath
	if v := h.env["HIKYO_HA"]; v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil || enabled {
			return errors.New("automatic systemd upgrade refuses HA deployments")
		}
	}
	if h.env["HIKYO_ROOT_KEY"] != "" {
		return errors.New("automatic upgrade requires the configured root-key file instead of HIKYO_ROOT_KEY")
	}
	info, err := os.Lstat(h.config.WorkingDirectory)
	if err != nil {
		return err
	}
	if !info.IsDir() || fileOwner(info) != int(h.uid) || info.Mode().Perm()&0022 != 0 {
		return errors.New("working directory must belong to the runtime user and not be group/world writable")
	}
	props, err := h.show(ctx, "User", "Group", "WorkingDirectory", "EnvironmentFiles", "ExecStart", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "RootDirectory", "RootImage", "KillMode", "ControlGroup")
	if err != nil {
		return err
	}
	if err = validateUnit(h.config, props); err != nil {
		return err
	}
	if err = h.validateUnitFiles(ctx); err != nil {
		return err
	}
	h.cgroup = props["ControlGroup"]
	if h.cgroup != "" && (!safePath(h.cgroup) || !strings.HasSuffix(h.cgroup, "/"+h.config.Unit)) {
		return errors.New("unexpected systemd control group")
	}
	return nil
}

func (h *Host) fenceConfiguration() string {
	return "[Unit]\nConditionPathExists=|!" + h.maintenanceFile() + "\nConditionPathExists=|" + h.startPermission() + "\n"
}

// An unrelated OR condition could satisfy systemd's trigger group while the
// maintenance condition is false. A later reset could remove it entirely.
// Refuse such unit configuration instead of weakening the reboot fence.
func (h *Host) validateUnitFiles(ctx context.Context) error {
	p, err := h.show(ctx, "FragmentPath", "DropInPaths")
	if err != nil {
		return err
	}
	paths := append([]string{p["FragmentPath"]}, strings.Fields(p["DropInPaths"])...)
	for _, path := range paths {
		if err := trustedFile(path); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Size() > 1<<20 {
			return errors.New("systemd unit file exceeds 1 MiB")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if path == h.dropin("90-hikyo-upgrade-fence.conf") {
			if string(data) != h.fenceConfiguration() {
				return errors.New("systemd upgrade fence differs from configured maintenance condition")
			}
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			if strings.HasSuffix(line, "\\") {
				return errors.New("automatic upgrade requires systemd directives without line continuations")
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "ConditionPathExists" || strings.HasPrefix(key, "Condition") && strings.HasPrefix(value, "|") {
				return errors.New("existing systemd condition could bypass the upgrade maintenance fence")
			}
		}
	}
	return nil
}

func validateUnit(c Config, p map[string]string) error {
	for name, want := range map[string]string{"User": c.User, "Group": c.Group, "WorkingDirectory": c.WorkingDirectory, "KillMode": "control-group"} {
		if p[name] != want {
			return fmt.Errorf("systemd %s does not match fixed upgrade configuration", name)
		}
	}
	for _, name := range []string{"ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "RootDirectory", "RootImage"} {
		if p[name] != "" {
			return fmt.Errorf("automatic upgrade does not support systemd %s", name)
		}
	}
	wantEnv := c.EnvironmentFile + " (ignore_errors=no)"
	if p["EnvironmentFiles"] != wantEnv && p["EnvironmentFiles"] != wantEnv+" "+filepath.Join(c.StateDirectory, "runtime.env")+" (ignore_errors=no)" {
		return errors.New("systemd EnvironmentFiles does not match fixed upgrade configuration")
	}
	// systemctl show's ExecStart includes state after the argv field. Match the
	// complete argv field, not a substring that would admit an extra command.
	start := p["ExecStart"]
	prefix := "{ path=" + c.Binary + " ; argv[]=" + c.Binary + " server "
	if !strings.HasPrefix(start, prefix) || strings.Count(start, "argv[]=") != 1 {
		return errors.New("systemd ExecStart is not the configured hikyo server")
	}
	args, _, ok := strings.Cut(strings.TrimPrefix(start, prefix), " ;")
	credential := "--root-key-file=/run/credentials/" + c.Unit + "/hikyo-root-key"
	if !ok || (args != credential && args != "--auto-migrate=false "+credential && args != "--root-key-file=%d/hikyo-root-key" && args != "--auto-migrate=false --root-key-file=%d/hikyo-root-key") {
		return errors.New("systemd ExecStart has unsupported arguments; use the configured root credential")
	}
	return nil
}

func (h *Host) PrepareDirectories() error {
	for _, p := range []string{h.config.StateDirectory, h.config.CustodyDirectory, h.config.CandidateDirectory, h.config.PublicDirectory, h.config.InstallationDirectory} {
		if err := trustedDirectory(filepath.Dir(p)); err != nil {
			return err
		}
		mode := os.FileMode(0700)
		owner := 0
		if p == h.config.CandidateDirectory || p == h.config.PublicDirectory {
			mode = 0755
		}
		if p == h.config.InstallationDirectory {
			owner = int(h.uid)
		}
		if err := os.Mkdir(p, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() || (fileOwner(info) != 0 && fileOwner(info) != owner) || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0077 != 0 && mode == 0700 {
			return fmt.Errorf("unsafe upgrade directory %s", p)
		}
		if err = os.Chown(p, owner, int(h.gid)); err != nil {
			return err
		}
		if err = os.Chmod(p, mode); err != nil {
			return err
		}
		if err = filedurability.SyncDirectory(filepath.Dir(p)); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) maintenanceFile() string { return filepath.Join(h.config.StateDirectory, "maintenance") }
func (h *Host) startPermission() string {
	dir := h.runtimeDirectory
	if dir == "" {
		dir = "/run/hikyo-upgrader"
	}
	return filepath.Join(dir, h.config.Unit+"-start")
}
func (h *Host) dropin(name string) string {
	dir := h.systemdDirectory
	if dir == "" {
		dir = "/etc/systemd/system"
	}
	return filepath.Join(dir, h.config.Unit+".d", name)
}

func (h *Host) writeDropin(name string, data []byte) error {
	dir := filepath.Dir(h.dropin(name))
	if err := trustedDirectory(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return atomicWrite(h.dropin(name), data, 0644)
}

func (h *Host) Maintenance() (bool, error) {
	_, err := os.Lstat(h.maintenanceFile())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// FenceAndStop first records maintenance and a persistent systemd condition,
// then stops the whole control group. Reboot cannot resurrect the old writer.
func (h *Host) FenceAndStop(ctx context.Context) error {
	fence := h.fenceConfiguration()
	if err := h.writeDropin("90-hikyo-upgrade-fence.conf", []byte(fence)); err != nil {
		return err
	}
	if _, err := h.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	// Install the boot condition before publishing its maintenance marker.
	// A crash before this point has not changed the database or stopped the
	// source. A crash after it cannot reboot an unfenced writer.
	if err := atomicWrite(h.maintenanceFile(), []byte("upgrade in progress\n"), 0600); err != nil {
		return err
	}
	if err := os.Remove(h.startPermission()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := h.systemctl(ctx, "stop", h.config.Unit); err != nil {
		return err
	}
	return h.verifyStopped(ctx)
}

func (h *Host) verifyStopped(ctx context.Context) error {
	p, err := h.show(ctx, "ActiveState", "MainPID", "ControlGroup")
	if err != nil {
		return err
	}
	if p["MainPID"] != "0" || (p["ActiveState"] != "inactive" && p["ActiveState"] != "failed") {
		return errors.New("systemd unit still has a running writer")
	}
	group := h.cgroup
	if p["ControlGroup"] != "" {
		group = p["ControlGroup"]
	}
	if group != "" {
		if !safePath(group) || !strings.HasSuffix(group, "/"+h.config.Unit) {
			return errors.New("unexpected systemd control group")
		}
		if err := emptyControlGroup(filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(group, "/"))); err != nil {
			return err
		}
	}
	if h.databasePath != "" {
		return noOpenDatabaseDescriptors("/proc", h.databasePath)
	}
	return nil
}

// Other local CLI/server processes may live outside the service's cgroup.
// Refuse any open database descriptor before export or migration instead of
// treating an inactive systemd unit as proof that every writer stopped.
func noOpenDatabaseDescriptors(proc, database string) error {
	processes, err := os.ReadDir(proc)
	if err != nil {
		return err
	}
	for _, process := range processes {
		if !process.IsDir() {
			continue
		}
		if _, err := strconv.ParseUint(process.Name(), 10, 32); err != nil {
			continue
		}
		fdDir := filepath.Join(proc, process.Name(), "fd")
		descriptors, err := os.ReadDir(fdDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot establish local SQLite writer stop: %w", err)
		}
		for _, descriptor := range descriptors {
			path, err := os.Readlink(filepath.Join(fdDir, descriptor.Name()))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("cannot inspect local database descriptor: %w", err)
			}
			path = strings.TrimSuffix(path, " (deleted)")
			if path == database || path == database+"-wal" || path == database+"-shm" {
				return fmt.Errorf("process %s still has the SQLite database open", process.Name())
			}
		}
	}
	return nil
}

func emptyControlGroup(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("unexpected symlink in systemd control group")
		}
		if entry.Name() == "cgroup.procs" {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(b)) != "" {
				return errors.New("systemd control group still contains writers")
			}
		}
		return nil
	})
}

type RuntimeEvidence struct {
	BundleDirectory      string
	OperatorPublicKey    string
	EvidenceDirectory    string
	CiphertextPath       string
	TargetManifest       string
	LegacyWritersStopped bool
}

func (h *Host) runtimeEnvironment(e RuntimeEvidence) (map[string]string, error) {
	values := h.Environment()
	for k := range values {
		if strings.HasPrefix(k, "HIKYO_UPGRADE_") {
			delete(values, k)
		}
	}
	for key, path := range map[string]string{"HIKYO_UPGRADE_BUNDLE": e.BundleDirectory, "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY": e.OperatorPublicKey, "HIKYO_UPGRADE_EVIDENCE": e.EvidenceDirectory, "HIKYO_UPGRADE_BACKUP": e.CiphertextPath} {
		if path == "" {
			continue
		}
		if !safePath(path) || !within(h.config.PublicDirectory, path) {
			return nil, fmt.Errorf("%s must be under the configured public upgrade directory", key)
		}
		values[key] = path
	}
	if e.BundleDirectory == "" || e.OperatorPublicKey == "" {
		return nil, errors.New("runtime upgrade requires bundle and operator public key")
	}
	if e.TargetManifest != "" && !validDigest(e.TargetManifest) {
		return nil, errors.New("invalid target manifest digest")
	}
	if e.TargetManifest != "" {
		values["HIKYO_UPGRADE_TARGET_MANIFEST"] = e.TargetManifest
	}
	values["HIKYO_UPGRADE_STATE_DIR"] = h.config.InstallationDirectory
	if e.LegacyWritersStopped {
		values["HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED"] = "true"
	}
	values["HIKYO_ROOT_KEY_FILE"] = "/proc/self/fd/3"
	return values, nil
}

type ExportRequest struct {
	OutputDirectory string
	Recipient       string
	Runtime         RuntimeEvidence
}

func (h *Host) Export(ctx context.Context, candidate string, request ExportRequest) ([]byte, error) {
	if !safePath(request.OutputDirectory) || !within(h.config.PublicDirectory, request.OutputDirectory) {
		return nil, errors.New("backup output must be inside the configured public upgrade directory")
	}
	if request.Recipient == "" || strings.ContainsAny(request.Recipient, "\x00\n\r") {
		return nil, errors.New("missing or invalid backup recipient")
	}
	return h.runRuntime(ctx, candidate, []string{"backup", "upgrade-export", "--json", "--out", request.OutputDirectory, "--recipient", request.Recipient}, request.Runtime)
}

func (h *Host) Migrate(ctx context.Context, candidate string, e RuntimeEvidence) ([]byte, error) {
	return h.runRuntime(ctx, candidate, []string{"migrate"}, e)
}

func (h *Host) runRuntime(ctx context.Context, candidate string, args []string, e RuntimeEvidence) ([]byte, error) {
	if !within(h.config.CandidateDirectory, candidate) || filepath.Dir(candidate) != h.config.CandidateDirectory {
		return nil, errors.New("runtime command requires a staged candidate")
	}
	if err := trustedFile(candidate); err != nil {
		return nil, err
	}
	if active, err := h.Maintenance(); err != nil || !active {
		return nil, errors.Join(err, errors.New("runtime preparation requires durable maintenance"))
	}
	if err := h.verifyStopped(ctx); err != nil {
		return nil, err
	}
	values, err := h.runtimeEnvironment(e)
	if err != nil {
		return nil, err
	}
	var stderr boundedOutput
	output, err := h.run(ctx, command{path: candidate, args: args, env: environmentList(values), directory: h.config.WorkingDirectory, runtime: true, uid: h.uid, gid: h.gid, rootKey: h.config.RootKeyFile, stderr: &stderr})
	if err == nil {
		return output, nil
	}
	// The operator must see why the runtime refused. Its stderr can name
	// configuration, so it goes to a root-only file, never to the terminal.
	log := filepath.Join(h.config.StateDirectory, "runtime-"+args[0]+"-"+time.Now().UTC().Format("20060102T150405Z")+".log")
	if writeErr := atomicWrite(log, stderr.bytes, 0600); writeErr != nil {
		return nil, fmt.Errorf("runtime %s failed: %w (its error output could not be saved: %v)", args[0], err, writeErr)
	}
	return nil, fmt.Errorf("runtime %s failed: %w; its error output is in %s", args[0], err, log)
}

func environmentList(values map[string]string) []string {
	// Do not inherit sudo, loader, signing passwords, or operator shell state.
	result := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	keys := make([]string, 0, len(values))
	for k := range values {
		if strings.HasPrefix(k, "HIKYO_") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		result = append(result, k+"="+values[k])
	}
	return result
}

func (h *Host) StageCandidate(source, digest string) (string, error) {
	if !validDigest(digest) {
		return "", errors.New("invalid candidate SHA-256")
	}
	destination := filepath.Join(h.config.CandidateDirectory, "hikyo-"+digest)
	return destination, copyBinary(source, destination, digest)
}

// PruneCandidates removes every staged candidate executable. InstallBinary
// copies the chosen candidate into place, so after a completed upgrade the
// staging copies are dead weight of over 100 MiB each.
func (h *Host) PruneCandidates() error {
	if err := trustedDirectory(h.config.CandidateDirectory); err != nil {
		return err
	}
	entries, err := os.ReadDir(h.config.CandidateDirectory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!(strings.HasPrefix(name, "hikyo-") && validDigest(strings.TrimPrefix(name, "hikyo-"))) && !strings.HasPrefix(name, ".hikyo-binary-")) {
			continue
		}
		if err := os.Remove(filepath.Join(h.config.CandidateDirectory, name)); err != nil {
			return err
		}
	}
	return filedurability.SyncDirectory(h.config.CandidateDirectory)
}

func (h *Host) InstallBinary(ctx context.Context, candidate, digest string) error {
	if filepath.Dir(candidate) != h.config.CandidateDirectory {
		return errors.New("binary installation requires a staged candidate")
	}
	if active, err := h.Maintenance(); err != nil || !active {
		return errors.Join(err, errors.New("binary installation requires durable maintenance"))
	}
	if err := h.verifyStopped(ctx); err != nil {
		return err
	}
	return copyBinary(candidate, h.config.Binary, digest)
}

func (h *Host) ConfigureRuntime(ctx context.Context, e RuntimeEvidence) error {
	values, err := h.runtimeEnvironment(e)
	if err != nil {
		return err
	}
	// Absence in this later EnvironmentFile would leave old evidence active.
	for _, key := range []string{"HIKYO_UPGRADE_BUNDLE", "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY", "HIKYO_UPGRADE_EVIDENCE", "HIKYO_UPGRADE_BACKUP", "HIKYO_UPGRADE_OPERATOR_INSTANCE", "HIKYO_UPGRADE_TARGET_MANIFEST", "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED"} {
		if _, exists := values[key]; !exists {
			values[key] = ""
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		if strings.HasPrefix(k, "HIKYO_UPGRADE_") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var env strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&env, "%s=%s\n", k, values[k])
	}
	if err = atomicWrite(filepath.Join(h.config.StateDirectory, "runtime.env"), []byte(env.String()), 0600); err != nil {
		return err
	}
	dropin := "[Service]\nEnvironmentFile=" + filepath.Join(h.config.StateDirectory, "runtime.env") + "\nReadWritePaths=" + h.config.PublicDirectory + " " + h.config.InstallationDirectory + "\nLoadCredential=hikyo-root-key:" + h.config.RootKeyFile + "\nExecStart=\nExecStart=" + h.config.Binary + " server --auto-migrate=false --root-key-file=%d/hikyo-root-key\n"
	if err = h.writeDropin("91-hikyo-upgrade-runtime.conf", []byte(dropin)); err != nil {
		return err
	}
	_, err = h.systemctl(ctx, "daemon-reload")
	return err
}

// StartCandidate permits this boot only. The persistent maintenance condition
// survives a host reboot until Complete follows the caller's exact DB check.
func (h *Host) StartCandidate(ctx context.Context, digest string, requireReady bool, timeout time.Duration) (err error) {
	if !validDigest(digest) || timeout <= 0 || timeout > 10*time.Minute {
		return errors.New("invalid candidate health check parameters")
	}
	if active, e := h.Maintenance(); e != nil || !active {
		return errors.Join(e, errors.New("candidate start requires durable maintenance"))
	}
	if err = os.Mkdir(filepath.Dir(h.startPermission()), 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err = atomicWrite(h.startPermission(), []byte("temporary candidate start\n"), 0600); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			err = errors.Join(err, h.FenceAndStop(cleanup))
		}
	}()
	if _, err = h.systemctl(ctx, "start", h.config.Unit); err != nil {
		return err
	}
	check, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err = h.checkCandidate(check, digest, requireReady); err == nil {
			return nil
		}
		select {
		case <-check.Done():
			return fmt.Errorf("candidate health check: %w", check.Err())
		case <-ticker.C:
		}
	}
}

func (h *Host) checkCandidate(ctx context.Context, digest string, requireReady bool) error {
	p, err := h.show(ctx, "ActiveState", "MainPID")
	if err != nil {
		return err
	}
	pid, err := strconv.ParseUint(p["MainPID"], 10, 32)
	if err != nil || pid == 0 || p["ActiveState"] != "active" {
		return errors.New("candidate service is not active")
	}
	actual, err := fileDigest(filepath.Join("/proc", strconv.FormatUint(pid, 10), "exe"))
	if err != nil || actual != digest {
		return errors.Join(err, errors.New("running service binary does not match authenticated candidate"))
	}
	if err = h.checkHTTP(ctx, strings.TrimSuffix(h.config.HealthURL, "/readyz")+"/healthz", http.StatusOK); err != nil {
		return err
	}
	status := http.StatusServiceUnavailable
	if requireReady {
		status = http.StatusOK
	}
	if err = h.checkHTTP(ctx, h.config.HealthURL, status); err != nil {
		return err
	}
	// Re-read PID after HTTP probes so a replacement process cannot borrow a
	// previous candidate's identity check.
	after, err := h.show(ctx, "ActiveState", "MainPID")
	if err != nil {
		return err
	}
	if after["MainPID"] != p["MainPID"] || after["ActiveState"] != "active" {
		return errors.New("candidate restarted during health check")
	}
	return nil
}

func (h *Host) checkHTTP(ctx context.Context, url string, status int) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := h.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != status {
		return fmt.Errorf("candidate health endpoint returned %d, expected %d", response.StatusCode, status)
	}
	return nil
}

// Complete may only be called after the coordinator validates the exact final
// database identity and healthy phase. It never performs a rollback.
func (h *Host) Complete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(h.maintenanceFile()); err != nil {
		return err
	}
	if err := filedurability.SyncDirectory(h.config.StateDirectory); err != nil {
		return err
	}
	if err := os.Remove(h.startPermission()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (h *Host) systemctl(ctx context.Context, args ...string) ([]byte, error) {
	diagnostics.Printf(ctx, 2, "systemd operation %s", args[0])
	defer diagnostics.Time(ctx, "systemd "+args[0])()
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	output, err := h.run(bounded, command{path: "/usr/bin/systemctl", args: append([]string{"--no-pager", "--no-ask-password"}, args...), env: []string{"PATH=/usr/bin:/bin", "LANG=C"}})
	if err != nil {
		return nil, fmt.Errorf("systemd %s: %w", args[0], err)
	}
	return output, nil
}

func (h *Host) show(ctx context.Context, properties ...string) (map[string]string, error) {
	output, err := h.systemctl(ctx, "show", "--all", "--property="+strings.Join(properties, ","), h.config.Unit)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(properties))
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, errors.New("invalid systemd property output")
		}
		if _, exists := values[key]; exists {
			if key == "EnvironmentFiles" {
				// systemctl emits one line per EnvironmentFile, preserving order.
				values[key] += " " + value
				continue
			}
			return nil, errors.New("duplicate systemd property")
		}
		values[key] = value
	}
	for _, key := range properties {
		if _, ok := values[key]; !ok {
			// systemctl omits empty Exec* command arrays even with --all.
			// Nonempty arrays are emitted and rejected by validateUnit; other
			// omitted properties still indicate an unsupported manager response.
			switch key {
			case "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost":
				values[key] = ""
				continue
			}
			return nil, fmt.Errorf("systemd omitted required property %s", key)
		}
	}
	return values, nil
}

// RunOperator executes one operator verb of the installed executable the way
// the service itself runs: as the runtime user, in the working directory,
// with the unit's environment files applied in order and the configured root
// key on descriptor 3. Root-run host commands therefore need no hand-fed
// configuration. A --root-key-file argument names a private file only root
// can read; it is handed to the child on descriptor 4 so an escrow custody
// copy never has to be re-owned. The verb keeps the operator's terminal.
func (h *Host) RunOperator(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("operator command requires a verb")
	}
	if err := h.Preflight(ctx); err != nil {
		return err
	}
	values := h.Environment()
	runtimeEnv := filepath.Join(h.config.StateDirectory, "runtime.env")
	if err := trustedFile(runtimeEnv); err == nil {
		data, err := os.ReadFile(runtimeEnv)
		if err != nil {
			return err
		}
		extra, err := ParseEnvironmentFile(data)
		if err != nil {
			return fmt.Errorf("%s: %w", runtimeEnv, err)
		}
		for k, v := range extra {
			values[k] = v
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	values["HIKYO_ROOT_KEY_FILE"] = "/proc/self/fd/3"
	args = slices.Clone(args)
	credential := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--root-key-file" && i+1 < len(args) {
			credential, args[i+1] = args[i+1], "/proc/self/fd/4"
		} else if rest, ok := strings.CutPrefix(args[i], "--root-key-file="); ok {
			credential, args[i] = rest, "--root-key-file=/proc/self/fd/4"
		}
	}
	if credential != "" && (!safePath(credential) || !filepath.IsAbs(credential)) {
		return errors.New("--root-key-file must be an absolute path")
	}
	_, err := h.run(ctx, command{path: h.config.Binary, args: args, env: environmentList(values), directory: h.config.WorkingDirectory, runtime: true, uid: h.uid, gid: h.gid, rootKey: h.config.RootKeyFile, credential: credential, stdin: stdin, stdout: stdout, stderr: stderr})
	return err
}
