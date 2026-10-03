// Command mutation runs `meaninggraph check` (in process) over the directories
// that mutate.mjs wrote, and compares its verdict with the one the Node
// reference checker gave, counting the disagreements by direction.
//
//	go run . <out dir of mutate.mjs> [examples per rule, default 3]
//
// It also compares the values: for each mutated YAML file that the reference
// parser reads, the value ParseYAML reads from the same bytes must be equal
// (numbers compared as numbers), or ParseYAML must refuse the file, which is
// counted. A file ParseYAML reads differently is a failure.
//
// It exits 1 when the CLI accepts anything the reference checker refuses, or
// reads a YAML file differently.
// This is a separate Go module on purpose: it is part of the regeneration path,
// not of the default test run or of the coverage the gate measures.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/meaninggraph/cli/internal/cli"
	"github.com/meaninggraph/cli/pkg/meaning"
)

type item struct {
	Address string            `json:"address"`
	Profile string            `json:"profile"`
	Check   string            `json:"check"`
	Graphs  map[string]string `json:"graphs"`
}

type verdict struct {
	Verdict  string   `json:"verdict"`
	Crashed  bool     `json:"crashed"`
	Problems []string `json:"problems"`
}

type report struct {
	Graphs []struct {
		Findings []meaning.Finding `json:"findings"`
	} `json:"graphs"`
}

// runCLI runs `meaninggraph check` over a variant and returns its verdict and
// the error findings it reported.
func runCLI(dir string, it item) (string, []meaning.Finding) {
	args := []string{"check", "--format", "json", filepath.Join(dir, it.Check)}
	for _, address := range sortedKeys(it.Graphs) {
		args = append(args, "--graph", address+"="+it.Graphs[address])
	}
	if it.Address != "" {
		args = append(args, "--address", it.Address)
	}
	if it.Profile != "" {
		args = append(args, "--profile", it.Profile)
	}
	var stdout, stderr bytes.Buffer
	env := cli.OSEnv()
	env.Stdout, env.Stderr = &stdout, &stderr
	code := cli.Run(args, env)
	var parsed report
	_ = json.Unmarshal(stdout.Bytes(), &parsed)
	var errors []meaning.Finding
	for _, g := range parsed.Graphs {
		for _, f := range g.Findings {
			if f.Severity == meaning.Error {
				errors = append(errors, f)
			}
		}
	}
	if code == cli.ExitClean {
		return "accept", nil
	}
	if len(errors) == 0 {
		errors = []meaning.Finding{{Rule: "usage", Message: strings.TrimSpace(stderr.String())}}
	}
	return "refuse", errors
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// equal compares two decoded JSON values.
func equal(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, present := y[k]
			if !present || !equal(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// compareValues returns how many mutated YAML files were compared, how many
// ParseYAML refused although the reference parser read them, and the files
// whose values differ.
func compareValues(out string, values map[string]map[string]any) (compared, refused int, differ []string) {
	for _, name := range sortedKeys(values) {
		for _, file := range sortedKeys(values[name]) {
			path := filepath.Join(out, "variants", name, file)
			data, err := os.ReadFile(path)
			if err != nil {
				differ = append(differ, path+": "+err.Error())
				continue
			}
			node, syntax := meaning.ParseYAML(data)
			if syntax != nil {
				refused++
				continue
			}
			compared++
			// Round trip through JSON, so that both sides are plain decoded values.
			var got any
			encoded, _ := json.Marshal(node.Value())
			_ = json.Unmarshal(encoded, &got)
			if !equal(got, values[name][file]) {
				differ = append(differ, path)
			}
		}
	}
	return compared, refused, differ
}

type example struct{ name, node, cli string }

func main() {
	examples := 3
	if len(os.Args) == 3 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil && n > 0 {
			examples = n
		}
	}
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: mutation <out dir of mutate.mjs> [examples per rule]")
		os.Exit(2)
	}
	out := os.Args[1]
	var verdicts map[string]verdict
	data, err := os.ReadFile(filepath.Join(out, "verdicts.json"))
	if err == nil {
		err = json.Unmarshal(data, &verdicts)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutation:", err)
		os.Exit(2)
	}
	var values map[string]map[string]any
	if data, err := os.ReadFile(filepath.Join(out, "values.json")); err == nil {
		if err = json.Unmarshal(data, &values); err != nil {
			fmt.Fprintln(os.Stderr, "mutation:", err)
			os.Exit(2)
		}
	}
	agree, unsafe := 0, 0
	stricter := map[string][]example{}
	var stricterTotal int
	for _, name := range sortedKeys(verdicts) {
		dir := filepath.Join(out, "variants", name)
		var it item
		raw, err := os.ReadFile(filepath.Join(dir, "item.json"))
		if err == nil {
			err = json.Unmarshal(raw, &it)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "mutation:", err)
			os.Exit(2)
		}
		node := verdicts[name]
		got, findings := runCLI(dir, it)
		switch {
		case got == node.Verdict:
			agree++
		case got == "accept":
			unsafe++
			fmt.Printf("CLI ACCEPTS, reference refuses: %s\n  reference: %s\n", filepath.Join(dir), strings.Join(node.Problems, " | "))
		default:
			stricterTotal++
			rule := findings[0].Rule
			stricter[rule] = append(stricter[rule], example{name, "", fmt.Sprintf("%s: %s", findings[0].Rule, findings[0].Message)})
		}
	}
	fmt.Printf("\nmutants: %d\nagree: %d\nCLI accepts, reference refuses: %d\nCLI refuses, reference accepts: %d\n", len(verdicts), agree, unsafe, stricterTotal)
	for _, rule := range sortedKeys(stricter) {
		list := stricter[rule]
		fmt.Printf("\n  %-26s %d\n", rule, len(list))
		for _, ex := range list[:min(examples, len(list))] {
			fmt.Printf("    %s/variants/%s: %.160s\n", out, ex.name, ex.cli)
		}
	}
	compared, refused, differ := compareValues(out, values)
	fmt.Printf("\nYAML values: %d mutated files read by both parsers, %d read by the reference parser and refused by ParseYAML, %d read differently\n", compared, refused, len(differ))
	for _, path := range differ[:min(10, len(differ))] {
		fmt.Printf("  differs: %s\n", path)
	}
	if unsafe > 0 || len(differ) > 0 {
		os.Exit(1)
	}
}
