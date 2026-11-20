// workflow-lint validates GitHub's concurrency queue configuration and the
// vulnerability-alerts permission before passing the workflow to pinned
// actionlint, which predates both.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

func field(node *yaml.Node, name string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func validateQueues(raw []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 {
		return nil, fmt.Errorf("expected one workflow document")
	}
	root := doc.Content[0]
	var blocks []*yaml.Node
	if concurrency := field(root, "concurrency"); concurrency != nil {
		blocks = append(blocks, concurrency)
	}
	if jobs := field(root, "jobs"); jobs != nil && jobs.Kind == yaml.MappingNode {
		for i := 1; i < len(jobs.Content); i += 2 {
			if concurrency := field(jobs.Content[i], "concurrency"); concurrency != nil {
				blocks = append(blocks, concurrency)
			}
		}
	}
	lines := strings.Split(string(raw), "\n")
	for _, block := range blocks {
		if block.Kind != yaml.MappingNode {
			continue
		}
		count := 0
		for i := 0; i < len(block.Content); i += 2 {
			if block.Content[i].Value == "queue" {
				count++
			}
		}
		if count > 1 {
			return nil, fmt.Errorf("duplicate concurrency queue")
		}
		queue := field(block, "queue")
		if queue == nil {
			continue
		}
		if queue.Kind != yaml.ScalarNode || queue.Tag != "!!str" || (queue.Value != "max" && queue.Value != "single") {
			return nil, fmt.Errorf("line %d: concurrency queue must be max or single", queue.Line)
		}
		if queue.Value == "max" {
			cancel := field(block, "cancel-in-progress")
			if cancel != nil && (cancel.Kind != yaml.ScalarNode || cancel.Tag != "!!bool" || cancel.Value != "false") {
				return nil, fmt.Errorf("line %d: queue max requires cancel-in-progress false or omitted", queue.Line)
			}
		}
		// Keep exact line numbers for actionlint's remaining diagnostics. Require
		// an ordinary standalone key so aliases or flow mappings cannot hide a
		// second key on the line being replaced. All other syntax stays intact.
		line := queue.Line - 1
		if line < 0 || line >= len(lines) || !regexp.MustCompile(`^\s+queue:\s+(max|single)\s*(#.*)?$`).MatchString(lines[line]) {
			return nil, fmt.Errorf("line %d: write concurrency queue as a standalone unquoted key", queue.Line)
		}
		lines[line] = ""
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// alertsPermission is the workflow permission scope GitHub added after actionlint
// v1.7.12; that release reports it as unknown, and an ignore entry would also stop
// its value from being checked. Validate it here, then blank the line.
// Remove this once the pinned actionlint knows the scope.
const alertsPermission = "vulnerability-alerts"

var alertsLine = regexp.MustCompile(`^\s+vulnerability-alerts:\s+(read|none)\s*(#.*)?$`)

func validateAlertsPermission(raw []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 {
		return nil, fmt.Errorf("expected one workflow document")
	}
	root := doc.Content[0]
	var blocks []*yaml.Node
	if permissions := field(root, "permissions"); permissions != nil {
		blocks = append(blocks, permissions)
	}
	if jobs := field(root, "jobs"); jobs != nil && jobs.Kind == yaml.MappingNode {
		for i := 1; i < len(jobs.Content); i += 2 {
			if permissions := field(jobs.Content[i], "permissions"); permissions != nil {
				blocks = append(blocks, permissions)
			}
		}
	}
	lines := strings.Split(string(raw), "\n")
	for _, block := range blocks {
		if block.Kind != yaml.MappingNode {
			continue
		}
		count := 0
		for i := 0; i < len(block.Content); i += 2 {
			if block.Content[i].Value == alertsPermission {
				count++
			}
		}
		if count > 1 {
			return nil, fmt.Errorf("duplicate %s permission", alertsPermission)
		}
		value := field(block, alertsPermission)
		if value == nil {
			continue
		}
		// The rewrite below edits the anchored text, which would also change every alias of it.
		if block.Anchor != "" {
			return nil, fmt.Errorf("line %d: do not anchor a permissions block that declares %s", block.Line, alertsPermission)
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || (value.Value != "read" && value.Value != "none") {
			return nil, fmt.Errorf("line %d: %s must be read or none", value.Line, alertsPermission)
		}
		line := value.Line - 1
		if line < 0 || line >= len(lines) || !alertsLine.MatchString(lines[line]) {
			return nil, fmt.Errorf("line %d: declare %s as a standalone unquoted key", value.Line, alertsPermission)
		}
		// A block whose only key is this scope would become empty, which actionlint
		// rejects; an empty flow mapping keeps the line count and parses.
		if len(block.Content) == 2 {
			lines[line] = lines[line][:len(lines[line])-len(strings.TrimLeft(lines[line], " \t"))] + "{}"
		} else {
			lines[line] = ""
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// normalize validates and strips every key the pinned actionlint predates.
func normalize(raw []byte) ([]byte, error) {
	normalized, err := validateQueues(raw)
	if err != nil {
		return nil, err
	}
	return validateAlertsPermission(normalized)
}

func execute(args []string) error {
	if len(args) == 0 || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(args[0]) {
		return fmt.Errorf("usage: workflow-lint PINNED_VERSION [workflow paths]")
	}
	var flags, files []string
	valueFlags := map[string]bool{"-config-file": true, "-format": true, "-ignore": true, "-pyflakes": true, "-shellcheck": true, "-stdin-filename": true}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") && arg != "-" {
			flags = append(flags, arg)
			if valueFlags[arg] {
				i++
				if i >= len(args) {
					return fmt.Errorf("missing value for %s", arg)
				}
				flags = append(flags, args[i])
			}
		} else {
			files = append(files, arg)
		}
	}
	for _, flag := range flags {
		if flag == "-version" || flag == "-init-config" {
			command := exec.Command("go", append([]string{"run", "github.com/rhysd/actionlint/cmd/actionlint@" + args[0]}, flags...)...)
			command.Stdout = os.Stdout
			command.Stderr = os.Stderr
			return command.Run()
		}
	}
	if len(files) == 0 {
		var err error
		files, err = filepath.Glob(".github/workflows/*.y*ml")
		if err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no workflow files found")
	}
	failed := false
	for _, path := range files {
		var raw []byte
		var err error
		if path == "-" {
			raw, err = io.ReadAll(os.Stdin)
		} else {
			raw, err = os.ReadFile(path)
		}
		if err != nil {
			return err
		}
		normalized, err := normalize(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
			continue
		}
		commandArgs := append([]string{"run", "github.com/rhysd/actionlint/cmd/actionlint@" + args[0]}, flags...)
		commandArgs = append(commandArgs, "-stdin-filename", path, "-")
		command := exec.Command("go", commandArgs...)
		command.Stdin = bytes.NewReader(normalized)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("workflow validation failed")
	}
	return nil
}

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
