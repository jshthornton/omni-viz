package main

import (
	"flag"
	"fmt"

	omniviz "github.com/jshthornton/omni-viz"
)

func cmdApprove(argv []string) error {
	fs := flag.NewFlagSet("approve", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	all := fs.Bool("all", false, "approve every failed and new shot from the last report")
	fs.Parse(argv)
	c, err := omniviz.LoadContext(*project)
	if err != nil {
		return err
	}
	keys := fs.Args()
	if *all {
		keys, err = c.PendingKeys()
		if err != nil {
			return err
		}
	}
	if len(keys) == 0 {
		fmt.Println("omniviz: nothing to approve")
		return nil
	}
	approved, err := c.ApproveKeys(keys)
	for _, k := range approved {
		fmt.Printf("  approved %s\n", k)
	}
	if err != nil {
		return err
	}
	fmt.Printf("omniviz: %d baseline(s) updated\n", len(approved))
	return nil
}
