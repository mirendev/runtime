package commands

const sagaSectionDescription = `Sagas are Miren's mechanism for multi-step operations that have to either finish or unwind cleanly, like provisioning an addon or building and deploying an app. Each saga execution records what it ran, in what order, and what each action returned, so a saga that wedges leaves behind a trail you can read.

These commands read that trail. The underlying records are also visible through ` + "`" + `miren debug entity list -k saga` + "`" + `, but the interesting fields are stored as JSON blobs, so that view shows you base64 rather than what happened.`

const sagaListDescription = `By default this lists only active sagas, meaning the ones that are pending, running, or undoing. Completed sagas accumulate and are rarely what you are looking for when something is wedged. Pass ` + "`" + `--all` + "`" + ` to include them, or ` + "`" + `--status` + "`" + ` to ask for one specific status.

The UPDATED column is the useful one for finding a stuck saga: the record is written after every action, so a saga that has been running for an hour without an update has stopped making progress.

` + "```" + `bash
miren debug saga list
miren debug saga list --status failed
miren debug saga list --definition provision_mysql_dedicated --all
miren debug saga list --format json
` + "```" + ``

const sagaShowDescription = `Shows one saga execution in full: its status, its initial inputs, and every action it ran with timing, undo state, and output.

:::note[Output details]
Action outputs are truncated by default so a long saga stays readable. Pass ` + "`" + `--full` + "`" + ` to print them whole. ` + "`" + `--format json` + "`" + ` always carries them in full, since a partial record is worse than a large one for anything parsing it.

Where a saga stopped is the last action listed. This shows the actions that ran, not the complete set the definition declares, since the saga definitions live in the server process and are not exposed over the API.
:::

` + "```" + `bash
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY --full
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY --format json
` + "```" + ``
