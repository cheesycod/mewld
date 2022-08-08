# mewld

Mewbot clusterer. Will be open source to help other large bot developers.

Mewbot clusterer is rather generic and any python bot should be able to adapt to it without any changes to ``mewld``

**$CHANNEL** in these docs is defined as the configured redis channel for the bot.

## Redis

Mewld uses redis for communication with clusters.

**Format:**

JSON with the following structure:

```go
Scope     string         `json:"scope"`
Action    string         `json:"action"`
Args      map[string]any `json:"args,omitempty"`
CommandId string         `json:"command_id,omitempty"`
Output    string         `json:"output,omitempty"`
```

## Operations

| Syntax      	   | Description 									  | Args                    |
| ------           | ----------- 									  | ----                    |
| launch_next      | Ready to launch next cluster, if available       | id -> cluster ID        |
| rollingrestart   | Rolling restart all clusters, one by one         |                         |
| statuses         | Gets the statuses of all clusters. Currently a *internal* API |            |
| shutdown         | Shuts down the entire bot and lets systemctl start it up again if needed | |
| restartproc      | Shuts down bot with error code so systemctl restarts it automatically |    |
| diag             | The cluster must respond with a ``proc.DiagResponse`` payload. This is used as a diagnostic by the clusterer and may be used in the future for more important actions | |

## TODO

Finish documenting redis and bug fixes

## Diagnostics

Whenever a diagnostic is sent over the ``$CHANNEL_diag`` channel, the cluster must respond with a ``diag`` (see Operations table) within 10 seconds.