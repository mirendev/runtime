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

A saga marked **BLOCKED** is one the server refused to resume, usually because it was started by an older release whose saga definition the running one cannot vouch for. Nothing ran or was undone when it refused, and nothing will drive the saga until someone acts: running a release that can still resume it lets it finish or compensate normally, and ` + "`" + `miren debug saga abandon` + "`" + ` gives it up when that is not possible.

A saga is also **BLOCKED** when one of its undos has failed at least three times over more than an hour. The release that gave up won't try that undo again, and the action shows its undo error and how long it has been failing. A different release tries it once more when it starts, so deploying a fix usually clears the block without anyone stepping in. If the undo can't succeed under any release, abandon the saga and clean up after the action by hand.

Where a saga stopped is the last action listed. This shows the actions that ran, not the complete set the definition declares, since the saga definitions live in the server process and are not exposed over the API.
:::

` + "```" + `bash
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY --full
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY --format json
` + "```" + ``

const sagaAbandonDescription = `Gives up a blocked saga execution, marking it failed **without** undoing the work it already did. It lists the actions whose work it will leave behind before asking you to confirm, so you know what to clean up by hand.

You should almost never need this. A saga is blocked when the server can't vouch for the definition it was started under, or when one of its undos keeps failing. Either way the normal fix is a release that can handle it, which lets the saga finish or roll back cleanly. Reach for abandon only when that is not an option.

Only blocked executions can be abandoned. One that is pending, running, or undoing without a block is still being driven or will be picked up by recovery, and abandoning it would skip a compensation that was going to happen on its own. A saga whose child saga is still in flight can't be abandoned either: abandon the blocked child first, which lists what it leaves behind, and the parent unwinds on its own the next time it is driven. After abandoning, a sandbox's saga is retried from scratch on its next reconcile, and a saga with a generated name is cleaned up by saga retention. Addon provisioning is the exception: an abandoned provisioning saga leaves its addon in an error state, so destroy the addon with ` + "`" + `miren addon destroy` + "`" + ` and add it again.

` + "```" + `bash
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY
miren debug saga abandon saga/sg-4TzP9hQ2mKdX8vNfR3wLbY
` + "```" + ``
