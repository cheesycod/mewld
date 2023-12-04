package config

type Oauth struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret" json:"-"`
	RedirectURL  string `yaml:"redirect_url"`
}

type CoreConfig struct {
	Dir                 string   `yaml:"dir"`
	OverrideDir         string   `yaml:"override_dir"`
	UseCurrentDirectory bool     `yaml:"use_current_directory"`
	Env                 []string `yaml:"env"`
	Names               []string `yaml:"names"`
	Redis               string   `yaml:"redis"`
	RedisChannel        string   `yaml:"redis_channel"`
	AllowedIDS          []string `yaml:"allowed_ids"`
	Oauth               Oauth    `yaml:"oauth"`
	PingTimeout         int      `yaml:"ping_timeout"`
	PingInterval        int      `yaml:"ping_interval"`
	PerCluster          uint64   `yaml:"per_cluster"`

	// The command/module to run
	Module string `yaml:"module"`
	Interp string `yaml:"interp"`
}
