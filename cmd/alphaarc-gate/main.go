// Command alphaarc-gate is the generalization gate + regression harness.
//
// WHY THIS EXISTS. The project's recurring failure mode is a fix that passes
// the very test written for it, accumulates as a crutch, and hides a wall one
// layer deeper -- "works perfectly while the one thing it was built for is
// tested." The antidote is a held-out measurement the shipped agent must clear
// on games it was NOT built against, checked against a committed baseline so
// "did this change regress?" is answered mechanically, not from memory.
//
// WHAT IT MEASURES. It runs the REAL live agent (cmd/alphaarc-play, the exact
// code path we ship -- NOT cmd/alphaarc-suite, which is a separate divergent
// loop that never exercises driveGain/ResidualTargets) over the frozen split in
// gate/games.txt, R repeats per game, and reports per game:
//   solveRate      = fraction of repeats that reached >=1 level (the ground
//                    truth -- the only signal the agent can't fake with busywork)
//   meanBestLevels = mean of the highest level reached per repeat
//   meanChanged    = mean board-changing actions per repeat (dense diagnostic:
//                    separates "engaging" from "babbling on dead pixels")
//
// Runs leans on REPEATS, not determinism: -seed reduces but can't remove
// run-to-run variation (Go randomizes map iteration too), so we average.
//
// DISCIPLINE. dev-set regressions FAIL the gate (exit 1). blind-set numbers are
// the honest generalization signal -- printed, never optimized against directly
// (see gate/games.txt). Update the baseline ONLY after a verified improvement,
// with -update-baseline.
//
// USAGE (build the player once; the gate execs it, it does not compile):
//   go build -o alphaarc-play ./cmd/alphaarc-play
//   export ARC_API_KEY=$(grep '^ARC_API_KEY=' .env | cut -d= -f2-)
//   go run ./cmd/alphaarc-gate -player ./alphaarc-play              # check vs baseline
//   go run ./cmd/alphaarc-gate -player ./alphaarc-play -set dev     # dev only (cheaper)
//   go run ./cmd/alphaarc-gate -player ./alphaarc-play -update-baseline
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

type splitEntry struct {
	set  string // "dev" | "blind"
	game string
}

// runResult is the JSON payload cmd/alphaarc-play prints under -result-json.
type runResult struct {
	Game           string `json:"game"`
	Seed           int64  `json:"seed"`
	Actions        int    `json:"actions"`
	BestLevels     int    `json:"bestLevels"`
	ChangedActions int    `json:"changedActions"`
}

// aggregate is the per-game summary stored in the baseline and compared.
type aggregate struct {
	Set            string  `json:"set"`
	Repeats        int     `json:"repeats"`
	SolveRate      float64 `json:"solveRate"`
	MeanBestLevels float64 `json:"meanBestLevels"`
	MeanChanged    float64 `json:"meanChanged"`
}

var resultLine = regexp.MustCompile(`(?m)^RESULT (\{.*\})\s*$`)

func main() {
	player := flag.String("player", "./alphaarc-play", "path to a prebuilt cmd/alphaarc-play binary (build: go build -o alphaarc-play ./cmd/alphaarc-play)")
	splitPath := flag.String("games", "gate/games.txt", "frozen dev/blind split file")
	baselinePath := flag.String("baseline", "gate/baseline.json", "committed baseline to compare against / write with -update-baseline")
	set := flag.String("set", "all", "which set to run: dev | blind | all")
	actions := flag.Int("actions", 150, "actions per game per repeat")
	maxBlobs := flag.Int("maxblobs", 20, "maxblobs passed to the player")
	repeats := flag.Int("repeats", 3, "repeats per game (averaged; the gate leans on this, not on determinism)")
	tolerance := flag.Float64("tolerance", 0.34, "allowed drop in dev solveRate before it counts as a regression (~1/repeats of noise)")
	updateBaseline := flag.Bool("update-baseline", false, "write the current run as the new baseline instead of comparing")
	flag.Parse()

	if os.Getenv("ARC_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "ARC_API_KEY not set -- export it before running the gate")
		os.Exit(2)
	}
	if _, err := os.Stat(*player); err != nil {
		fmt.Fprintf(os.Stderr, "player binary %q not found: %v\nbuild it first: go build -o alphaarc-play ./cmd/alphaarc-play\n", *player, err)
		os.Exit(2)
	}

	split, err := readSplit(*splitPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read split: %v\n", err)
		os.Exit(2)
	}

	current := map[string]aggregate{}
	for _, e := range split {
		if *set != "all" && e.set != *set {
			continue
		}
		agg := runGame(*player, e, *actions, *maxBlobs, *repeats)
		current[e.game] = agg
		fmt.Printf("  %-16s %-5s solveRate=%.2f meanLevels=%.2f meanChanged=%.1f (n=%d)\n",
			e.game, e.set, agg.SolveRate, agg.MeanBestLevels, agg.MeanChanged, agg.Repeats)
	}

	if *updateBaseline {
		if err := writeBaseline(*baselinePath, current); err != nil {
			fmt.Fprintf(os.Stderr, "write baseline: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("\nbaseline written to %s (%d games)\n", *baselinePath, len(current))
		return
	}

	baseline, err := readBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nno baseline at %s (%v)\nrun once with -update-baseline to establish it\n", *baselinePath, err)
		os.Exit(2)
	}

	fmt.Printf("\n=== GATE vs %s ===\n", *baselinePath)
	regressed := false
	games := make([]string, 0, len(current))
	for g := range current {
		games = append(games, g)
	}
	sort.Strings(games)
	for _, g := range games {
		cur := current[g]
		base, ok := baseline[g]
		if !ok {
			fmt.Printf("  %-16s %-5s solveRate=%.2f (NEW -- not in baseline)\n", g, cur.Set, cur.SolveRate)
			continue
		}
		delta := cur.SolveRate - base.SolveRate
		flag := "ok"
		if cur.Set == "dev" && delta < -*tolerance {
			flag = "REGRESSED"
			regressed = true
		} else if delta < -*tolerance {
			flag = "dropped (blind, informational)"
		} else if delta > *tolerance {
			flag = "improved"
		}
		fmt.Printf("  %-16s %-5s solveRate %.2f -> %.2f (%+.2f)  %s\n", g, cur.Set, base.SolveRate, cur.SolveRate, delta, flag)
	}

	if regressed {
		fmt.Println("\nGATE: FAIL -- a dev game regressed. Do not accept this change as-is.")
		os.Exit(1)
	}
	fmt.Println("\nGATE: PASS -- no dev regression.")
}

func runGame(player string, e splitEntry, actions, maxBlobs, repeats int) aggregate {
	agg := aggregate{Set: e.set, Repeats: repeats}
	solved, sumLevels, sumChanged := 0, 0, 0
	for i := 0; i < repeats; i++ {
		res, ok := runOnce(player, e.game, actions, maxBlobs, int64(i+1))
		if !ok {
			continue
		}
		if res.BestLevels >= 1 {
			solved++
		}
		sumLevels += res.BestLevels
		sumChanged += res.ChangedActions
	}
	if repeats > 0 {
		agg.SolveRate = float64(solved) / float64(repeats)
		agg.MeanBestLevels = float64(sumLevels) / float64(repeats)
		agg.MeanChanged = float64(sumChanged) / float64(repeats)
	}
	return agg
}

func runOnce(player, game string, actions, maxBlobs int, seed int64) (runResult, bool) {
	cmd := exec.Command(player,
		"-game", game,
		"-actions", fmt.Sprintf("%d", actions),
		"-maxblobs", fmt.Sprintf("%d", maxBlobs),
		"-seed", fmt.Sprintf("%d", seed),
		"-result-json",
	)
	cmd.Env = os.Environ() // pass ARC_API_KEY through
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "    run %s seed=%d failed: %v\n", game, seed, err)
		return runResult{}, false
	}
	m := resultLine.FindSubmatch(out)
	if m == nil {
		fmt.Fprintf(os.Stderr, "    run %s seed=%d: no RESULT line in output\n", game, seed)
		return runResult{}, false
	}
	var res runResult
	if err := json.Unmarshal(m[1], &res); err != nil {
		fmt.Fprintf(os.Stderr, "    run %s seed=%d: bad RESULT json: %v\n", game, seed, err)
		return runResult{}, false
	}
	return res, true
}

func readSplit(path string) ([]splitEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []splitEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || (fields[0] != "dev" && fields[0] != "blind") {
			return nil, fmt.Errorf("bad split line %q (want `<dev|blind> <game-id>`)", line)
		}
		out = append(out, splitEntry{set: fields[0], game: fields[1]})
	}
	return out, sc.Err()
}

func readBaseline(path string) (map[string]aggregate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]aggregate
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func writeBaseline(path string, m map[string]aggregate) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
