package config

type Oauth struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret" json:"-"`
	RedirectURL  string `yaml:"redirect_url"`
}

type CoreConfig struct {
	Token                        string   `yaml:"token"` // Either set token or the MTOKEN env var
	Dir                          string   `yaml:"dir"`
	OverrideDir                  string   `yaml:"override_dir"`
	UseCurrentDirectory          bool     `yaml:"use_current_directory"`
	UseCustomWebUI               bool     `yaml:"use_custom_webui"`
	ExperimentalFeatures         []string `yaml:"experimental_features"` // 'reshard'
	Env                          []string `yaml:"env"`
	Names                        []string `yaml:"names"`
	Redis                        string   `yaml:"redis"`
	RedisChannel                 string   `yaml:"redis_channel"`
	AllowedIDS                   []string `yaml:"allowed_ids"`
	Oauth                        Oauth    `yaml:"oauth"`
	PingTimeout                  *int     `yaml:"ping_timeout"`
	PingInterval                 int      `yaml:"ping_interval"`
	ClusterStartNextDelay        *int     `yaml:"cluster_start_next_delay"`
	PerCluster                   uint64   `yaml:"per_cluster"`
	MinimumSafeSessionsRemaining *uint64  `yaml:"minimum_safe_sessions_remaining"`
	FixedShardCount              uint64   `yaml:"fixed_shard_count"` // You likely don't want this outside of rare use cases...

	// The command/module to run, only applicable when using DefaultStart (or the mewld executable)
	Module string `yaml:"module"`
	Interp string `yaml:"interp"`
}
