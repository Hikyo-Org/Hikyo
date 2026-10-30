// Command codspeed-verify checks that CodSpeed's uploaded measurement path
// agrees with the accumulated Go benchmark timing from the same invocation.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// This detects a broken measurement boundary, not a performance regression.
// The original timer-tail defect differed by several orders of magnitude.
const maxTimingRatio = 2.0

// Keep this receipt check in sync with the workflow pin when qualifying a new
// Go integration. The outer runner can reuse a preinstalled integration binary.
const qualifiedIntegrationVersion = "1.3.0"

var benchmarkLine = regexp.MustCompile(`^(Benchmark\S*)-[0-9]+\s+([0-9]+)\s+([0-9.eE+-]+)\s+ns/op(?:\s|$)`)

type benchmarkID struct {
	pkg, name string
}

type resultFile struct {
	Creator struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"creator"`
	Instrument struct {
		Type string `json:"type"`
	} `json:"instrument"`
	Benchmarks []struct {
		Name  string `json:"name"`
		URI   string `json:"uri"`
		Stats struct {
			MeanNS float64 `json:"mean_ns"`
			Rounds uint64  `json:"rounds"`
		} `json:"stats"`
	} `json:"benchmarks"`
}

type measuredBenchmark struct {
	owner, name, uri string
	meanNS           float64
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "codspeed-verify:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("codspeed-verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	logPath := flags.String("log", "", "stdout captured from the instrumented Go benchmarks")
	resultsPath := flags.String("results", "", "CodSpeed profile results directory from that invocation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *logPath == "" || *resultsPath == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: codspeed-verify --log FILE --results DIRECTORY")
	}
	log, err := os.Open(*logPath)
	if err != nil {
		return err
	}
	defer log.Close()
	native, err := readNative(log)
	if err != nil {
		return err
	}
	measured, err := readMeasured(*resultsPath)
	if err != nil {
		return err
	}
	if len(native) != len(measured) {
		return fmt.Errorf("benchmark count mismatch: Go stdout has %d, CodSpeed has %d", len(native), len(measured))
	}
	// Root-package URIs have no import-path suffix. Match named package owners
	// first so a repeated root benchmark name can be resolved unambiguously.
	sort.Slice(measured, func(i, j int) bool {
		if measured[i].owner == "." && measured[j].owner != "." {
			return false
		}
		if measured[j].owner == "." && measured[i].owner != "." {
			return true
		}
		if len(measured[i].owner) != len(measured[j].owner) {
			return len(measured[i].owner) > len(measured[j].owner)
		}
		return measured[i].uri < measured[j].uri
	})
	matched := make(map[benchmarkID]bool, len(native))
	for _, benchmark := range measured {
		var candidates []benchmarkID
		for id := range native {
			if !matched[id] && id.name == benchmark.name && (benchmark.owner == "." || strings.HasSuffix(id.pkg, "/"+benchmark.owner)) {
				candidates = append(candidates, id)
			}
		}
		if len(candidates) != 1 {
			return fmt.Errorf("%s matches %d unmatched Go benchmarks; expected one", benchmark.uri, len(candidates))
		}
		id := candidates[0]
		ratio := benchmark.meanNS / native[id]
		if ratio < 1/maxTimingRatio || ratio > maxTimingRatio {
			return fmt.Errorf("%s: measurement paths disagree: Go %.3f ns/op, CodSpeed %.3f ns/op (ratio %.6f, allowed %.1f to %.1f); inspect timer boundaries", benchmark.uri, native[id], benchmark.meanNS, ratio, 1/maxTimingRatio, maxTimingRatio)
		}
		matched[id] = true
	}
	_, err = fmt.Fprintf(out, "CodSpeed timing validation passed for %d benchmark cases.\n", len(matched))
	return err
}

func readNative(reader io.Reader) (map[benchmarkID]float64, error) {
	benchmarks := make(map[benchmarkID]float64)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var pkg string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "pkg: ") {
			pkg = strings.TrimSpace(strings.TrimPrefix(line, "pkg: "))
			continue
		}
		if !strings.HasPrefix(line, "Benchmark") || !strings.Contains(line, "ns/op") {
			continue
		}
		fields := benchmarkLine.FindStringSubmatch(line)
		if fields == nil || pkg == "" {
			return nil, fmt.Errorf("invalid benchmark timing or missing package: %s", line)
		}
		iterations, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil || iterations == 0 {
			return nil, fmt.Errorf("invalid benchmark iterations: %s", line)
		}
		ns, err := strconv.ParseFloat(fields[3], 64)
		if err != nil || !positiveFinite(ns) {
			return nil, fmt.Errorf("invalid benchmark duration: %s", line)
		}
		id := benchmarkID{pkg: pkg, name: fields[1]}
		if _, exists := benchmarks[id]; exists {
			return nil, fmt.Errorf("duplicate Go benchmark %s/%s", id.pkg, id.name)
		}
		benchmarks[id] = ns
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(benchmarks) == 0 {
		return nil, fmt.Errorf("Go stdout contains no benchmark timings")
	}
	return benchmarks, nil
}

func readMeasured(directory string) ([]measuredBenchmark, error) {
	files, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("CodSpeed results directory contains no JSON files: %s", directory)
	}
	var benchmarks []measuredBenchmark
	seen := make(map[benchmarkID]bool)
	for _, filename := range files {
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		var file resultFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("%s: %w", filename, err)
		}
		if file.Creator.Name != "codspeed-go" || file.Instrument.Type != "walltime" || len(file.Benchmarks) == 0 {
			return nil, fmt.Errorf("%s: expected nonempty codspeed-go walltime results", filename)
		}
		if file.Creator.Version != qualifiedIntegrationVersion {
			return nil, fmt.Errorf("%s: unqualified Go integration version %q, expected %q", filename, file.Creator.Version, qualifiedIntegrationVersion)
		}
		for _, benchmark := range file.Benchmarks {
			uriPath, uriName, ok := strings.Cut(benchmark.URI, "::")
			name := strings.ReplaceAll(benchmark.Name, "::", "/")
			cleanPath := path.Clean(uriPath)
			if !ok || uriPath == "" || strings.Contains(uriPath, "\\") || path.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || !strings.HasSuffix(cleanPath, ".go") || !strings.HasPrefix(name, "Benchmark") || name != strings.ReplaceAll(uriName, "::", "/") {
				return nil, fmt.Errorf("%s: invalid benchmark identity %q / %q", filename, benchmark.Name, benchmark.URI)
			}
			if !positiveFinite(benchmark.Stats.MeanNS) || benchmark.Stats.Rounds == 0 {
				return nil, fmt.Errorf("%s: invalid mean or rounds for %s", filename, benchmark.URI)
			}
			id := benchmarkID{pkg: path.Dir(cleanPath), name: name}
			if seen[id] {
				return nil, fmt.Errorf("duplicate CodSpeed benchmark %s/%s", id.pkg, id.name)
			}
			seen[id] = true
			benchmarks = append(benchmarks, measuredBenchmark{owner: id.pkg, name: name, uri: benchmark.URI, meanNS: benchmark.Stats.MeanNS})
		}
	}
	return benchmarks, nil
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
