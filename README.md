# mewld

Mewbot clusterer. Will possibly be open source as it could help other bot developers.

Mewbot clusterer is rather generic and any python bot should be able to adapt to it without any changes to ``mewld``

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

| Syntax      	   | Description 									  | Args                    |
| ------           | ----------- 									  | ----                    |
| launch_next      | Ready to launch next cluster, if available       | id -> cluster ID        |
| rollingrestart   | Rolling restart all clusters, one by one         |                         |
| statuses         | Gets the statuses of all clusters. Currently a *internal* API |            |
| shutdown         | Shuts down the entire bot and lets systemctl start it up again if needed | |

## TODO

Finish documenting redis and bug fixes