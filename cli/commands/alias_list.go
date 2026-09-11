package commands

import (
	"fmt"
	"sort"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/ui"
)

func AliasList(ctx *Context, opts struct {
	FormatOptions
}) error {
	ac, configPath, err := local.load()
	if err != nil {
		local.fatal("")
		return err
	}

	if ac == nil || len(ac.Aliases) == 0 {
		ctx.Printf("No aliases configured.\n")
		return nil
	}

	lineNumbers := appconfig.AliasLineNumbers(configPath)

	names := make([]string, 0, len(ac.Aliases))
	for name := range ac.Aliases {
		names = append(names, name)
	}
	sort.Strings(names)

	if opts.IsJSON() {
		type AliasInfo struct {
			Name    string `json:"name"`
			Command string `json:"command"`
			Source  string `json:"source"`
		}

		var items []AliasInfo
		for _, name := range names {
			source := configPath
			if line, ok := lineNumbers[name]; ok {
				source = fmt.Sprintf("%s:%d", configPath, line)
			}
			items = append(items, AliasInfo{
				Name:    name,
				Command: ac.Aliases[name],
				Source:  source,
			})
		}
		return PrintJSON(items)
	}

	headers := []string{"NAME", "COMMAND", "SOURCE"}
	var rows []ui.Row

	for _, name := range names {
		source := configPath
		if line, ok := lineNumbers[name]; ok {
			source = fmt.Sprintf("%s:%d", configPath, line)
		}
		rows = append(rows, ui.Row{name, ac.Aliases[name], source})
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0).NoTruncate(1))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}
