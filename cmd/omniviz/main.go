package main

import (
	"fmt"
	"os"

	// Builtin drivers — new drivers plug in here (or in any custom binary
	// that imports this package tree).
	_ "github.com/jshthornton/omni-viz/drivers/command"
	_ "github.com/jshthornton/omni-viz/drivers/godot"
	_ "github.com/jshthornton/omni-viz/drivers/web"

	omniviz "github.com/jshthornton/omni-viz"
)

const version = "0.1.0"

const usageText = `omniviz %s — visual regression testing for anything with a pixel

One core (capture → diff → review → approve) and a driver per target kind:
engines, browsers, processes, terminals — the workflow is the same everywhere.

Usage:
  omniviz test     [--project DIR] [filters]   capture every shot, diff vs baselines, report (CI exit code)
  omniviz capture  [--project DIR] [filters]   capture current screenshots only
  omniviz compare  [--project DIR]             diff existing current screenshots vs baselines (no capture)
  omniviz approve  [--project DIR] [--all|KEY...]  promote current screenshots to baselines
  omniviz review   [--project DIR] [--port N]  open the review UI (diffs, overlays, recordings, approvals)
  omniviz shots    [--project DIR]             list configured shots
  omniviz run      [--project DIR] [--driver NAME] TARGET [-- USER_ARGS...]
                                               launch one target adhoc (windowed; no diffing)
  omniviz summary  [--project DIR] [--markdown] print the last report (markdown for
                                               PR comments / $GITHUB_STEP_SUMMARY)
  omniviz drivers                              list registered drivers
  omniviz version

Filters:  --only NAME   run jobs whose name/target contains NAME (repeatable)
          --skip NAME   skip jobs whose name/target contains NAME (repeatable)

Common flags:
  --project DIR    project root containing omniviz.toml (default: .)
  --driver NAME    override the default driver for this run
  --set KEY=VAL    driver-specific override (godot: SECTION/KEY=VALUE project setting)
  --no-record      disable recordings for this run
  --record         force recordings on for this run

Docs: https://github.com/jshthornton/omni-viz
`

func usage() int {
	fmt.Fprintf(os.Stderr, usageText, version)
	return 2
}

func main() {
	os.Exit(run(os.Args))
}

func run(args []string) int {
	if len(args) < 2 {
		return usage()
	}
	var err error
	switch args[1] {
	case "test":
		err = cmdTest(args[2:])
	case "capture":
		err = cmdCapture(args[2:])
	case "compare":
		err = cmdCompare(args[2:])
	case "approve":
		err = cmdApprove(args[2:])
	case "review":
		err = cmdReview(args[2:])
	case "shots":
		err = cmdShots(args[2:])
	case "run":
		err = cmdRun(args[2:])
	case "summary":
		err = cmdSummary(args[2:])
	case "drivers":
		for _, name := range omniviz.DriverNames() {
			fmt.Println(name)
		}
	case "version", "--version", "-v":
		fmt.Println("omniviz", version)
	case "help", "--help", "-h":
		fmt.Printf(usageText, version)
	default:
		return usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "omniviz:", err)
		return 1
	}
	return 0
}
